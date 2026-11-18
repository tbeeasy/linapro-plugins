// Package bindlog 补记对外推送接口「请求体绑定失败」的调用方上下文。
//
// 对外接口使用 GoFrame 强类型签名，框架会在调用控制器之前把请求体绑定到 DTO；字段结构对不上时
// （如 recipients 传成字符串、数组元素不是对象）绑定直接失败，控制器根本不会执行。这类失败既不是
// 签名中间件拒绝的，也没走到 finishSend，现有日志里只剩框架自己那条堆栈——看不到请求体、路径与
// 凭据标识，无法和调用方对账。本中间件只补这一段：按非 bizerr 判定为框架层失败后，记录
// request_id、路径、凭据标识与请求体摘要。
//
// 挂在签名与按路由上限之后：前者保证凭据标识可取，后者保证请求体已读入且可重复读取。
package bindlog

import (
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"

	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
)

// Middleware 构造请求体绑定失败日志中间件。
func Middleware() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		if r == nil {
			return
		}
		r.Middleware.Next()
		err := r.GetError()
		if !shouldLog(err) {
			return
		}
		// 签名已通过，凭据标识可安全进日志（不含 secret 与签名原文）。
		identity, _ := signature.FromContext(r.Context())
		ctx := r.Context()
		logger.Warningf(ctx,
			"lark-push http: 请求体绑定失败（控制器未执行） request_id=%s method=%s path=%s content_type=%s content_length=%d key_id=%s app_key=%s err=%v 请求体=%s",
			gctx.CtxId(ctx), r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.ContentLength,
			identity.KeyID, identity.AppKey, err, summarizeBody(r))
	}
}

// shouldLog 报告是否为此处负责的失败：只记录非 bizerr 的框架层错误。
//
// 签名类错误、请求体超限、控制器返回的参数/投递错误都是 bizerr，各有自己的日志与响应错误码，
// 在这里重复记录只会淹没有用信息。服务端侧少数非 bizerr 错误（如推送配置读取失败）也会命中
// 本规则，多一条带请求体的日志，代价可接受。
func shouldLog(err error) bool {
	if err == nil {
		return false
	}
	_, ok := bizerr.As(err)
	return !ok
}
