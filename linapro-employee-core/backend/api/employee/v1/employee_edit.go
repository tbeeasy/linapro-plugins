// employee_edit.go 定义后台员工编辑的请求 / 响应结构。
//
// 只覆盖同步不写的字段：飞书身份绑定 / 解绑、关联后台账号。
// 姓名、工号、租户、在职状态由 Moka 全量同步维护，后台只读（见 design D25）。
package v1

import "github.com/gogf/gf/v2/frame/g"

// BindLarkIdentityReq 手工绑定一条本企业飞书身份。
// 系统会先用该应用凭证在飞书通讯录确认 openId 真实存在并取回 user_id，再落库。
type BindLarkIdentityReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/lark-identities" method:"post" tags:"Contacts" summary:"Bind Lark identity" dc:"Manually bind one identity owned by this tenant, for people the sync could not match by employee number. The openId is verified against the Lark contact directory of the selected app and its user_id is captured before writing. Rejected with LARK_IDENTITY_TAKEN when the identity already belongs to another employee; binding an identity that already belongs to this employee is idempotent. Note: if the Lark member can later be matched by employee number, the next sync run re-owns this identity." permission:"linapro-employee-core:employee:edit"`
	EmpId     int64  `json:"empId" v:"required|min:1" dc:"Internal EmpID" eg:"1001"`
	LarkAppId string `json:"larkAppId" v:"required|length:1,128" dc:"Lark app id; must be one of the apps configured for this plugin" eg:"cli_a1b2c3d4"`
	OpenId    string `json:"openId" v:"required|length:1,191" dc:"Lark open_id under that app" eg:"ou_a1b2c3d4"`
}

// BindLarkIdentityRes 是绑定结果（无额外载荷）。
type BindLarkIdentityRes struct{}

// UnbindLarkIdentityReq 解绑员工名下的一条飞书身份（own 或 assoc 均可）。
type UnbindLarkIdentityReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/lark-identities" method:"delete" tags:"Contacts" summary:"Unbind Lark identity" dc:"Delete one Lark identity mapping owned by this employee. Only rows belonging to this employee are deleted; an identity that belongs to someone else returns LARK_IDENTITY_NOT_FOUND. Note: if the identity can still be matched by employee number, the next sync run binds it again." permission:"linapro-employee-core:employee:edit"`
	EmpId     int64  `json:"empId" v:"required|min:1" dc:"Internal EmpID" eg:"1001"`
	LarkAppId string `json:"larkAppId" v:"required|length:1,128" dc:"Lark app id" eg:"cli_a1b2c3d4"`
	OpenId    string `json:"openId" v:"required|length:1,191" dc:"Lark open_id" eg:"ou_a1b2c3d4"`
}

// UnbindLarkIdentityRes 是解绑结果（无额外载荷）。
type UnbindLarkIdentityRes struct{}

// SetSysUserReq 设置或取消员工的关联后台账号。
type SetSysUserReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/sys-user" method:"put" tags:"Contacts" summary:"Link system account" dc:"Set the system account linked to this employee; sysUserId 0 clears the link. One system account can be linked to at most one employee (returns SYS_USER_TAKEN when taken). Candidates and visibility are validated through the host user capability." permission:"linapro-employee-core:employee:edit"`
	EmpId     int64 `json:"empId" v:"required|min:1" dc:"Internal EmpID" eg:"1001"`
	SysUserId int   `json:"sysUserId" v:"min:0" dc:"System account id; 0 clears the link" eg:"1"`
}

// SetSysUserRes 是设置结果（无额外载荷）。
type SetSysUserRes struct{}

// ListLarkAppsReq 返回本插件已配置的飞书应用（不含密钥），供绑定弹窗下拉。
type ListLarkAppsReq struct {
	g.Meta `path:"/contacts/employees/lark-apps" method:"get" tags:"Contacts" summary:"Lark app options" dc:"Return the Lark apps configured under plugin.linapro-employee-core.lark_apps with their app_id and display name. Secrets are never returned. Used by the manual identity binding dialog." permission:"linapro-employee-core:employee:edit"`
}

// ListLarkAppsRes 是飞书应用下拉响应。
type ListLarkAppsRes struct {
	List []*LarkAppItem `json:"list" dc:"Configured Lark apps" eg:"[]"`
}

// LarkAppItem 是一个已配置的飞书应用。
type LarkAppItem struct {
	LarkAppId string `json:"larkAppId" dc:"Lark app id" eg:"cli_a1b2c3d4"`
	Name      string `json:"name" dc:"Display name from config; empty when unnamed" eg:"System notice (Horgos)"`
}

// ListSysUserOptionsReq 按关键词搜索后台用户，供关联账号弹窗下拉。
type ListSysUserOptionsReq struct {
	g.Meta   `path:"/contacts/employees/sys-user-options" method:"get" tags:"Contacts" summary:"System account options" dc:"Search system account candidates by keyword (username, nickname, phone or email). Results come from the host user capability and are constrained by the operator's user data permission; accounts already linked to another employee are still returned and reported as taken by clients." permission:"linapro-employee-core:employee:edit"`
	Keyword  string `json:"keyword" dc:"Search keyword; empty returns the first page" eg:"admin"`
	PageNum  int    `json:"pageNum" dc:"One-based page number, defaults to 1" eg:"1"`
	PageSize int    `json:"pageSize" dc:"Page size, defaults to 10, max 50" eg:"10"`
}

// ListSysUserOptionsRes 是后台账号候选响应。
type ListSysUserOptionsRes struct {
	List  []*SysUserOption `json:"list" dc:"Candidate system accounts" eg:"[]"`
	Total int              `json:"total" dc:"Total matching rows" eg:"1"`
}

// SysUserOption 是一个后台账号候选。
type SysUserOption struct {
	Id       int    `json:"id" dc:"System account id" eg:"1"`
	Username string `json:"username" dc:"Login name" eg:"admin"`
	Nickname string `json:"nickname" dc:"Nickname" eg:"Admin"`
}
