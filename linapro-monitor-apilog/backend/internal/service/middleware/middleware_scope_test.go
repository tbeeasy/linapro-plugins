// middleware_scope_test.go 锁定排除名单的三种配置状态、接口请求判定与 D2 各条件的正反例。
//
// 保护对象是 design D2 的「键缺失 vs 显式空数组」区分：漏配一个键时若被当成空列表，
// linapro-lark-push 的消息正文就会静默开始落库——正是那种不报错的失效。

package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/guid"

	"lina-core/pkg/plugin/capability/bizctxcap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// fakeHostConfig 是宿主配置能力替身：内嵌接口满足方法集，只重写中间件用到的 Get；
// 调到其余方法会因内嵌 nil 而 panic，借此暴露新增的未预期依赖。
type fakeHostConfig struct {
	hostconfigcap.Service
	values map[string]any
}

// Get 返回配置键对应的值；不存在时返回 nil，模拟「键缺失」。
func (f *fakeHostConfig) Get(_ context.Context, key string, _ any) (*gvar.Var, error) {
	value, ok := f.values[key]
	if !ok {
		return nil, nil
	}
	return gvar.New(value), nil
}

// TestExcludeRouteOwnersFallsBackToDefaultWhenKeyMissing 锁定：键缺失时用内置默认值排除 lark-push。
func TestExcludeRouteOwnersFallsBackToDefaultWhenKeyMissing(t *testing.T) {
	svc := &serviceImpl{hostConfig: &fakeHostConfig{values: map[string]any{}}}

	owners, err := svc.excludeRouteOwners(context.Background())
	if err != nil {
		t.Fatalf("读取排除名单不应失败: %v", err)
	}
	if len(owners) != 1 || owners[0] != "linapro-lark-push" {
		t.Fatalf("键缺失时应回落到内置默认值 [linapro-lark-push]，实际为 %v", owners)
	}
	if !svc.isExcludedRouteOwner(context.Background(), "linapro-lark-push") {
		t.Fatal("键缺失时 lark-push 必须被排除")
	}
}

// TestExcludeRouteOwnersHonorsExplicitEmptyList 锁定：显式空数组表示全量记录。
func TestExcludeRouteOwnersHonorsExplicitEmptyList(t *testing.T) {
	svc := &serviceImpl{hostConfig: &fakeHostConfig{
		values: map[string]any{configKeyExcludeRouteOwners: []any{}},
	}}

	owners, err := svc.excludeRouteOwners(context.Background())
	if err != nil {
		t.Fatalf("读取排除名单不应失败: %v", err)
	}
	if len(owners) != 0 {
		t.Fatalf("显式 [] 时不应排除任何插件，实际为 %v", owners)
	}
	if svc.isExcludedRouteOwner(context.Background(), "linapro-lark-push") {
		t.Fatal("显式 [] 时 lark-push 应被记录")
	}
}

// TestExcludeRouteOwnersUsesExplicitList 锁定：显式列表照写执行。
func TestExcludeRouteOwnersUsesExplicitList(t *testing.T) {
	svc := &serviceImpl{hostConfig: &fakeHostConfig{
		values: map[string]any{configKeyExcludeRouteOwners: []any{"linapro-attend-poc"}},
	}}

	if !svc.isExcludedRouteOwner(context.Background(), "linapro-attend-poc") {
		t.Fatal("显式列表中的插件应被排除")
	}
	if svc.isExcludedRouteOwner(context.Background(), "linapro-lark-push") {
		t.Fatal("显式列表未包含 lark-push 时不应排除它：显式配置优先于内置默认")
	}
}

// TestIsExcludedPathMatchesPrefix 锁定路径排除按前缀匹配。
func TestIsExcludedPathMatchesPrefix(t *testing.T) {
	svc := &serviceImpl{hostConfig: &fakeHostConfig{
		values: map[string]any{configKeyExcludePaths: []any{"/x/healthz"}},
	}}
	if !svc.isExcludedPath(context.Background(), "/x/healthz/ping") {
		t.Fatal("前缀匹配应命中")
	}
	if svc.isExcludedPath(context.Background(), "/x/other") {
		t.Fatal("不相关路径不应命中")
	}
}

