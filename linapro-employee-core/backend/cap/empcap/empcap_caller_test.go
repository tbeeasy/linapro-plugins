// empcap_caller_test.go 锁定调用方标识约定：键名与 JSON 结构。
//
// 保护对象是「约定漂移」：日志插件读的是同名字符串键，两边一旦不一致，
// 后果不是报错而是 caller_* 五列静默为空，事后无法区分「写入方没写」与「键拼错了」。

package empcap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
)

// TestCallerCtxKeyValueIsStable 锁定键名等于日志插件读取的约定值。
func TestCallerCtxKeyValueIsStable(t *testing.T) {
	if CallerCtxKey != "linapro.request.caller" {
		t.Fatalf("约定键名必须为 linapro.request.caller，实际为 %q", CallerCtxKey)
	}
}

// TestSetCallerWritesDecodablePayload 锁定序列化结果可反序列化回等价结构。
func TestSetCallerWritesDecodablePayload(t *testing.T) {
	request := &ghttp.Request{Request: httptest.NewRequest(http.MethodGet, "/x/emp/me", nil)}

	SetCaller(request, EmpContext{EmpID: 5, TenantID: 1}, "张三", "GZ001198")

	raw := request.GetCtxVar(CallerCtxKey).String()
	if raw == "" {
		t.Fatal("SetCaller 必须把约定变量写进请求上下文")
	}
	var payload struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Tenant int    `json:"tenant"`
		Name   string `json:"name"`
		No     string `json:"no"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("约定变量必须是合法 JSON，实际为 %q: %v", raw, err)
	}
	if payload.Kind != CallerKindEmp {
		t.Fatalf("kind 应为 %q，实际为 %q", CallerKindEmp, payload.Kind)
	}
	// id 用字符串承载 EmpID：避免 64 位整数在 JavaScript 侧丢精度。
	if payload.ID != "5" {
		t.Fatalf("id 应为 EmpID 的字符串形式 %q，实际为 %q", "5", payload.ID)
	}
	if payload.Tenant != 1 {
		t.Fatalf("tenant 应为 1，实际为 %d", payload.Tenant)
	}
	if payload.Name != "张三" {
		t.Fatalf("name 应为张三，实际为 %q", payload.Name)
	}
	if payload.No != "GZ001198" {
		t.Fatalf("no 应为 GZ001198，实际为 %q", payload.No)
	}
}

// TestSetCallerOmitsEmptyFields 锁定拿不到姓名与工号时省略字段，读取侧对应列为空。
func TestSetCallerOmitsEmptyFields(t *testing.T) {
	request := &ghttp.Request{Request: httptest.NewRequest(http.MethodGet, "/x/emp/me", nil)}

	SetCaller(request, EmpContext{EmpID: 5, TenantID: 0}, "", "")

	raw := request.GetCtxVar(CallerCtxKey).String()
	if raw != `{"kind":"emp","id":"5"}` {
		t.Fatalf("空字段应被省略，实际为 %q", raw)
	}
}

// TestSetCallerIgnoresNilRequest 锁定空请求不 panic：写入方不应因为写日志而失败。
func TestSetCallerIgnoresNilRequest(t *testing.T) {
	SetCaller(nil, EmpContext{EmpID: 5}, "张三", "GZ001198")
}
