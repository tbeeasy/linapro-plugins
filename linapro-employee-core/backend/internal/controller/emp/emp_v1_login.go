// emp_v1_login.go 实现公开的登录换票：它将飞书
// code 换票 + 员工解析委托给登录服务，并直接投影服务返回的
// 身份快照用于响应（服务在签发 token 时已读取员工，不在此二次查询）。

package emp

import (
	"context"

	v1 "lina-plugin-linapro-employee-core/backend/api/emp/v1"
)

// Login 用飞书 code 换取员工 JWT。
func (c *LoginController) Login(ctx context.Context, req *v1.LoginReq) (*v1.LoginRes, error) {
	result, err := c.logins.Login(ctx, req.LarkAppId, req.Code)
	if err != nil {
		return nil, err
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