// TestIsServiceRequestRejectsNonHandler 锁定 OPTIONS 预检与没有服务处理器的请求不记录。
//
// 静态文件服务的处理器同样为空（宿主直接 serve 文件，不进处理器链），因此「处理器为空即跳过」
// 这一条同时覆盖了插件前端资源。
func TestIsServiceRequestRejectsNonHandler(t *testing.T) {
	optionsRequest := &ghttp.Request{Request: httptest.NewRequest(http.MethodOptions, "/x/api", nil)}
	if isServiceRequest(optionsRequest) {
		t.Fatal("OPTIONS 预检不是接口请求")
	}

	getRequest := &ghttp.Request{Request: httptest.NewRequest(http.MethodGet, "/x/api", nil)}
	if isServiceRequest(getRequest) {
		t.Fatal("没有服务处理器的请求不是接口请求")
	}
}

// fakeBizCtx 是后台业务上下文替身：固定返回一个登录用户名，空串表示未登录。
type fakeBizCtx struct {
	username string
}

// Current 返回只带用户名的上下文快照。
func (f fakeBizCtx) Current(_ context.Context) bizctxcap.CurrentContext {
	return bizctxcap.CurrentContext{Username: f.username}
}

// probeShouldRecord 起一个真实 ghttp 服务，在处理器内调用 shouldRecord 并返回判定结果。
//
// 「是接口请求」依赖 GoFrame 在路由分发时挂上的服务处理器，手工构造的 ghttp.Request 拿不到，
// 因此正例必须走一次真实分发。
func probeShouldRecord(t *testing.T, svc *serviceImpl, scope recordScope, path string) bool {
	t.Helper()
	server := ghttp.GetServer("apilog-should-record-" + guid.S())
	server.SetPort(0)
	server.SetDumpRouterMap(false)

	var recorded atomic.Bool
	server.BindHandler(path, func(r *ghttp.Request) {
		recorded.Store(svc.shouldRecord(r, scope))
		r.Response.Write("ok")
	})
	if err := server.Start(); err != nil {
		t.Fatalf("启动测试服务失败: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Shutdown(); err != nil {
			t.Fatalf("关闭测试服务失败: %v", err)
		}
	})

	response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", server.GetListenedPort(), path))
	if err != nil {
		t.Fatalf("请求测试服务失败: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("关闭响应体失败: %v", closeErr)
	}
	return recorded.Load()
}

// TestShouldRecordConditions 覆盖 D2 判定条件的正反例：每个反例只破坏一个条件，其余保持满足。
func TestShouldRecordConditions(t *testing.T) {
	const (
		ownerPlugin = "linapro-eval-360"
		probePath   = "/x/linapro-eval-360/api/v1/probe"
	)
	newSvc := func(username string, values map[string]any) *serviceImpl {
		return &serviceImpl{
			bizCtxSvc:  fakeBizCtx{username: username},
			hostConfig: &fakeHostConfig{values: values},
		}
	}

	cases := []struct {
		name   string
		svc    *serviceImpl
		scope  recordScope
		expect bool
	}{
		{
			name:   "全部条件满足时记录",
			svc:    newSvc("", map[string]any{}),
			scope:  recordScope{routeOwner: ownerPlugin},
			expect: true,
		},
		{
			name:   "归属插件为空（宿主自有路由）不记录",
			svc:    newSvc("", map[string]any{}),
			scope:  recordScope{routeOwner: ""},
			expect: false,
		},
		{
			name:   "归属插件只有空白时按空处理",
			svc:    newSvc("", map[string]any{}),
			scope:  recordScope{routeOwner: "  "},
			expect: false,
		},
		{
			name:   "有后台登录身份时交给 operlog，不记录",
			svc:    newSvc("admin", map[string]any{}),
			scope:  recordScope{routeOwner: ownerPlugin},
			expect: false,
		},
		{
			name:   "归属插件在排除名单里不记录",
			svc:    newSvc("", map[string]any{configKeyExcludeRouteOwners: []any{ownerPlugin}}),
			scope:  recordScope{routeOwner: ownerPlugin},
			expect: false,
		},
		{
			name:   "路径命中排除前缀不记录",
			svc:    newSvc("", map[string]any{configKeyExcludePaths: []any{"/x/linapro-eval-360/"}}),
			scope:  recordScope{routeOwner: ownerPlugin},
			expect: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeShouldRecord(t, tc.svc, tc.scope, probePath); got != tc.expect {
				t.Fatalf("shouldRecord 期望 %v，实际 %v", tc.expect, got)
			}
		})
	}
}
