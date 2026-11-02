// ==========================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// LarkIdentityDao 是表 plugin_linapro_employee_core_lark_identity 的数据访问对象。
type LarkIdentityDao struct {
	table    string              // table 是 DAO 的底层表名。
	group    string              // group 是当前 DAO 的数据库配置组名。
	columns  LarkIdentityColumns // columns 包含表的所有列名，便于使用。
	handlers []gdb.ModelHandler  // handlers 用于自定义模型修改。
}

// LarkIdentityColumns 定义并存储表 plugin_linapro_employee_core_lark_identity 的列名。
type LarkIdentityColumns struct {
	LarkAppId  string // 飞书 App ID（cli_xxx）；标识该 open_id 所属的飞书企业
	LarkOpenId string // 飞书应用内 open_id；同一 lark_app_id 下唯一
	LarkUserId string // 飞书 user_id（企业级，跨该企业所有 app 一致）；考勤等按 user_id 推送的数据源用它反查 emp_id；应用无 user_id 权限时为空
	IdSource   string // 身份来源：own=本租户原生身份（可登录/考勤反查）；assoc=关联组织视角身份（仅供写多维表格人员字段，不参与登录/反查）
	EmpId      string // 映射到的内部 EmpID；同一 emp_id 可有多条（跨飞书企业）
	CreatedAt  string // 创建时间
}

// larkIdentityColumns 保存表 plugin_linapro_employee_core_lark_identity 的列名。
var larkIdentityColumns = LarkIdentityColumns{
	LarkAppId:  "lark_app_id",
	LarkOpenId: "lark_open_id",
	LarkUserId: "lark_user_id",
	IdSource:   "id_source",
	EmpId:      "emp_id",
	CreatedAt:  "created_at",
}

// NewLarkIdentityDao 创建并返回一个新的 DAO 对象，用于表数据访问。
func NewLarkIdentityDao(handlers ...gdb.ModelHandler) *LarkIdentityDao {
	return &LarkIdentityDao{
		group:    "default",
		table:    "plugin_linapro_employee_core_lark_identity",
		columns:  larkIdentityColumns,
		handlers: handlers,
	}
}

// DB 获取并返回当前 DAO 的底层原始数据库管理对象。
func (dao *LarkIdentityDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table 返回当前 DAO 的表名。
func (dao *LarkIdentityDao) Table() string {
	return dao.table
}

// Columns 返回当前 DAO 的所有列名。
func (dao *LarkIdentityDao) Columns() LarkIdentityColumns {
	return dao.columns
}

// Group 返回当前 DAO 的数据库配置组名。
func (dao *LarkIdentityDao) Group() string {
	return dao.group
}

// Ctx 为当前 DAO 创建并返回一个 Model，并自动为当前操作设置上下文。
func (dao *LarkIdentityDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *LarkIdentityDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
