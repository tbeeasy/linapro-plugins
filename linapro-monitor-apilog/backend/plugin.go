// Package backend 把 linapro-monitor-apilog 源码插件装配进宿主机插件注册表。
//
// 本插件只做两件事：
//  1. 注册一个全局中间件，自动记录「路由归属插件、且没有后台登录身份」的接口请求；
//  2. 注册一个每日任务补齐未来分区（只创建、不删除）。
//
// 它不依赖任何插件：调用方身份只从上下文约定键读取，告警走共享库 linapro-lark-sdk/larkalert。
package backend

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/pluginhost"
	pluginmonitorapilog "lina-plugin-linapro-monitor-apilog"
	apilogcontroller "lina-plugin-linapro-monitor-apilog/backend/internal/controller/apilog"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
	middlewaresvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/middleware"
	"lina-plugin-linapro-monitor-apilog/backend/internal/service/partition"
	"linapro-lark-sdk/larkalert"
	"linapro-lark-sdk/larkim"
)

// pluginID 是嵌入的 plugin.yaml 声明的不可变标识。
const pluginID = "linapro-monitor-apilog"

// 分区补齐任务的注册参数与告警配置。
const (
	// partitionJobName 是管理端任务面板中展示的规范任务名。
	partitionJobName = "linapro-monitor-apilog-partition"
	// partitionJobPattern 是每日 04:07 执行：每月只需成功一次，避开整点高峰。
	partitionJobPattern = "0 7 4 * * *"
	// partitionAlertConfigKey 是本插件自己的告警配置键（与 lark-push 的键互不复用）。
	partitionAlertConfigKey = "plugin.linapro-monitor-apilog.alert"
	// partitionAlertKind 是分区补齐失败的告警类型，取值必须稳定（同时用作防抖键）。
	partitionAlertKind = "apilog_partition_missing"
)

// init 注册源码插件、全局记录中间件与分区补齐任务。
func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(pluginmonitorapilog.EmbeddedFiles)
	if err := plugin.HTTP().RegisterRoutes(
		pluginhost.ExtensionPointHTTPRouteRegister,
		pluginhost.CallbackExecutionModeBlocking,
		registerRoutes,
	); err != nil {
		panic(err)
	}
	if err := plugin.Jobs().RegisterJobs(
		pluginhost.ExtensionPointJobsRegister,
		pluginhost.CallbackExecutionModeBlocking,
		registerPartitionJob,
	); err != nil {
		panic(err)
	}
	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}

// registerRoutes 绑定全局记录中间件与后台查询路由。
//
// DI 来源（依赖的 owner、创建位置、传递路径）：
//   - routecap / bizctxcap / hostconfigcap / cachecap：宿主创建，经 registrar.Services() 传入；
//   - apilogsvc：本插件在下方创建，纯内存无依赖；
//   - middlewaresvc：本插件创建，持有上面四项。
func registerRoutes(_ context.Context, registrar pluginhost.HTTPRegistrar) error {
	var (
		routes      = registrar.Routes()
		middlewares = routes.Middlewares()
		services    = registrar.Services()
	)
	if services == nil || services.Route() == nil || services.BizCtx() == nil || services.HostConfig() == nil {
		return gerror.New("linapro-monitor-apilog：注册路由需要宿主提供 route、bizctx、hostconfig 能力")
	}

	var (
		apiLogSvc        = apilogsvc.New()
		recordMiddleware = middlewaresvc.New(services.Route(), services.BizCtx(), services.HostConfig(), apiLogSvc)
	)
	// 全局中间件：绑定 "/*" 才能在插件路由执行完之后判定归属插件。
	// 判定不通过的请求（后台管理、静态资源、宿主路由）零开销。
	if err := registrar.GlobalMiddlewares().Bind("/*", recordMiddleware.Record); err != nil {
		return err
	}

	routes.Group(routes.APIPrefix(), func(group pluginhost.RouteGroup) {
		group.Group("/api/v1", func(group pluginhost.RouteGroup) {
			group.Middleware(
				middlewares.NeverDoneCtx(),
				middlewares.HandlerResponse(),
				middlewares.CORS(),
				middlewares.RequestBodyLimit(),
				middlewares.Ctx(),
			)
			group.Group("/", func(group pluginhost.RouteGroup) {
				// 后台查询走宿主会话鉴权与权限点；权限码声明在各接口的 g.Meta 上。
				group.Middleware(
					middlewares.Auth(),
					middlewares.Tenancy(),
					middlewares.Permission(),
				)
				group.Bind(apilogcontroller.NewV1(apiLogSvc))
			})
		})
	})
	return routes.Err()
}

// registerPartitionJob 注册每日分区补齐任务。
//
// 只在主节点执行：多节点同时执行 DDL 会互相阻塞。任务从不删除分区，
// 与「记录只增不删」的约束并不冲突——禁止的是删除路径，不是定时任务。
func registerPartitionJob(ctx context.Context, registrar pluginhost.JobsRegistrar) error {
	services := registrar.Services()
	if services == nil {
		return gerror.New("linapro-monitor-apilog：注册分区补齐任务需要宿主能力")
	}
	partitionSvc := partition.New()

	alertEnv := larkalert.Env{
		Config: services.HostConfig(),
		Cache:  services.Cache(),
		NewClient: func(appID, appSecret string) larkalert.TextSender {
			return larkim.NewClient(appID, appSecret)
		},
	}

	return registrar.AddWithMetadata(
		ctx,
		partitionJobPattern,
		partitionJobName,
		"接口请求日志分区补齐",
		"确保接口请求日志表「当前月 + 未来 6 个月」的月度分区存在；只创建，不删除任何分区",
		func(jobCtx context.Context) error {
			if !registrar.IsPrimaryNode() {
				logger.Debugf(jobCtx, "linapro-monitor-apilog：非主节点，跳过分区补齐")
				return nil
			}
			created, err := partitionSvc.Ensure(jobCtx)
			if err != nil {
				partition.LogFailure(jobCtx, err)
				// 告警无返回值：通道未配置时库内静默丢弃，不改变任务成败判定。
				larkalert.Notify(jobCtx, alertEnv, larkalert.Event{
					ConfigKey: partitionAlertConfigKey,
					Kind:      partitionAlertKind,
					Detail:    buildAlertDetail(created, err),
				})
				return err
			}
			if len(created) > 0 {
				logger.Infof(jobCtx, "linapro-monitor-apilog：已创建分区 %v", created)
			}
			return nil
		},
	)
}

// buildAlertDetail 拼装告警详情：带上已完成的分区与失败原因，便于对账。
func buildAlertDetail(created []string, err error) string {
	return fmt.Sprintf("已创建分区=%v；失败原因=%v", created, err)
}
