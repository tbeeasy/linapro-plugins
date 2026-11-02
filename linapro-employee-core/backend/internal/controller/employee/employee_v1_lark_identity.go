// employee_v1_lark_identity.go 实现飞书身份的手工绑定与解绑。
//
// 绑定是「同步按工号匹配不上」时的人工兜底路径，因此写入前必须确认该 open_id
// 在该应用通讯录中真实存在：否则会落一条永远登录不了、且让同步排查更混乱的脏映射。
package employee

import (
	"context"
	"errors"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
	"linapro-lark-sdk/larkcontact"
)

// BindLarkIdentity 校验飞书侧存在性后，为员工手工绑定一条本企业身份。
func (c *ControllerV1) BindLarkIdentity(ctx context.Context, req *v1.BindLarkIdentityReq) (res *v1.BindLarkIdentityRes, err error) {
	cred, err := c.findCredential(ctx, req.LarkAppId)
	if err != nil {
		return nil, err
	}
	if c.contactClient == nil {
		return nil, gerror.New("linapro-employee-core: lark contact client not wired")
	}
	user, err := c.contactClient(cred.AppID, cred.AppSecret).GetUser(ctx, req.OpenId)
	if err != nil {
		if errors.Is(err, larkcontact.ErrUserNotFound) {
			return nil, empcap.ErrLarkUserNotFound
		}
		return nil, err
	}
	// user_id 只在飞书返回时落库（应用缺 user_id 权限时为空，不影响登录）。
	if err = c.employees.BindLarkIdentity(ctx, req.EmpId, req.LarkAppId, user.OpenID, user.UserID); err != nil {
		return nil, err
	}
	return &v1.BindLarkIdentityRes{}, nil
}

// UnbindLarkIdentity 解绑员工名下的一条飞书身份。
func (c *ControllerV1) UnbindLarkIdentity(ctx context.Context, req *v1.UnbindLarkIdentityReq) (res *v1.UnbindLarkIdentityRes, err error) {
	if err = c.employees.UnbindLarkIdentity(ctx, req.EmpId, req.LarkAppId, req.OpenId); err != nil {
		return nil, err
	}
	return &v1.UnbindLarkIdentityRes{}, nil
}

// findCredential 从运行期配置中取指定应用的凭据。未配置该应用时按既有
// 凭据缺失语义失败关闭，而不是回退到别的应用。
func (c *ControllerV1) findCredential(ctx context.Context, larkAppID string) (lark.Credential, error) {
	if c.hostConfig == nil {
		return lark.Credential{}, lark.ErrCredentialMissing
	}
	apps, err := lark.LoadApps(ctx, c.hostConfig)
	if err != nil {
		return lark.Credential{}, err
	}
	for _, app := range apps {
		if app.AppID == larkAppID && app.SecretKey != "" {
			return lark.Credential{AppID: app.AppID, AppSecret: app.SecretKey}, nil
		}
	}
	return lark.Credential{}, lark.ErrCredentialMissing
}
