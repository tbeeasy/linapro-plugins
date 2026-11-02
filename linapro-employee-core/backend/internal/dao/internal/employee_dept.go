// ==========================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// EmployeeDeptDao 是表 plugin_linapro_employee_core_employee_dept 的数据访问对象。
type EmployeeDeptDao struct {
	table    string              // table 是 DAO 的底层表名。
	group    string              // group 是当前 DAO 的数据库配置组名。
	columns  EmployeeDeptColumns // columns 包含表的所有列名，便于使用。
	handlers []gdb.ModelHandler  // handlers 用于自定义模型修改。
}

// EmployeeDeptColumns 定义并存储表 plugin_linapro_employee_core_employee_dept 的列名。
type EmployeeDeptColumns struct {
	Id               string //
	TenantId         string //
	EmpId            string // 所属员工的 EmpID
	LarkDepartmentId string // 登录时从飞书采集的原始部门 ID
	IsPrimary        string // 主部门标识：1=主部门，0=兼属部门
	CreatedAt        string //
}

// employeeDeptColumns 保存表 plugin_linapro_employee_core_employee_dept 的列名。
var employeeDeptColumns = EmployeeDeptColumns{
	Id:               "id",
	TenantId:         "tenant_id",
	EmpId:            "emp_id",
	LarkDepartmentId: "lark_department_id",
	IsPrimary:        "is_primary",
	CreatedAt:        "created_at",
}

// NewEmployeeDeptDao 创建并返回一个新的 DAO 对象，用于表数据访问。
func NewEmployeeDeptDao(handlers ...gdb.ModelHandler) *EmployeeDeptDao {
	return &EmployeeDeptDao{
		group:    "default",
		table:    "plugin_linapro_employee_core_employee_dept",
		columns:  employeeDeptColumns,
		handlers: handlers,
	}
}

// DB 获取并返回当前 DAO 的底层原始数据库管理对象。
func (dao *EmployeeDeptDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table 返回当前 DAO 的表名。
func (dao *EmployeeDeptDao) Table() string {
	return dao.table
}

// Columns 返回当前 DAO 的所有列名。
func (dao *EmployeeDeptDao) Columns() EmployeeDeptColumns {
	return dao.columns
}

// Group 返回当前 DAO 的数据库配置组名。
func (dao *EmployeeDeptDao) Group() string {
	return dao.group
}

// Ctx 为当前 DAO 创建并返回一个 Model，并自动为当前操作设置上下文。
func (dao *EmployeeDeptDao) Ctx(ctx context.Context) *gdb.Model {
	model := dao.DB().Model(dao.table)
	for _, handler := range dao.handlers {
		model = handler(model)
	}
	return model.Safe().Ctx(ctx)
}

// Transaction 使用函数 f 封装事务逻辑。
// 若函数 f 返回非空错误，则回滚事务并返回该错误。
// 若函数 f 返回 nil，则提交事务并返回 nil。
//
// 注意：不要在函数 f 中提交或回滚事务，
// 事务由本函数自动处理。
func (dao *EmployeeDeptDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
