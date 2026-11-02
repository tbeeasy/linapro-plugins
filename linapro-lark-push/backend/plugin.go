// Package backend 是 linapro-lark-push 能力插件的入口，被编译进宿主进程后经 init() 注册。
//
// 它做四件事：
//   - 装配发送编排（backend/internal/service/send）并绑定到公开门面 larkpushcap；
//   - 装配签名凭据服务（backend/internal/service/apikey），同一实例供后台管理接口与签名中间件使用；
//   - 注册后台管理路由组 /api/v1（「飞书推送凭据」页，挂宿主 Auth/Tenancy/Permission）；
//   - 注册对外 HTTP 路由组 /open/v1（自建 HMAC 签名鉴权，刻意不挂宿主
//     Auth/Tenancy/Permission——宿主没有机器身份认证机制）；
//   - 前置不可用（宿主配置能力缺失等）时降级为 nil 实现，但不阻断插件注册，
//     让门面能返回明确的「能力未就绪」而不是让消费方编译/装配失败。
package backend

import (
	"context"

	larkpushplugin "lina-plugin-linapro-lark-push"

	apikeycontroller "lina-plugin-linapro-lark-push/backend/internal/controller/apikey"
	pushcontroller "lina-plugin-linapro-lark-push/backend/internal/controller/push"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/bodylimit"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
	"lina-plugin-linapro-lark-push/backend/internal/service/apikey"
	"lina-plugin-linapro-lark-push/backend/internal/service/send"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"

	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/capability/cachecap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-core/pkg/plugin/pluginhost"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

const pluginID = "linapro-lark-push"

// 对外接口的请求体上限：文本 256 KB；文件 30 MB（飞书上限）+ 1 MB multipart 封装。
const (
	textBodyLimit = 256 << 10
	fileBodyLimit = 31 << 20
)

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(larkpushplugin.EmbeddedFiles)

	if err := plugin.HTTP().RegisterRoutes(
		pluginhost.ExtensionPointHTTPRouteRegister,
		pluginhost.CallbackExecutionModeBlocking,
		registerRoutes,
	); err != nil {
		panic(err)
	}

	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}

// registerRoutes 装配发送服务、绑定门面，并注册对外路由组。
func registerRoutes(ctx context.Context, registrar pluginhost.HTTPRegistrar) error {
	var (
		routes      = registrar.Routes()
		middlewares = routes.Middlewares()
		services    = registrar.Services()
	)

	var (
		hostConfig hostconfigcap.Service
		hostCache  cachecap.Service
	)
	if services != nil {
		hostConfig = services.HostConfig()
		// nonce 防重放复用宿主共享缓存：单机为进程内内存、集群为共享 KV，
		// 中间件不自建实例（见 .agents/rules/cache-consistency.md）。
		hostCache = services.Cache()
	}
	// 装配失败不阻断注册：门面返回「能力未就绪」，HTTP 侧返回 503。
	// 运行期依赖逐项显式传入：发送器工厂与寻址来源由装配层决定，send 组件不自行创建。
	// 告警通道独立装配、只依赖 alert 自己的配置块：push_groups 损坏或本组件装配失败时，
	// 它仍然能把失败原因发到飞书群。
	alerter, alerterErr := alert.New(
		hostConfig,
		hostCache,
		func(appID, appSecret string) alert.MessageSender { return larkim.NewClient(appID, appSecret) },
	)
	if alerterErr != nil {
		logger.Errorf(ctx, "lark-push: 构造告警通道失败，本次运行不发送告警: %v", alerterErr)
		alerter = nil
	}
	sender, err := send.New(
		hostConfig,
		func(appID, appSecret string) send.MessageSender { return larkim.NewClient(appID, appSecret) },
		empcap.LarkUserIDResolverByApp,
		alerter,
	)
	if err != nil {
		// 缺依赖属装配缺陷：记 ERROR 并降级为未就绪，不影响其它插件与宿主启动。
		logger.Errorf(ctx, "lark-push: 构造推送服务失败，门面降级为未就绪: %v", err)
		if alerter != nil {
			alerter.Notify(ctx, alert.Event{
				Kind:   alert.KindAssemblyFailed,
				Detail: err.Error(),
			})
		}
		sender = nil
	}
	larkpushcap.BindSender(sender)
	if alerter != nil && !alerter.Enabled(ctx) {
		// 告警未配置属预期状态：提示一次，不阻断、不反复刷日志。
		logger.Warningf(ctx, "lark-push: 未配置 %s，部署与数据缺陷只会写日志，不会推送到飞书群",
			alert.ConfigKey)
	}

	// 签名凭据服务只构造一次：后台管理接口与签名中间件共用，后者每次请求查库、不缓存。
	apiKeySvc := apikey.New(hostConfig)
	controller := pushcontroller.NewV1(sender)
	apiKeyController := apikeycontroller.NewV1(apiKeySvc)

	routes.Group(routes.APIPrefix(), func(group pluginhost.RouteGroup) {
		group.Group("/api/v1", func(group pluginhost.RouteGroup) {
			// 后台管理接口：走宿主会话鉴权与权限点（权限点见各接口 g.Meta）。
			group.Middleware(
				middlewares.NeverDoneCtx(),
				middlewares.HandlerResponse(),
				middlewares.CORS(),
				middlewares.RequestBodyLimit(),
				middlewares.Ctx(),
				middlewares.Auth(),
				middlewares.Tenancy(),
				middlewares.Permission(),
			)
			group.Bind(apiKeyController)
		})
		group.Group("/open/v1", func(group pluginhost.RouteGroup) {
			// 对外接口：只挂「上下文 / 统一响应 / 请求体上限 / 自建 HMAC 签名 / 按路由请求体上限」。
			// 刻意不挂 Auth/Tenancy/Permission：调用方是外部系统，没有宿主会话。
			// 响应体一律由宿主 HandlerResponse 包装为 code / message / data / errorCode（FB-12），
			// 签名中间件与处理器只返回错误或数据，不自行写 JSON。
			// 签名不读请求体；按路由上限（文本 256 KB、文件 31 MB）放在签名之后，未认证的请求不读请求体。
			group.Middleware(
				middlewares.NeverDoneCtx(),
				middlewares.HandlerResponse(),
				middlewares.RequestBodyLimit(),
			)
			group.Middleware(
				signature.Middleware(apiKeySvc, hostCache, alerter),
				bodylimit.Middleware(
					bodylimit.Rule{PathSuffix: "/message/text", Limit: textBodyLimit},
					bodylimit.Rule{PathSuffix: "/message/file", Limit: fileBodyLimit},
				),
			)
			group.Bind(controller)
		})
	})
	return routes.Err()
}
