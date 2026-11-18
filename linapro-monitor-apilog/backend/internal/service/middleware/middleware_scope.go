// middleware_scope.go 实现「该不该记录」的判定，以及路由归属与路由模式的解析。
//
// 五个条件必须全部满足才记录（见 design D2）：
//  1. 路由归属某个插件；
//  2. 请求上下文里没有后台登录用户名；
//  3. 是接口请求（有服务处理器，不是静态文件服务，不是 OPTIONS 预检）；
//  4. 归属插件不在 exclude_route_owners 里；
//  5. 路径不在 exclude_paths 里。

package middleware

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/gconv"

	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/capability/routecap"
	"lina-core/pkg/plugin/pluginhost"
)

// 配置键。排除名单每次请求读取（宿主配置在内存里，成本可忽略），改配置无需重启。
const (
	// configKeyExcludeRouteOwners 是按归属插件排除的名单。
	configKeyExcludeRouteOwners = "plugin.linapro-monitor-apilog.exclude_route_owners"
	// configKeyExcludePaths 是按路径排除的名单。
	configKeyExcludePaths = "plugin.linapro-monitor-apilog.exclude_paths"
	// configKeyMaskFields 是追加脱敏的字段名。
	configKeyMaskFields = "plugin.linapro-monitor-apilog.mask_fields"
)

// defaultExcludeRouteOwners 是 exclude_route_owners 键缺失时的内置默认值。
//
// 键缺失时回落到它而不是当成空列表：漏抄一个键不能让 linapro-lark-push 的消息正文
// 静默开始落库——该插件已明确决定不保存正文。显式配置 `[]` 才是管理员有意的全量记录。
var defaultExcludeRouteOwners = []string{"linapro-lark-push"}

// resolveScope 解析本次请求的记录范围与判定结果。
func (s *serviceImpl) resolveScope(request *ghttp.Request) recordScope {
	scope := recordScope{}

	// 动态路由元数据在分发时才挂到请求上下文上，因此必须在这里（Next() 之后）读取。
	var dynamicMeta *routecap.Metadata
	if s.routeMetaSvc != nil {
		dynamicMeta = s.routeMetaSvc.GetMetadata(request.GetCtx())
	}
	scope.routeOwner, scope.routePath, scope.responseBody, scope.responseContentType =
		resolveRouteFields(request, dynamicMeta)

	if !s.shouldRecord(request, scope) {
		return scope
	}
	scope.record = true
	return scope
}

// resolveRouteFields 解析归属插件、路由模式与动态路由的响应兜底。
func resolveRouteFields(
	request *ghttp.Request,
	dynamicMeta *routecap.Metadata,
) (routeOwner, routePath, responseBody, responseContentType string) {
	if handler := request.GetServeHandler(); handler != nil {
		// 源码插件：宿主在请求上下文里标记了归属插件 ID。
		routeOwner = pluginhost.SourcePluginIDFromRequest(request)
		if handler.Handler != nil && handler.Handler.Router != nil {
			// 路由模式（如 /task/{id}）便于按接口聚合；取不到时下方回退到真实路径。
			routePath = handler.Handler.Router.Uri
		}
	}
	if dynamicMeta != nil {
		// 动态插件：元数据只暴露真实公开路径（不是模板），因此 route_path 直接等于 path。
		if strings.TrimSpace(dynamicMeta.PluginID) != "" {
			routeOwner = dynamicMeta.PluginID
		}
		routePath = dynamicMeta.PublicPath
		responseBody = dynamicMeta.ResponseBody
		responseContentType = dynamicMeta.ResponseContentType
	}
	if strings.TrimSpace(routePath) == "" && request.URL != nil {
		routePath = request.URL.Path
	}
	return routeOwner, routePath, responseBody, responseContentType
}

