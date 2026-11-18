// employee_edit.go 定义后台员工编辑的请求 / 响应结构。
//
// 只覆盖同步不写的字段：飞书身份绑定 / 解绑、关联后台账号。
// 姓名、工号、租户、在职状态由 Moka 全量同步维护，后台只读（见 design D25）。
package v1

import "github.com/gogf/gf/v2/frame/g"

// BindLarkIdentityReq 手工绑定一条本企业飞书身份。
// 系统会先用该应用凭证在飞书通讯录确认 openId 真实存在并取回 user_id，再落库。
type BindLarkIdentityReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/lark-identities" method:"post" tags:"Contacts" summary:"Bind Lark identity" dc:"手工绑定一条本企业 own 飞书身份，用于同步按工号匹配不到的员工。写入前会先用所选应用的凭证在飞书通讯录确认 openId 真实存在并取回 user_id。身份已归属其他员工时返回 LARK_IDENTITY_TAKEN；绑定已属于本员工的身份是幂等的。注意：若该飞书成员后续能被工号匹配到，下一轮同步会重新归属这条身份。" permission:"linapro-employee-core:employee:edit"`
	EmpId     int64  `json:"empId" v:"required|min:1" dc:"内部员工 id" eg:"1001"`
	LarkAppId string `json:"larkAppId" v:"required|length:1,128" dc:"飞书应用 id；必须是本插件已配置的应用之一" eg:"cli_a1b2c3d4"`
	OpenId    string `json:"openId" v:"required|length:1,191" dc:"该应用下的飞书 open_id" eg:"ou_a1b2c3d4"`
}

// BindLarkIdentityRes 是绑定结果（无额外载荷）。
type BindLarkIdentityRes struct{}

// UnbindLarkIdentityReq 解绑员工名下的一条飞书身份（own 或 assoc 均可）。
type UnbindLarkIdentityReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/lark-identities" method:"delete" tags:"Contacts" summary:"Unbind Lark identity" dc:"删除该员工名下的一条飞书身份映射。只删除属于本员工的行；身份归属他人时返回 LARK_IDENTITY_NOT_FOUND。注意：若该身份仍能被工号匹配到，下一轮同步会重新绑定。" permission:"linapro-employee-core:employee:edit"`
	EmpId     int64  `json:"empId" v:"required|min:1" dc:"内部员工 id" eg:"1001"`
	LarkAppId string `json:"larkAppId" v:"required|length:1,128" dc:"飞书应用 id" eg:"cli_a1b2c3d4"`
	OpenId    string `json:"openId" v:"required|length:1,191" dc:"飞书 open_id" eg:"ou_a1b2c3d4"`
}

// UnbindLarkIdentityRes 是解绑结果（无额外载荷）。
type UnbindLarkIdentityRes struct{}

// SetSysUserReq 设置或取消员工的关联后台账号。
type SetSysUserReq struct {
	g.Meta    `path:"/contacts/employees/{empId}/sys-user" method:"put" tags:"Contacts" summary:"Link system account" dc:"设置该员工关联的后台账号；sysUserId 为 0 表示取消关联。一个后台账号最多只能关联一名员工（已被占用时返回 SYS_USER_TAKEN）。候选范围与可见性通过宿主用户能力校验。" permission:"linapro-employee-core:employee:edit"`
	EmpId     int64 `json:"empId" v:"required|min:1" dc:"内部员工 id" eg:"1001"`
	SysUserId int   `json:"sysUserId" v:"min:0" dc:"后台账号 id；0 表示取消关联" eg:"1"`
}

// SetSysUserRes 是设置结果（无额外载荷）。
type SetSysUserRes struct{}

// ListLarkAppsReq 返回本插件已配置的飞书应用（不含密钥），供绑定弹窗下拉。
type ListLarkAppsReq struct {
	g.Meta `path:"/contacts/employees/lark-apps" method:"get" tags:"Contacts" summary:"Lark app options" dc:"返回 plugin.linapro-employee-core.lark_apps 下已配置的飞书应用及其 app_id 与显示名。永不返回密钥。供手工绑定飞书身份弹窗使用。" permission:"linapro-employee-core:employee:edit"`
}

// ListLarkAppsRes 是飞书应用下拉响应。
type ListLarkAppsRes struct {
	List []*LarkAppItem `json:"list" dc:"已配置的飞书应用" eg:"[]"`
}

// LarkAppItem 是一个已配置的飞书应用。
type LarkAppItem struct {
	LarkAppId string `json:"larkAppId" dc:"飞书应用 id" eg:"cli_a1b2c3d4"`
	Name      string `json:"name" dc:"配置中的显示名；未命名时为空" eg:"系统通知（霍尔果斯）"`
}

// ListSysUserOptionsReq 按关键词搜索后台用户，供关联账号弹窗下拉。
type ListSysUserOptionsReq struct {
	g.Meta   `path:"/contacts/employees/sys-user-options" method:"get" tags:"Contacts" summary:"System account options" dc:"按关键词（用户名、昵称、手机号或邮箱）搜索后台账号候选。结果来自宿主用户能力，并受操作员的数据权限约束；已关联其他员工的账号仍会返回，由调用端自行标注已占用。" permission:"linapro-employee-core:employee:edit"`
	Keyword  string `json:"keyword" dc:"搜索关键词；为空时返回第一页" eg:"admin"`
	PageNum  int    `json:"pageNum" dc:"页码，从 1 开始，默认 1" eg:"1"`
	PageSize int    `json:"pageSize" dc:"每页条数，默认 10，最大 50" eg:"10"`
}

// ListSysUserOptionsRes 是后台账号候选响应。
type ListSysUserOptionsRes struct {
	List  []*SysUserOption `json:"list" dc:"候选后台账号" eg:"[]"`
	Total int              `json:"total" dc:"匹配的总行数" eg:"1"`
}

// SysUserOption 是一个后台账号候选。
type SysUserOption struct {
	Id       int    `json:"id" dc:"后台账号 id" eg:"1"`
	Username string `json:"username" dc:"登录名" eg:"admin"`
	Nickname string `json:"nickname" dc:"昵称" eg:"管理员"`
}
