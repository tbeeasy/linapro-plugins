// larkpushcode_http_test.go 覆盖对外接口「错误 → HTTP 状态码」映射（FB-12）。

package larkpushcode

import (
	"net/http"
	"testing"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"
)

// TestHTTPStatus 验证只有请求体超限为 413、服务端能力不可用为 503、配置问题与未归类错误为 500，
// 参数错误、签名类错误与部分投递失败一律 200（由 code / errorCode 区分）。
func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"无错误", nil, http.StatusOK},
		{"参数错误", bizerr.NewCode(CodeInvalidParam, bizerr.P("item", "x")), http.StatusOK},
		{"缺签名头", bizerr.NewCode(CodeSignatureMissing), http.StatusOK},
		{"未知 app_key", bizerr.NewCode(CodeSignatureUnknownAppKey), http.StatusOK},
		{"时间戳无效", bizerr.NewCode(CodeSignatureTimestampInvalid), http.StatusOK},
		{"签名不匹配", bizerr.NewCode(CodeSignatureMismatch), http.StatusOK},
		{"nonce 重放", bizerr.NewCode(CodeSignatureNonceReplayed), http.StatusOK},
		{"部分投递失败", bizerr.NewCode(CodeSendPartialFailed, bizerr.P("count", 1)), http.StatusOK},
		{"请求体超限", bizerr.NewCode(CodeBodyTooLarge, bizerr.P("limit", "256 KB")), http.StatusRequestEntityTooLarge},
		{"发送实现未就绪", bizerr.NewCode(CodeSenderUnbound), http.StatusServiceUnavailable},
		{"员工能力未就绪", bizerr.NewCode(CodeEmpcapUnbound), http.StatusServiceUnavailable},
		{"宿主配置不可用", bizerr.NewCode(CodeHostConfigMissing), http.StatusServiceUnavailable},
		{"凭据表不可用", bizerr.NewCode(CodeCredentialStoreUnavailable), http.StatusServiceUnavailable},
		{"去重存储不可用", bizerr.NewCode(CodeNonceStoreUnavailable), http.StatusServiceUnavailable},
		{"分组无推送应用", bizerr.NewCode(CodeNoPushApp, bizerr.P("group", "g")), http.StatusInternalServerError},
		{"推送配置非法", bizerr.NewCode(CodeConfigInvalid, bizerr.P("item", "x")), http.StatusInternalServerError},
		{"包装后的业务错误", gerror.Wrap(bizerr.NewCode(CodeSignatureMismatch), "外层"), http.StatusOK},
		{"非业务错误", gerror.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := httpStatus(tc.err); got != tc.want {
			t.Errorf("%s: want %d, got %d", tc.name, tc.want, got)
		}
	}
}
