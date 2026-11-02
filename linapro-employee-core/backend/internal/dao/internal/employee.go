// ==========================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// EmployeeDao 是表 plugin_linapro_employee_core_employee 的数据访问对象。
type EmployeeDao struct {
	table    string             // table 是 DAO 的底层表名。
	group    string             // group 是当前 DAO 的数据库配置组名。
	columns  EmployeeColumns    // columns 包含表的所有列名，便于使用。
	handlers []gdb.ModelHandler // handlers 用于自定义模型修改。
}

// EmployeeColumns 定义并存储表 plugin_linapro_employee_core_employee 的列名。
type EmployeeColumns struct {
	Id             string // 内部 EmpID：与身份提供方无关的代理主键，所有业务数据引用此值
	TenantId       string // 所属租户；同一自然人在两个租户中是两条记录
	Name           string // 员工姓名；Moka 为权威来源，登录时不覆盖
	MokaEmployeeNo string // Moka HR 系统工号（如 GZ001198）；同租户内非空值唯一，是员工行存在的稳定锚点；未接入 Moka 时为空字符串
	Status         string // 在职状态：1=在职，0=离职；EmpAuth 每次请求查询此字段以实时拦截离职员工
	SysUserId      string // 可选关联：该员工同时是后台管理员时回填对应 sys_user.id；否则为 0
	CreatedAt      string // 创建时间
	UpdatedAt      string // 更新时间
	DeletedAt      string // 软删除时间；存活行为 NULL
}

// employeeColumns 保存表 plugin_linapro_employee_core_employee 的列名。
var employeeColumns = EmployeeColumns{
	Id:             "id",
	TenantId:       "tenant_id",
	Name:           "name",
	MokaEmployeeNo: "moka_employee_no",
	Status:         "status",
	SysUserId:      "sys_user_id",
	CreatedAt:      "created_at",
	UpdatedAt:      "updated_at",
	DeletedAt:      "deleted_at",
}

// NewEmployeeDao 创建并返回一个新的 DAO 对象，用于表数据访问。
func NewEmployeeDao(handlers ...gdb.ModelHandler) *EmployeeDao {
	return &EmployeeDao{
		group:    "default",
		table:    "plugin_linapro_employee_core_employee",
		columns:  employeeColumns,
		handlers: handlers,
	}
}

// DB 获取并返回当前 DAO 的底层原始数据库管理对象。
func (dao *EmployeeDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table 返回当前 DAO 的表名。
func (dao *EmployeeDao) Table() string {
	return dao.table
}

// Columns 返回当前 DAO 的所有列名。
func (dao *EmployeeDao) Columns() EmployeeColumns {
	return dao.columns
}

// Group 返回当前 DAO 的数据库配置组名。
func (dao *EmployeeDao) Group() string {
	return dao.group
}

// Ctx 为当前 DAO 创建并返回一个 Model，并自动为当前操作设置上下文。
func (dao *EmployeeDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *EmployeeDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
