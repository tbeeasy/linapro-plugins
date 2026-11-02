// =================================================================================
// 此文件由 GoFrame CLI 工具自动生成。可按需修改。
// =================================================================================

package dao

import (
	"lina-plugin-linapro-employee-core/backend/internal/dao/internal"
)

// employeeDao 是表 plugin_linapro_employee_core_employee 的数据访问对象。
type employeeDao struct {
	*internal.EmployeeDao
}

var (
	// Employee 是用于表 plugin_linapro_employee_core_employee 操作的全局可访问对象。
	Employee = employeeDao{internal.NewEmployeeDao()}
)
