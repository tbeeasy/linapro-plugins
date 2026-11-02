// Package backend 把 linapro-employee-core 数据源插件装配进宿主机插件注册表。
// 它将员工写入网关服务和员工 JWT 密钥提供方绑定到 empcap 门面
// （供考勤/问卷通过 go.mod replace 消费），然后注册员工登录/身份路由
// 以及 Moka 驱动的全量同步 cron 任务。
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/pluginhost"
	pluginemployeecore "lina-plugin-linapro-employee-core"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	empctrl "lina-plugin-linapro-employee-core/backend/internal/controller/emp"
	adminempctrl "lina-plugin-linapro-employee-core/backend/internal/controller/employee"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
	employeesvc "lina-plugin-linapro-employee-core/backend/internal/service/employee"
	loginsvc "lina-plugin-linapro-employee-core/backend/internal/service/login"
	"lina-plugin-linapro-employee-core/backend/internal/sync"
	"linapro-lark-sdk/larkcontact"

	"github.com/gogf/gf/v2/errors/gerror"
)

// pluginID 是嵌入的 plugin.yaml 声明的不可变标识。
const pluginID = "linapro-employee-core"

// empJWTSecretKey 是独立的员工 JWT 签名密钥配置键。
const empJWTSecretKey = "plugin.linapro-employee-core.emp_jwt_secret"

// deptTenantMapKey 是保存 Moka 顶层部门名 → tenant_id JSON 映射的 sys_config 键，
// 供全量同步任务使用。键为 Moka 完整部门路径（如
// "广州星嘉/公共部门/总经办/CEO"）第一个 "/" 之前的顶层部门名（"广州星嘉"），
// 而非部门 ID。
const deptTenantMapKey = "plugin.linapro-employee-core.dept_tenant_map"

// syncIntervalKey 是同步周期配置键。
const syncIntervalKey = "plugin.linapro-employee-core.sync_interval_minutes"

// syncEnabledKey 是全量同步 cron 的总开关。缺省 true（线上无需配置）；
// 本地开发置 false 时本插件仍照常加载（empcap 绑定与路由不变，考勤/问卷等
// 依赖方不受影响），只是不声明同步任务——避免本地库被线上 Moka 数据覆盖。
// 注意：interval_minutes 设为 0 不能关任务，那只会落到默认的 0 分整点执行。
const syncEnabledKey = "plugin.linapro-employee-core.sync_enabled"

// syncIntervalMinutes 是默认同步间隔。该值在任务注册时读取一次；
// 修改它需要进程重启。
const syncIntervalMinutes = 0

// syncTimeoutMinutes 是单次全量同步的执行超时。全量同步需拉 Moka 全量员工、
// 逐飞书应用枚举本租户及关联组织通讯录、再做 upsert/姓名回填/离职对账，
// 关联组织递归遍历带节流后整体耗时可达数分钟，远超宿主 300 秒默认任务超时。
// 故在 handler 内解绑宿主 deadline，改用本插件自控的更长超时。
const syncTimeoutMinutes = 30

// syncJobName 是管理端任务面板中展示的规范任务名。
const syncJobName = "linapro-employee-core-sync"

// sharedEmployee 是进程共享的员工写入网关/查询服务。
var sharedEmployee = employeesvc.New()

// init 把员工服务绑定到 empcap 门面，并注册路由与全量同步任务。
func init() {
	empcap.BindEmployeeService(sharedEmployee)

	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(pluginemployeecore.EmbeddedFiles)

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
		registerSyncJob,
	); err != nil {
		panic(err)
	}
	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}

// registerRoutes 绑定 JWT 密钥提供方并注册 HTTP 路由。
func registerRoutes(_ context.Context, registrar pluginhost.HTTPRegistrar) error {
	var (
		routes      = registrar.Routes()
		middlewares = routes.Middlewares()
		services    = registrar.Services()
	)
	if services == nil || services.HostConfig() == nil {
		return gerror.New("linapro-employee-core 路由注册需要宿主机配置能力")
	}

	hostConfig := services.HostConfig()
	empcap.BindTokenSecretProvider(func(ctx context.Context) (string, error) {
		return hostConfig.String(ctx, empJWTSecretKey, "")
	})

	loginService := loginsvc.New(sharedEmployee, lark.NewProvider(), lark.NewCredentialLoader(hostConfig))
	loginController := empctrl.NewLoginV1(loginService)
	meController := empctrl.NewMeV1(sharedEmployee)

	// 后台控制器依赖宿主租户与用户能力补名字 / 候选；宿主未绑定时传 nil，
	// 控制器内部按「附加信息缺失」降级，不阻断列表与详情。
	adminController := adminempctrl.NewV1(
		sharedEmployee,
		hostConfig,
		services.Tenant(),
		services.Users(),
		func(appID, appSecret string) adminempctrl.ContactClient {
			return larkcontact.NewClient(appID, appSecret)
		},
	)

	routes.Group(routes.APIPrefix(), func(group pluginhost.RouteGroup) {
		group.Group("/api/v1", func(group pluginhost.RouteGroup) {
			group.Middleware(
				middlewares.NeverDoneCtx(),
				middlewares.HandlerResponse(),
				middlewares.CORS(),
				middlewares.RequestBodyLimit(),
			)
			group.Bind(loginController)
			group.Group("/", func(group pluginhost.RouteGroup) {
				group.Middleware(empcap.EmpAuth())
				group.Bind(meController)
			})
			// 后台「通讯录管理」：走宿主会话鉴权与权限点（权限点见各接口 g.Meta），
			// 刻意不挂 EmpAuth——这里的访问者是管理员，不是员工作答端。
			group.Group("/", func(group pluginhost.RouteGroup) {
				group.Middleware(
					middlewares.Ctx(),
					middlewares.Auth(),
					middlewares.Tenancy(),
					middlewares.Permission(),
				)
				group.Bind(adminController)
			})
		})
	})
	return routes.Err()
}

