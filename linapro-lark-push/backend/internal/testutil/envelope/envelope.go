// Package envelope 提供测试用的统一响应中间件替身，供插件内需要起 HTTP 服务的单元测试使用。
//
// 生产路由的响应体由宿主 HandlerResponse 中间件（lina-core/internal，插件不可直接引用）包装为
// code / message / data / errorCode。本替身按同样的字段与规则输出，让签名中间件与控制器在单测中
// 走与生产一致的「只返回 (res, err) / SetError，由外层统一写响应体」路径。仅供测试使用。
package envelope

import (
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/bizerr"
)

// Response 是统一响应结构，字段与宿主 HandlerResponse 输出一致。
type Response struct {
	// Code 是 GoFrame 错误大类，0 表示成功。
	Code int `json:"code"`
	// Message 是展示文案。
	Message string `json:"message"`
	// Data 是接口返回数据。
	Data any `json:"data"`
	// ErrorCode 是稳定的机器可读错误码，成功时省略。
	ErrorCode string `json:"errorCode,omitempty"`
}

// Middleware 返回统一响应中间件替身：处理链已写出内容时不再包装，否则按处理器结果与错误输出 JSON。
func Middleware() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		r.Middleware.Next()
		if r.Response.BufferLength() > 0 || r.Response.BytesWritten() > 0 {
			return
		}
		res := Response{Code: gcode.CodeOK.Code(), Message: gcode.CodeOK.Message(), Data: r.GetHandlerResponse()}
		if err := r.GetError(); err != nil {
			code := gerror.Code(err)
			if code == gcode.CodeNil {
				code = gcode.CodeInternalError
			}
			res.Code = code.Code()
			res.Message = err.Error()
			if be, ok := bizerr.As(err); ok {
				res.ErrorCode = be.RuntimeCode()
			}
		}
		r.Response.WriteJson(res)
	}
}
