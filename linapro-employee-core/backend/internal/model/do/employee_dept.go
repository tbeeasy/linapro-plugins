// =================================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// EmployeeDept 是表 plugin_linapro_employee_core_employee_dept 的 Go 结构体，用于 Where/Data 等 DAO 操作。
type EmployeeDept struct {
	g.Meta           `orm:"table:plugin_linapro_employee_core_employee_dept, do:true"`
	Id               any        //
	TenantId         any        //
	EmpId            any        // 所属员工的 EmpID
	LarkDepartmentId any        // 登录时从飞书采集的原始部门 ID
	IsPrimary        any        // 主部门标识：1=主部门，0=兼属部门
	CreatedAt        *time.Time //
}
