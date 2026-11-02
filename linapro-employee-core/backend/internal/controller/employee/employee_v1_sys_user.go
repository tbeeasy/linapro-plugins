// employee_v1_sys_user.go 实现关联后台账号的设置与取消。
//
// sys_user_id 是同步从不触碰的列，因此可以安全地在后台编辑；写入前经宿主用户
// 能力校验目标账号对当前操作者可见，避免凭 id 关联到看不到的账号。
package employee

import (
	"context"
	"strconv"

	"lina-core/pkg/plugin/capability/usercap"
	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// SetSysUser 设置或取消员工关联的后台账号。sysUserId 为 0 表示取消关联。
func (c *ControllerV1) SetSysUser(ctx context.Context, req *v1.SetSysUserReq) (res *v1.SetSysUserRes, err error) {
	if req.SysUserId > 0 {
		if c.users == nil {
			// 宿主用户能力不可用时拒绝关联：无法校验可见性就不写。
			return nil, empcap.ErrInvalidParameter
		}
		if err = c.users.EnsureVisible(ctx, []usercap.UserID{usercap.UserID(strconv.Itoa(req.SysUserId))}); err != nil {
			return nil, err
		}
	}
	if err = c.employees.SetSysUser(ctx, req.EmpId, req.SysUserId); err != nil {
		return nil, err
	}
	return &v1.SetSysUserRes{}, nil
}

// sysUserName 返回关联后台账号的登录名与昵称。未关联、账号已删除或宿主用户
// 能力不可用时返回空串，由前端退化为只显示 id。
func (c *ControllerV1) sysUserName(ctx context.Context, sysUserID int) (username, nickname string) {
	if sysUserID <= 0 || c.users == nil {
		return "", ""
	}
	info, err := c.users.Get(ctx, usercap.UserID(strconv.Itoa(sysUserID)))
	if err != nil || info == nil {
		return "", ""
	}
	return info.Username, info.Nickname
}

// userIDToInt 把宿主用户 id（字符串形态）转成员工表 sys_user_id 所用的 int。
// 解析失败返回 0——调用方把 0 视为「未关联」，不会误绑到别的账号上。
func userIDToInt(id usercap.UserID) int {
	n, err := strconv.Atoi(string(id))
	if err != nil {
		return 0
	}
	return n
}
