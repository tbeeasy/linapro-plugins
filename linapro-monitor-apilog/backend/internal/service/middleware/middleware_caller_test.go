// middleware_caller_test.go 锁定调用方约定键的取值与拆分存储：未写入、完整、部分缺失、解析失败
// 四种情形都不能丢记录，只影响对应的 caller_* 列。

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
)

// newCallerRequest 构造一个带（或不带）调用方约定键的请求。raw 为空串时不写入该键。
func newCallerRequest(raw string) *ghttp.Request {
	request := &ghttp.Request{Request: httptest.NewRequest(http.MethodGet, "/x/api", nil)}
	if raw != "" {
		request.SetCtxVar(callerCtxKey, raw)
	}
	return request
}

// TestCallerCtxKeyValue 锁定约定键名：写入方持有同名副本，两边任一漂移都会让 caller_* 静默为空。
func TestCallerCtxKeyValue(t *testing.T) {
	if callerCtxKey != "linapro.request.caller" {
		t.Fatalf("约定键名必须为 linapro.request.caller，实际为 %q", callerCtxKey)
	}
}

// TestParseCallerSplitsFields 覆盖四种写入情形。
func TestParseCallerSplitsFields(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		expect callerFields
	}{
		{
			name:   "未写入时各列为空",
			raw:    "",
			expect: callerFields{},
		},
		{
			name: "完整写入时逐列拆分",
			raw:  `{"kind":"emp","id":"1840","tenant":1,"name":"刘鹏","no":"GZ000001"}`,
			expect: callerFields{
				Kind: "emp", ID: "1840", Tenant: 1, Name: "刘鹏", No: "GZ000001",
			},
		},
		{
			name:   "外部系统只写 kind 与 id 时其余列为空",
			raw:    `{"kind":"app_key","id":"ak_demo"}`,
			expect: callerFields{Kind: "app_key", ID: "ak_demo"},
		},
		{
			name:   "解析失败时各列留空且不 panic",
			raw:    `{"kind":"emp",`,
			expect: callerFields{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCaller(newCallerRequest(tc.raw)); got != tc.expect {
				t.Fatalf("parseCaller 期望 %+v，实际 %+v", tc.expect, got)
			}
		})
	}
}