// shouldRecord 按 D2 的五个条件依次判定。
func (s *serviceImpl) shouldRecord(request *ghttp.Request, scope recordScope) bool {
	// 1. 归属插件：宿主自有路由不记录（宿主登录等由 loginlog 负责）。
	if strings.TrimSpace(scope.routeOwner) == "" {
		return false
	}
	// 2. 没有后台登录身份：有身份的那部分由 operlog 负责。
	if strings.TrimSpace(s.currentUsername(request.Context())) != "" {
		return false
	}
	// 3. 是接口请求：静态文件服务与 OPTIONS 预检不记录。
	if !isServiceRequest(request) {
		return false
	}
	// 4. 归属插件不在排除名单里。
	if s.isExcludedRouteOwner(request.Context(), scope.routeOwner) {
		return false
	}
	// 5. 路径不在排除名单里。
	return !s.isExcludedPath(request.Context(), request.URL.Path)
}

// currentUsername 读取后台登录用户名。
func (s *serviceImpl) currentUsername(ctx context.Context) string {
	if s == nil || s.bizCtxSvc == nil {
		return ""
	}
	return s.bizCtxSvc.Current(ctx).Username
}

// isServiceRequest 判定请求是否由接口处理器处理。
//
// 静态文件服务没有服务处理器，因此 `GetServeHandler()` 为 nil；插件前端资源请求走的就是这类路径。
// 中间件与钩子类型同样不是接口处理器。
func isServiceRequest(request *ghttp.Request) bool {
	if request.Method == http.MethodOptions {
		return false
	}
	handler := request.GetServeHandler()
	if handler == nil || handler.Handler == nil {
		return false
	}
	switch handler.Handler.Type {
	case ghttp.HandlerTypeHandler, ghttp.HandlerTypeObject:
		return true
	default:
		return false
	}
}

// isExcludedRouteOwner 判定归属插件是否在排除名单里。
func (s *serviceImpl) isExcludedRouteOwner(ctx context.Context, routeOwner string) bool {
	owners, err := s.excludeRouteOwners(ctx)
	if err != nil {
		// 配置读失败时保守处理：按内置默认名单排除，避免把本不该记录的插件正文写进库。
		logger.Warningf(ctx, "linapro-monitor-apilog：读取 %s 失败，回落到内置默认名单: %v", configKeyExcludeRouteOwners, err)
		owners = defaultExcludeRouteOwners
	}
	return slices.Contains(owners, routeOwner)
}

// excludeRouteOwners 读取排除名单，区分「键缺失」与「显式空数组」。
//
// 键缺失（配置值返回 nil）回落到内置默认值；显式 `[]` 照写执行，即全量记录。
func (s *serviceImpl) excludeRouteOwners(ctx context.Context) ([]string, error) {
	if s == nil || s.hostConfig == nil {
		return defaultExcludeRouteOwners, nil
	}
	value, err := s.hostConfig.Get(ctx, configKeyExcludeRouteOwners, nil)
	if err != nil {
		return nil, err
	}
	if value == nil || value.IsNil() {
		return defaultExcludeRouteOwners, nil
	}
	owners := make([]string, 0)
	if convErr := gconv.Structs(value.Val(), &owners); convErr != nil {
		return nil, convErr
	}
	return owners, nil
}

// isExcludedPath 判定路径是否在排除名单里（前缀匹配）。
func (s *serviceImpl) isExcludedPath(ctx context.Context, path string) bool {
	paths := s.stringListConfig(ctx, configKeyExcludePaths)
	for _, prefix := range paths {
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// stringListConfig 读取一个字符串数组配置。缺失或解析失败一律视为空列表。
//
// 只用于 exclude_paths 与 mask_fields：它们的默认值就是空，缺失与 `[]` 效果相同，
// 也不涉及隐私，因此不做 D2 那种「缺失 vs 显式空」的区分。
func (s *serviceImpl) stringListConfig(ctx context.Context, key string) []string {
	if s == nil || s.hostConfig == nil {
		return nil
	}
	value, err := s.hostConfig.Get(ctx, key, nil)
	if err != nil {
		logger.Warningf(ctx, "linapro-monitor-apilog：读取 %s 失败，按空列表处理: %v", key, err)
		return nil
	}
	if value == nil || value.IsNil() {
		return nil
	}
	items := make([]string, 0)
	if convErr := gconv.Structs(value.Val(), &items); convErr != nil {
		logger.Warningf(ctx, "linapro-monitor-apilog：解析 %s 失败，按空列表处理: %v", key, convErr)
		return nil
	}
	return items
}