// registerSyncJob 注册 Moka 驱动的员工目录全量同步 cron 任务。
// 间隔在注册时读取一次；凭据与映射在每次执行时重新读取，
// 使后端修改无需重启即可生效。
func registerSyncJob(ctx context.Context, registrar pluginhost.JobsRegistrar) error {
	services := registrar.Services()
	if services == nil || services.HostConfig() == nil {
		return gerror.New("linapro-employee-core 同步任务注册需要宿主机配置能力")
	}

	hc := services.HostConfig()
	enabled, err := hc.Bool(ctx, syncEnabledKey, true)
	if err != nil {
		return gerror.Wrapf(err, "linapro-employee-core: read config %s failed", syncEnabledKey)
	}
	// 开关关闭时不声明任务：宿主会把未声明的 builtin 任务行连同历史日志剪除，
	// 本地面板也就看不到这个任务（不是「已禁用」，而是不存在）。
	if !enabled {
		logger.Infof(ctx, "linapro-employee-core: %s=false，跳过员工目录同步任务注册", syncEnabledKey)
		return nil
	}

	intervalMin, err := hc.Int(ctx, syncIntervalKey, syncIntervalMinutes)
	if err != nil {
		return gerror.Wrapf(err, "linapro-employee-core: read config %s failed", syncIntervalKey)
	}
	if intervalMin <= 0 {
		intervalMin = syncIntervalMinutes
	}
	pattern := fmt.Sprintf("0 %d * * * 0", intervalMin)

	return registrar.AddWithMetadata(
		ctx,
		pattern,
		syncJobName,
		"员工目录全量同步",
		"定时从 Moka HCM 拉全量员工数据，飞书通讯录补 open_id，对账离职状态",
		func(jobCtx context.Context) error {
			// 宿主给每次任务执行套了 300 秒默认 deadline，全量同步（尤其关联组织
			// 递归枚举）可能超过它。此处解绑宿主 deadline，改用本插件自控的更长
			// 超时；WithoutCancel 保留 jobCtx 上的日志/追踪等值，只去掉其取消信号。
			syncCtx, cancel := context.WithTimeout(
				context.WithoutCancel(jobCtx),
				syncTimeoutMinutes*time.Minute,
			)
			defer cancel()

			if err := runSync(syncCtx, registrar); err != nil {
				logger.Errorf(syncCtx, "员工目录全量同步失败: %+v", err)
				return err
			}

			logger.Info(syncCtx, "员工目录全量同步完成")

			return nil
		},
	)
}

// runSync 执行一次完整同步周期。它从 sys_config 读取 dept_tenant_map，
// 为 Moka 和飞书 fetcher 构造适配器并运行 syncer。cron 任务与
// （未来的）手动触发都会调用它。
func runSync(ctx context.Context, registrar pluginhost.JobsRegistrar) error {
	services := registrar.Services()
	if services == nil || services.HostConfig() == nil {
		return gerror.New("员工目录同步: 宿主服务不可用")
	}
	hc := services.HostConfig()
	sc := hc.SysConfig()
	if sc == nil {
		return gerror.New("员工目录同步: sys_config 能力不可用")
	}

	info, err := sc.Get(ctx, deptTenantMapKey)
	if err != nil {
		return gerror.Wrapf(err, "员工目录同步: 读取配置 %s 失败", deptTenantMapKey)
	}
	if info == nil || info.Value == "" {
		return gerror.Newf("员工目录同步: %s 为空 — 中止", deptTenantMapKey)
	}
	var deptTenantMap map[string]int
	if err := json.Unmarshal([]byte(info.Value), &deptTenantMap); err != nil {
		return gerror.Wrapf(err, "员工目录同步: 解析配置 %s 失败", deptTenantMapKey)
	}

	logger.Debugf(ctx, "sys_config表获取的deptTenantMap:%+v", deptTenantMap)

	// 生产 MokaFetcher 适配器：从宿主机配置读取 Moka 凭据并包装 *hcm.Client，
	// 把 hcm.HCMEmployee 转换为 sync.MokaEmployee。
	mokaFetcher, err := newMokaFetcher(ctx, hc)
	if err != nil {
		return gerror.Wrap(err, "员工目录同步: 构建Moka数据源失败")
	}

	// 生产 LarkAppFetcher 适配器：lark_apps 中每个已配置的 lark_app_id
	// 对应一个。每个都枚举该飞书企业本租户的通讯录，
	// 使跨企业员工得到每个应用各一行的 lark_identity。
	larkFetchers, err := newLarkFetchers(ctx, hc)
	if err != nil {
		return gerror.Wrap(err, "员工目录同步: 构建飞书数据源失败")
	}

	syncer := sync.NewSyncer(sharedEmployee, mokaFetcher, larkFetchers)
	_, err = syncer.RunFullSync(ctx, deptTenantMap)
	return err
}
