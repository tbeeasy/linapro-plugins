// employee_detail.go 定义后台员工详情的请求 / 响应结构。
package v1

import "github.com/gogf/gf/v2/frame/g"

// DetailReq 是后台员工详情请求。
type DetailReq struct {
	g.Meta `path:"/contacts/employees/{empId}" method:"get" tags:"Contacts" summary:"Employee detail" dc:"返回单个员工的完整档案：基础字段、关联后台账号、所属部门（飞书原始部门 id）以及全部飞书身份（含关联组织视角的身份）。员工不存在时返回 EMPLOYEE_NOT_FOUND。" permission:"linapro-employee-core:employee:query"`
	EmpId  int64 `json:"empId" v:"required|min:1" dc:"内部员工 id" eg:"1001"`
}

// DetailRes 是后台员工详情响应。
type DetailRes struct {
	EmpId          int64  `json:"empId" dc:"内部员工 id" eg:"1001"`
	Name           string `json:"name" dc:"员工姓名；以 Moka 为准，后台只读" eg:"张三"`
	MokaEmployeeNo string `json:"mokaEmployeeNo" dc:"Moka 工号；后台只读" eg:"GZ001198"`
	TenantId       int    `json:"tenantId" dc:"所属租户 id；后台只读" eg:"1"`
	TenantName     string `json:"tenantName" dc:"所属租户名称；取不到时为空" eg:"星嘉科技"`
	Status         int    `json:"status" dc:"在职状态：1=在职，0=离职；后台只读" eg:"1"`
	// 关联后台账号：同步从不写此列，是后台可编辑的字段之一。
	SysUserId       int    `json:"sysUserId" dc:"关联的后台账号 id；0 表示未关联" eg:"1"`
	SysUserName     string `json:"sysUserName" dc:"关联后台账号的用户名；未关联或账号已删除时为空" eg:"admin"`
	SysUserNickname string `json:"sysUserNickname" dc:"关联后台账号的昵称；未关联时为空" eg:"管理员"`
	// 三个任职日期均来自 Moka 同步、后台只读，格式 YYYY-MM-DD，空字符串表示 Moka 未提供。
	OnBoardingDate   string              `json:"onBoardingDate" dc:"来自 Moka 的入职日期（YYYY-MM-DD，date-only 日历日，不含时间点语义）；Moka 未提供时为空；后台只读" eg:"2021-03-01"`
	EndProbationDate string              `json:"endProbationDate" dc:"来自 Moka 的转正日期（YYYY-MM-DD，date-only 日历日）；Moka 未提供时为空；后台只读" eg:"2021-06-01"`
	LeaveDate        string              `json:"leaveDate" dc:"来自 Moka 的离职日期（YYYY-MM-DD，date-only 日历日）；在职期间或 Moka 未提供时为空；后台只读" eg:"2024-08-31"`
	CreatedAt        *int64              `json:"createdAt" dc:"创建时间，Unix 毫秒时间戳" eg:"1777334400000"`
	UpdatedAt        *int64              `json:"updatedAt" dc:"最后更新时间，Unix 毫秒时间戳" eg:"1777334400000"`
	DepartmentIds    []string            `json:"departmentIds" dc:"所属部门（飞书原始部门 id）；部门数据当前预留、尚未写入，通常为空数组" eg:"[]"`
	LarkIdentities   []*LarkIdentityItem `json:"larkIdentities" dc:"该员工的全部飞书身份（本企业 own 身份与关联组织视角 assoc 身份）" eg:"[]"`
}

// LarkIdentityItem 是一条飞书身份映射。
type LarkIdentityItem struct {
	LarkAppId string `json:"larkAppId" dc:"飞书应用 id，用于标识所属飞书企业" eg:"cli_a1b2c3d4"`
	// LarkAppName 取配置中的显示名；未配置名称时为空。
	LarkAppName string `json:"larkAppName" dc:"飞书应用的显示名；配置未命名时为空" eg:"系统通知（霍尔果斯）"`
	OpenId      string `json:"openId" dc:"飞书 open_id，应用内唯一；可用于核对同步结果" eg:"ou_a1b2c3d4"`
	UserId      string `json:"userId" dc:"飞书租户级 user_id；应用缺少 user_id 权限或身份为关联组织视角时为空" eg:"u_9f8e7d6c"`
	IdSource    string `json:"idSource" dc:"身份来源：own=本企业原生身份（可登录），assoc=关联组织视角身份（仅用于写飞书多维表人员字段，不可登录）" eg:"own"`
	CreatedAt   *int64 `json:"createdAt" dc:"绑定时间，Unix 毫秒时间戳" eg:"1777334400000"`
}
