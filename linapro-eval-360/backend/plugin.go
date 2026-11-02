// Package backend 是 linapro-eval-360 源插件入口。
// 它被编译进宿主进程，经 init() 注册。
//
// 骨架阶段仅注册两侧 HTTP 路由组：
//   - 管理端 /api/v1/eval/admin/*：挂宿主 Auth()/Tenancy()/Permission()，复用 sys_user 身份；
//   - 员工端 /api/v1/eval/emp/*：挂 empcap.EmpAuth()（复用 linapro-employee-core 的员工认证），
//     不挂宿主 Auth()/Permission()。
//
// 数据表、service、飞书推送（larkim）、导出等由后续任务补齐。
package backend

import (
	"context"

	eval360plugin "lina-plugin-linapro-eval-360"
	empcontroller "lina-plugin-linapro-eval-360/backend/internal/controller/emp"
	evalcontroller "lina-plugin-linapro-eval-360/backend/internal/controller/eval"
	esvc "lina-plugin-linapro-eval-360/backend/internal/service/employee"
	expSvc "lina-plugin-linapro-eval-360/backend/internal/service/export"
	psvc "lina-plugin-linapro-eval-360/backend/internal/service/project"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
	rsvc "lina-plugin-linapro-eval-360/backend/internal/service/relation"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-core/pkg/plugin/pluginhost"
)

const pluginID = "linapro-eval-360"

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(eval360plugin.EmbeddedFiles)

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

// registerRoutes 在插件 API 前缀下注册管理端与员工端两个路由组。
func registerRoutes(ctx context.Context, registrar pluginhost.HTTPRegistrar) error {
	var (
		routes      = registrar.Routes()
		middlewares = routes.Middlewares()
		services    = registrar.Services()
	)

	// 本插件为平台级：问卷对所有租户员工通用，不做租户隔离，故不依赖宿主租户能力。
	questionnaireSvc := qsvc.New()
	relationSvc := rsvc.New()
	// 项目服务注入 questionnaire 与推送编排 Sender。
	// 推送编排属第 8 节：评价者工号经 linapro-lark-push（larkpushcap）按企业扇出投递
	// 飞书模板卡片，推送应用凭据与寻址都归 lark-push；
	// 一评价者一张卡、全部评价者送达才发布、失败清单按评价者工号粒度。
	// newNotifySender 在 empcap 未绑定或 hostConfig 不可用时返回 nil（不阻断路由注册）。
	var hostConfig hostconfigcap.Service
	if services != nil {
		hostConfig = services.HostConfig()
	}
	sender := newNotifySender(ctx, hostConfig)
	projectSvc := psvc.New(questionnaireSvc, sender)
	exportSvc := expSvc.New()
	// 员工端作答服务：注入 questionnaire（取 live 结构），empcap 经 BoundEmployeeService 懒解析。
	employeeSvc := esvc.New(questionnaireSvc)

	routes.Group(routes.APIPrefix(), func(group pluginhost.RouteGroup) {
		group.Group("/api/v1/eval", func(group pluginhost.RouteGroup) {
			// 管理端：宿主 Auth/Tenancy/Permission 链，复用 sys_user 身份与声明式权限。
			group.Group("/admin", func(group pluginhost.RouteGroup) {
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
				group.Bind(evalcontroller.NewV1(questionnaireSvc, relationSvc, projectSvc, exportSvc))
			})

			// 员工端：挂 empcap.EmpAuth()（自研员工认证），刻意不挂宿主 Auth/Permission；
			// 身份经 EmpAuth 注入 EmpContext，由控制器自行读取。
			group.Group("/emp", func(group pluginhost.RouteGroup) {
				group.Middleware(
					middlewares.NeverDoneCtx(),
					middlewares.HandlerResponse(),
					middlewares.CORS(),
					middlewares.RequestBodyLimit(),
					empcap.EmpAuth(),
				)
				group.Bind(empcontroller.NewV1(employeeSvc))
			})
		})
	})
	return nil
}
