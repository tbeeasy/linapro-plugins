// employee_list.go 定义后台员工列表的请求 / 响应结构。
package v1

import "github.com/gogf/gf/v2/frame/g"

// ListReq 是后台员工分页列表请求。
type ListReq struct {
	g.Meta    `path:"/contacts/employees" method:"get" tags:"Contacts" summary:"Employee list" dc:"Paginated roster of employees synced from Moka (departed ones included). Filtering and paging run in SQL, ordered by EmpID descending. The list does not expose Lark open_id or user_id; the Lark binding flag only counts identities owned by this tenant." permission:"linapro-employee-core:employee:list"`
	PageNum   int    `json:"pageNum" dc:"One-based page number, defaults to 1" eg:"1"`
	PageSize  int    `json:"pageSize" dc:"Page size, defaults to 10, max 100" eg:"10"`
	Keyword   string `json:"keyword" dc:"Fuzzy match on employee name or employee number; empty means no filter" eg:"Jane"`
	TenantId  int    `json:"tenantId" dc:"Filter by owning tenant; omitted or 0 means all tenants" eg:"1"`
	Status    *int   `json:"status" dc:"Filter by employment status: 1=active, 0=departed; omitted means no filter" eg:"1"`
	LarkBound *bool  `json:"larkBound" dc:"Filter by whether the employee is bound to a Lark identity owned by this tenant; omitted means no filter" eg:"true"`
}

// ListRes 是后台员工分页列表响应。
type ListRes struct {
	List  []*EmployeeItem `json:"list" dc:"Employees on the current page" eg:"[]"`
	Total int             `json:"total" dc:"Total rows matching the filters" eg:"1"`
}

// EmployeeItem 是列表中的一名员工。
type EmployeeItem struct {
	EmpId          int64    `json:"empId" dc:"Internal EmpID referenced by business data" eg:"1001"`
	Name           string   `json:"name" dc:"Employee name; Moka is the authority, read-only in admin" eg:"Jane Doe"`
	MokaEmployeeNo string   `json:"mokaEmployeeNo" dc:"Moka employee number; the stable anchor for login and person resolution" eg:"GZ001198"`
	TenantId       int      `json:"tenantId" dc:"Owning tenant id" eg:"1"`
	TenantName     string   `json:"tenantName" dc:"Owning tenant name; empty when the tenant capability is unavailable or the tenant no longer exists" eg:"Guangzhou Xingjia"`
	Status         int      `json:"status" dc:"Employment status: 1=active, 0=departed" eg:"1"`
	LarkBound      bool     `json:"larkBound" dc:"Whether a Lark identity owned by this tenant is bound (this is what decides whether the employee can log in)" eg:"true"`
	LarkAppIds     []string `json:"larkAppIds" dc:"Lark app ids of the bound own identities; empty array when unbound" eg:"[\"cli_a1b2c3d4\"]"`
	UpdatedAt      *int64   `json:"updatedAt" dc:"Last update time, Unix timestamp in milliseconds" eg:"1777334400000"`
}
