// larkpushcode_http.go 维护对外 HTTP 接口「错误 → HTTP 状态码」的映射（FB-12）。
//
// 对外接口的响应体统一由宿主 HandlerResponse 中间件包装为 code / message / data / errorCode，
// 调用方以 code 与 errorCode 区分结果。HTTP 状态码只保留三类：
//   - 413：请求体超限；
//   - 503：服务端能力不可用（发送实现、员工能力、宿主配置、凭据表、去重存储）；
//   - 500：服务端推送配置有问题，以及其它未归类的内部错误；
//
// 其余结果（参数错误、签名类错误、部分投递失败、成功）一律 200。签名中间件与控制器共用本映射，
// 保证同一错误码在任何入口返回同一状态码。

package larkpushcode

import (
	"net/http"

	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/bizerr"
)

// okStatusCodes 是以 HTTP 200 返回、只靠 code / errorCode 区分的错误码：调用方修正请求或
// 按失败明细重试即可，不属于服务端故障。
var okStatusCodes = []*bizerr.Code{
	CodeInvalidParam,
	CodeSignatureMissing,
	CodeSignatureUnknownAppKey,
	CodeSignatureTimestampInvalid,
	CodeSignatureMismatch,
	CodeSignatureNonceReplayed,
	CodeSendPartialFailed,
}

// unavailableCodes 是以 HTTP 503 返回的错误码：服务端依赖暂不可用，调用方可稍后重试。
var unavailableCodes = []*bizerr.Code{
	CodeSenderUnbound,
	CodeEmpcapUnbound,
	CodeHostConfigMissing,
	CodeCredentialStoreUnavailable,
	CodeNonceStoreUnavailable,
}

// WithHTTPStatus 按 err 设置响应的 HTTP 状态码（200 时不设置）并原样返回 err。
//
// 只设置状态码、不写响应体：响应体由外层宿主统一响应中间件按 err 包装为 code / message / errorCode。
// 控制器以 `return nil, larkpushcode.WithHTTPStatus(r, err)` 返回错误；中间件以
// `r.SetError(larkpushcode.WithHTTPStatus(r, err))` 拒绝请求且不调用 Next。
func WithHTTPStatus(r *ghttp.Request, err error) error {
	if r == nil || err == nil {
		return err
	}
	if status := httpStatus(err); status != http.StatusOK {
		r.Response.WriteHeader(status)
	}
	return err
}

// httpStatus 返回对外接口在出现 err 时应使用的 HTTP 状态码；err 为 nil 时返回 200。
// 不在任何清单中的错误（含非 bizerr 的内部错误、推送配置错误）按 500 处理。
func httpStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	be, ok := bizerr.As(err)
	if !ok {
		return http.StatusInternalServerError
	}
	switch {
	case be.Matches(CodeBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	case matchesAny(be, okStatusCodes):
		return http.StatusOK
	case matchesAny(be, unavailableCodes):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// matchesAny 报告 be 是否命中 codes 中任一错误码。
func matchesAny(be *bizerr.Error, codes []*bizerr.Code) bool {
	for _, code := range codes {
		if be.Matches(code) {
			return true
		}
	}
	return false
}
