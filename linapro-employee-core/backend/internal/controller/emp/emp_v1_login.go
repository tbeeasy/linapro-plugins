// emp_v1_login.go 实现公开的登录换票：它将飞书
// code 换票 + 员工解析委托给登录服务，并直接投影服务返回的
// 身份快照用于响应（服务在签发 token 时已读取员工，不在此二次查询）。

package emp

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	v1 "lina-plugin-linapro-employee-core/backend/api/emp/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// Login 用飞书 code 换取员工 JWT。
func (c *LoginController) Login(ctx context.Context, req *v1.LoginReq) (*v1.LoginRes, error) {
	result, err := c.logins.Login(ctx, req.LarkAppId, req.Code)
	if err != nil {
		// 失败路径不写调用方标识：码无效、员工未登记、已离职三种情况下确实不知道是谁，
		// caller_* 五列留空，靠 larkAppId 与请求体排查。
		return nil, err
	}

	// /emp/login 挂在 EmpAuth 之前，是公开接口；不在这里补写一次，所有登录记录的
	// caller_* 都会为空——而「我登录不了」恰是最需要知道「谁」的场景。
	// SetCtxVar 是原地改 *r.Request，外层日志中间件在 Next() 返回后读得到。
	if httpRequest := g.RequestFromCtx(ctx); httpRequest != nil {
		empcap.SetCaller(
			httpRequest,
			empcap.EmpContext{EmpID: result.EmpID, TenantID: result.TenantID},
			result.Name,
			result.EmployeeNo,
		)
	}

	return &v1.LoginRes{
		Token:          result.Token,
		EmpId:          result.EmpID,
		TenantId:       result.TenantID,
		OpenId:         result.OpenID,
		Name:           result.Name,
		MokaEmployeeNo: result.EmployeeNo,
	}, nil
}
