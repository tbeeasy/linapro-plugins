// =================================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// =================================================================================

package entity

import (
	"time"
)

// EmployeeDept 是表 employee_dept 的 Go 结构体。
type EmployeeDept struct {
	Id               int64      `json:"id"               orm:"id"                 description:""`
	TenantId         int        `json:"tenantId"         orm:"tenant_id"          description:""`
	EmpId            int64      `json:"empId"            orm:"emp_id"             description:"所属员工的 EmpID"`
	LarkDepartmentId string     `json:"larkDepartmentId" orm:"lark_department_id" description:"登录时从飞书采集的原始部门 ID"`
	IsPrimary        int        `json:"isPrimary"        orm:"is_primary"         description:"主部门标识：1=主部门，0=兼属部门"`
	CreatedAt        *time.Time `json:"createdAt"        orm:"created_at"         description:""`
}
