// Package bodylimit 实现对外推送接口的按路由请求体上限中间件（FB-12）。
//
// 对外接口的请求体不参与签名，签名中间件不读请求体，上限只能在进入处理器前把关。处理器使用
// GoFrame 强类型签名，框架会在调用处理器前解析请求体；为了让超限请求得到 413 而不是框架内部
// 读取失败，本中间件先按路由上限读完请求体：
//   - multipart 请求：在上限内调用 ParseMultipartForm，文件部分全部留在内存、不产生临时文件，
//     框架随后的解析直接复用已解析的表单；
//   - 其它请求：读入内存后换成可重复读取的请求体，框架与处理器都能再次读取。
//
// 超限返回 413（LINAPRO_LARK_PUSH_BODY_TOO_LARGE），表单无法解析返回参数错误；响应体均由外层宿主
// 统一响应中间件包装。未登记规则的路由不设限、原样放行。
package bodylimit

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// Rule 是一条按路由路径后缀匹配的请求体上限。
type Rule struct {
	// PathSuffix 是路由路径后缀，如 /message/text。
	PathSuffix string
	// Limit 是请求体字节上限，必须大于 0。
	Limit int64
}

// Middleware 构造请求体上限中间件，按 rules 的顺序取第一条匹配的规则。
func Middleware(rules ...Rule) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		if r == nil {
			return
		}
		rule, ok := match(rules, r.URL.Path)
		if !ok {
			r.Middleware.Next()
			return
		}
		if err := readBody(r, rule.Limit); err != nil {
			r.SetError(larkpushcode.WithHTTPStatus(r, err))
			return
		}
		r.Middleware.Next()
	}
}
