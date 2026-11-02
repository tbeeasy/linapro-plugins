// empcap_auth.go 实现 EmpAuth：自研的员工认证中间件，由消费方路由组
// 挂载，用于替代宿主机 Auth()/Permission() 链。每个请求它会
// (1) 校验员工 JWT，并 (2) 重新读取员工状态以实现实时离职拦截 ——
// 离职员工在下一个请求就会被拒绝，与 token 是否过期无关（见 ADR-0002）。
// 绑定的服务按请求懒解析，因此插件 init() 顺序无关紧要。

package empcap

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// bearerPrefix 是承载员工 JWT 的 Authorization 方案。
const bearerPrefix = "Bearer "

// EmpAuth 返回员工认证中间件。将它挂载到员工路由组；
// 切勿同时挂载宿主机 Auth()/Permission() 链。
func EmpAuth() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		raw := strings.TrimSpace(r.Request.Header.Get("Authorization"))
		if raw == "" || !strings.HasPrefix(raw, bearerPrefix) {
			// 缺失/格式错误的凭据：附结构化 bizerr（携带稳定 errorCode 与 i18n
			// messageKey），仅设置 401 状态、不写 body，交由宿主 HandlerResponse
			// 中间件本地化渲染统一响应。保留 HTTP 401 以触发前端静默重登（ADR-0002）。
			r.SetError(ErrUnauthorized)
			r.Response.WriteStatus(http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(raw, bearerPrefix))

		ctx := r.Context()
		empCtx, err := VerifyToken(ctx, token)
		if err != nil {
			r.SetError(ErrUnauthorized)
			r.Response.WriteStatus(http.StatusUnauthorized)
			return
		}

		svc := BoundEmployeeService()
		if svc == nil {
			// 拥有方插件被禁用/未安装：员工服务尚未绑定，按服务不可用返回。
			r.SetError(ErrServiceUnbound)
			r.Response.WriteStatus(http.StatusServiceUnavailable)
			return
		}

		// 实时离职拦截：每个请求都重新检查状态。查询失败或已离职一律按认证失败
		// 处理（fail-closed），复用 ErrUnauthorized 携带的 code/messageKey。
		emp, err := svc.GetByID(ctx, empCtx.EmpID)
		if err != nil || !emp.Active() {
			r.SetError(ErrUnauthorized)
			r.Response.WriteStatus(http.StatusUnauthorized)
			return
		}

		r.SetCtx(WithEmpContext(ctx, empCtx))
		r.Middleware.Next()
	}
}
