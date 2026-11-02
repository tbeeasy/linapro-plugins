// employee_detail.go 定义后台员工详情的请求 / 响应结构。
package v1

import "github.com/gogf/gf/v2/frame/g"

// DetailReq 是后台员工详情请求。
type DetailReq struct {
	g.Meta `path:"/contacts/employees/{empId}" method:"get" tags:"Contacts" summary:"Employee detail" dc:"Full profile of one employee: basic fields, linked system account, department memberships (raw Lark department ids) and every Lark identity (including associativity-scoped ones). Returns EMPLOYEE_NOT_FOUND when the employee does not exist." permission:"linapro-employee-core:employee:query"`
	EmpId  int64 `json:"empId" v:"required|min:1" dc:"Internal EmpID" eg:"1001"`
}

// DetailRes 是后台员工详情响应。
type DetailRes struct {
	EmpId          int64  `json:"empId" dc:"Internal EmpID" eg:"1001"`
	Name           string `json:"name" dc:"Employee name; Moka is the authority, read-only in admin" eg:"Jane Doe"`
	MokaEmployeeNo string `json:"mokaEmployeeNo" dc:"Moka employee number; read-only in admin" eg:"GZ001198"`
	TenantId       int    `json:"tenantId" dc:"Owning tenant id; read-only in admin" eg:"1"`
	TenantName     string `json:"tenantName" dc:"Owning tenant name; empty when unavailable" eg:"Guangzhou Xingjia"`
	Status         int    `json:"status" dc:"Employment status: 1=active, 0=departed; read-only in admin" eg:"1"`
	// 关联后台账号：同步从不写此列，是后台可编辑的字段之一。
	SysUserId       int                 `json:"sysUserId" dc:"Linked system account id; 0 means not linked" eg:"1"`
	SysUserName     string              `json:"sysUserName" dc:"Username of the linked system account; empty when unlinked or deleted" eg:"admin"`
	SysUserNickname string              `json:"sysUserNickname" dc:"Nickname of the linked system account; empty when unlinked" eg:"Admin"`
	CreatedAt       *int64              `json:"createdAt" dc:"Creation time, Unix timestamp in milliseconds" eg:"1777334400000"`
	UpdatedAt       *int64              `json:"updatedAt" dc:"Last update time, Unix timestamp in milliseconds" eg:"1777334400000"`
	DepartmentIds   []string            `json:"departmentIds" dc:"Department memberships (raw Lark department ids); department data is reserved and not written yet, so usually an empty array" eg:"[]"`
	LarkIdentities  []*LarkIdentityItem `json:"larkIdentities" dc:"Every Lark identity of this employee (own identities of this tenant plus associativity-scoped ones)" eg:"[]"`
}

// LarkIdentityItem 是一条飞书身份映射。
type LarkIdentityItem struct {
	LarkAppId string `json:"larkAppId" dc:"Lark app id, identifying the Lark tenant" eg:"cli_a1b2c3d4"`
	// LarkAppName 取配置中的显示名；未配置名称时为空。
	LarkAppName string `json:"larkAppName" dc:"Display name of the Lark app; empty when the config does not name it" eg:"System notice (Horgos)"`
	OpenId      string `json:"openId" dc:"Lark open_id, unique within the app; useful for cross-checking sync results" eg:"ou_a1b2c3d4"`
	UserId      string `json:"userId" dc:"Lark tenant-level user_id; empty when the app lacks the user_id permission or the identity is associativity-scoped" eg:"u_9f8e7d6c"`
	IdSource    string `json:"idSource" dc:"Identity source: own=this tenant's native identity (can log in); assoc=associativity-scoped identity (only used for writing Bitable person fields, cannot log in)" eg:"own"`
	CreatedAt   *int64 `json:"createdAt" dc:"Bind time, Unix timestamp in milliseconds" eg:"1777334400000"`
}
