// Package employee 定义后台「通讯录管理 → 员工列表」的控制器契约。
//
// 这些接口走宿主会话鉴权与权限点，与员工端的 EmpAuth 路由组完全分开：
// 员工端接口只返回本人数据，这里按权限点返回全量花名册（见 design D25）。
// 刻意不提供新增与删除——员工行由 Moka 全量同步维护。
package employee

import (
	"context"

	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
)

// IEmployeeV1 是后台员工列表 / 详情 / 编辑 API。
type IEmployeeV1 interface {
	List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error)
	Detail(ctx context.Context, req *v1.DetailReq) (res *v1.DetailRes, err error)
	BindLarkIdentity(ctx context.Context, req *v1.BindLarkIdentityReq) (res *v1.BindLarkIdentityRes, err error)
	UnbindLarkIdentity(ctx context.Context, req *v1.UnbindLarkIdentityReq) (res *v1.UnbindLarkIdentityRes, err error)
	SetSysUser(ctx context.Context, req *v1.SetSysUserReq) (res *v1.SetSysUserRes, err error)
	ListLarkApps(ctx context.Context, req *v1.ListLarkAppsReq) (res *v1.ListLarkAppsRes, err error)
	ListSysUserOptions(ctx context.Context, req *v1.ListSysUserOptionsReq) (res *v1.ListSysUserOptionsRes, err error)
}
