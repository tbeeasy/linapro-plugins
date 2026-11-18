// employee_list.go 定义后台员工列表的请求 / 响应结构。
package v1

import "github.com/gogf/gf/v2/frame/g"

// ListReq 是后台员工分页列表请求。
type ListReq struct {
	g.Meta    `path:"/contacts/employees" method:"get" tags:"Contacts" summary:"Employee list" dc:"Moka 同步落库的员工分页花名册（含已离职员工）。过滤与分页均在 SQL 内完成，按 EmpID 倒序。列表不返回飞书 open_id 与 user_id；绑定标记只统计本企业 own 身份。" permission:"linapro-employee-core:employee:list"`
	PageNum   int    `json:"pageNum" dc:"页码，从 1 开始，默认 1" eg:"1"`
	PageSize  int    `json:"pageSize" dc:"每页条数，默认 10，最大 100" eg:"10"`
	Keyword   string `json:"keyword" dc:"按员工姓名或工号模糊匹配；为空表示不过滤" eg:"张三"`
	TenantId  int    `json:"tenantId" dc:"按所属租户过滤；不传或 0 表示全部租户" eg:"1"`
	Status    *int   `json:"status" dc:"按在职状态过滤：1=在职，0=离职；不传表示不过滤" eg:"1"`
	LarkBound *bool  `json:"larkBound" dc:"按是否绑定本企业 own 飞书身份过滤；不传表示不过滤" eg:"true"`
}

// ListRes 是后台员工分页列表响应。
type ListRes struct {
	List  []*EmployeeItem `json:"list" dc:"当前页的员工" eg:"[]"`
	Total int             `json:"total" dc:"符合过滤条件的总行数" eg:"1"`
}

// EmployeeItem 是列表中的一名员工。
type EmployeeItem struct {
	EmpId          int64  `json:"empId" dc:"内部员工 id，业务数据引用此 id" eg:"1001"`
	Name           string `json:"name" dc:"员工姓名；以 Moka 为准，后台只读" eg:"张三"`
	MokaEmployeeNo string `json:"mokaEmployeeNo" dc:"Moka 工号；登录与人员解析的稳定锚点" eg:"GZ001198"`
	TenantId       int    `json:"tenantId" dc:"所属租户 id" eg:"1"`
	TenantName     string `json:"tenantName" dc:"所属租户名称；租户能力不可用或租户已不存在时为空" eg:"星嘉科技"`
	Status         int    `json:"status" dc:"在职状态：1=在职，0=离职" eg:"1"`
	// 三个任职日期与详情接口同源同格式，供花名册直接展示，避免逐行打开详情补齐。
	OnBoardingDate   string   `json:"onBoardingDate" dc:"来自 Moka 的入职日期（YYYY-MM-DD，date-only 日历日）；Moka 未提供时为空字符串" eg:"2021-03-01"`
	EndProbationDate string   `json:"endProbationDate" dc:"来自 Moka 的转正日期（YYYY-MM-DD，date-only 日历日）；Moka 未提供时为空字符串" eg:"2021-06-01"`
	LeaveDate        string   `json:"leaveDate" dc:"来自 Moka 的离职日期（YYYY-MM-DD，date-only 日历日）；在职期间或 Moka 未提供时为空字符串" eg:"2024-08-31"`
	LarkBound        bool     `json:"larkBound" dc:"是否已绑定本企业 own 飞书身份（决定该员工能否登录）" eg:"true"`
	LarkAppIds       []string `json:"larkAppIds" dc:"已绑定的 own 身份所属飞书应用 id；未绑定时为空数组" eg:"[\"cli_a1b2c3d4\"]"`
	UpdatedAt        *int64   `json:"updatedAt" dc:"最后更新时间，Unix 毫秒时间戳" eg:"1777334400000"`
}
