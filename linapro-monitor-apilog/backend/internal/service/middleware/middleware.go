// Package middleware 实现 linapro-monitor-apilog 的全局请求记录中间件。
//
// 记录范围是「路由归属插件、且没有后台登录用户名」的接口请求：有后台身份的请求由
// linapro-monitor-operlog 负责，本插件不重复记录（见 design D2）。
//
// 记录的判定与读取全部发生在 r.Middleware.Next() 返回之后，因此不记录的请求（后台管理、
// 静态资源、宿主自有路由）零开销：它们既不会被包装 Body，也不会被拷贝。
package middleware

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/plugin/capability/bizctxcap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-core/pkg/plugin/capability/routecap"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
)

// Service 定义本插件的中间件服务契约。
type Service interface {
	// Record 捕获一次已完成的请求，并在命中记录条件时异步落库。
	Record(request *ghttp.Request)
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的实现。
type serviceImpl struct {
	routeMetaSvc routecap.Service      // 动态路由元数据读取
	bizCtxSvc    bizctxcap.Service     // 后台登录用户名读取（用于排除后台请求）
	hostConfig   hostconfigcap.Service // 排除名单、脱敏追加字段的配置读取
	apiLogSvc    apilogsvc.Service     // 插件自己的记录写入服务
}

// New 构造中间件服务。依赖全部由宿主插件注册回调解构后传入。
func New(
	routeMetaSvc routecap.Service,
	bizCtxSvc bizctxcap.Service,
	hostConfig hostconfigcap.Service,
	apiLogSvc apilogsvc.Service,
) Service {
	return &serviceImpl{
		routeMetaSvc: routeMetaSvc,
		bizCtxSvc:    bizCtxSvc,
		hostConfig:   hostConfig,
		apiLogSvc:    apiLogSvc,
	}
}
