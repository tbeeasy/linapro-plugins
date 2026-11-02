// signature_alert_test.go 覆盖签名中间件的告警边界：只有服务端基础设施不可用才告警，
// 调用方侧签名错误一律不告警。

package signature

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/plugin/capability/cachecap"

	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
	"lina-plugin-linapro-lark-push/backend/internal/service/apikey"
	"lina-plugin-linapro-lark-push/backend/internal/testutil/envelope"
)

// fakeAlerter 是 alert.Service 的替身：记录收到的告警事件。
type fakeAlerter struct {
	events *[]alert.Event
}

func (f *fakeAlerter) Notify(_ context.Context, ev alert.Event) {
	*f.events = append(*f.events, ev)
}

func (f *fakeAlerter) Enabled(context.Context) bool { return true }

// newAlerter 返回一个记录告警事件的假通道。
func newAlerter() (*fakeAlerter, *[]alert.Event) {
	events := &[]alert.Event{}
	return &fakeAlerter{events: events}, events
}

// newAlertingTestServer 与 newTestServer 相同，但注入告警通道，用于观察是否触发告警。
func newAlertingTestServer(
	t *testing.T,
	keys apikey.Service,
	cache cachecap.Service,
	alerter alert.Service,
) *testServer {
	t.Helper()
	s := ghttp.GetServer("signature-alert-test-" + t.Name())
	s.SetAddr("127.0.0.1:0")
	s.SetDumpRouterMap(false)
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(envelope.Middleware(), Middleware(keys, cache, alerter))
		group.GET("/probe", func(r *ghttp.Request) { r.Response.Write("ok") })
	})
	if err := s.Start(); err != nil {
		t.Fatalf("启动测试服务: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	return &testServer{URL: "http://" + s.GetListenedAddress()}
}

// TestMiddleware_AlertsCredentialStoreUnavailable 验证凭据服务未装配时告警一次：
// 这是服务端 fail-closed，所有对外请求都会被拒。
func TestMiddleware_AlertsCredentialStoreUnavailable(t *testing.T) {
	alerter, events := newAlerter()
	srv := newAlertingTestServer(t, nil, newFakeCache(), alerter)

	send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("a1"), time.Now()))

	if len(*events) != 1 || (*events)[0].Kind != alert.KindCredentialStoreUnavailable {
		t.Fatalf("应告警 KindCredentialStoreUnavailable 一次, got %+v", *events)
	}
}

// TestMiddleware_AlertsNonceStoreUnavailable 验证缓存未装配时告警一次。
func TestMiddleware_AlertsNonceStoreUnavailable(t *testing.T) {
	alerter, events := newAlerter()
	srv := newAlertingTestServer(t, entries("k1", testSecret, "ops-alert"), nil, alerter)

	send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("a2"), time.Now()))

	if len(*events) != 1 || (*events)[0].Kind != alert.KindNonceStoreUnavailable {
		t.Fatalf("应告警 KindNonceStoreUnavailable 一次, got %+v", *events)
	}
}

// TestMiddleware_AlertsCredentialLookupFailure 验证查库失败时告警（基础设施故障，非调用方问题）。
func TestMiddleware_AlertsCredentialLookupFailure(t *testing.T) {
	alerter, events := newAlerter()
	keys := entries("k1", testSecret, "ops-alert")
	keys.err = context.DeadlineExceeded
	srv := newAlertingTestServer(t, keys, newFakeCache(), alerter)

	send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("a3"), time.Now()))

	if len(*events) != 1 || (*events)[0].Kind != alert.KindCredentialStoreUnavailable {
		t.Fatalf("应告警 KindCredentialStoreUnavailable, got %+v", *events)
	}
}

// TestMiddleware_NoAlertForCallerErrors 验证调用方侧签名错误不告警：这些会被扫描器反复
// 触发，告警只会淹没真正的故障。
func TestMiddleware_NoAlertForCallerErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(req *http.Request)
		appKey string
		secret string
	}{
		{name: "缺签名头", mutate: func(r *http.Request) { r.Header.Del(HeaderSignature) }, appKey: "k1", secret: testSecret},
		{name: "app_key 未知", appKey: "k9", secret: testSecret},
		{name: "签名不匹配", appKey: "k1", secret: "wrong-secret"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alerter, events := newAlerter()
			srv := newAlertingTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache(), alerter)

			req := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", c.appKey, c.secret, validNonce("a4"), time.Now())
			if c.mutate != nil {
				c.mutate(req)
			}
			send(t, req)

			if len(*events) != 0 {
				t.Fatalf("调用方侧错误不应告警, got %+v", *events)
			}
		})
	}
}

// TestMiddleware_NoAlertOnSuccess 验证正常请求不告警。
func TestMiddleware_NoAlertOnSuccess(t *testing.T) {
	alerter, events := newAlerter()
	srv := newAlertingTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache(), alerter)

	code, _, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("a5"), time.Now()))
	if code != http.StatusOK {
		t.Fatalf("正常请求应 200, got %d", code)
	}
	if len(*events) != 0 {
		t.Fatalf("成功请求不应告警, got %+v", *events)
	}
}
