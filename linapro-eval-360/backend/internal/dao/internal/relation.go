// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// RelationDao is the data access object for the table plugin_linapro_eval_360_relation.
type RelationDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  RelationColumns    // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// RelationColumns defines and stores column names for the table plugin_linapro_eval_360_relation.
type RelationColumns struct {
	Id              string //
	ProjectId       string // 所属项目 id
	QuestionnaireId string // 所属问卷 id（必须在唯一键内）
	EvaluateeId     string // 被评估人 evaluatee.id
	EvaluatorEmpId  string // 评价者 EmpID
	RoleId          string // 角色 role.id（属性，不进唯一键）
	Source          string // 来源：import=全量导入 manual=关系编辑器手动新增
	Completed       string // 完成状态：0=未填 1=已填；派生已作废不落库
	CompletedAt     string // 首次提交时间
	CreatedAt       string //
	UpdatedAt       string //
}

// relationColumns holds the columns for the table plugin_linapro_eval_360_relation.
var relationColumns = RelationColumns{
	Id:              "id",
	ProjectId:       "project_id",
	QuestionnaireId: "questionnaire_id",
	EvaluateeId:     "evaluatee_id",
	EvaluatorEmpId:  "evaluator_emp_id",
	RoleId:          "role_id",
	Source:          "source",
	Completed:       "completed",
	CompletedAt:     "completed_at",
	CreatedAt:       "created_at",
	UpdatedAt:       "updated_at",
}

// NewRelationDao creates and returns a new DAO object for table data access.
func NewRelationDao(handlers ...gdb.ModelHandler) *RelationDao {
	return &RelationDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_relation",
		columns:  relationColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *RelationDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *RelationDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *RelationDao) Columns() RelationColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *RelationDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *RelationDao) Ctx(ctx context.Context) *gdb.Model {
	model := dao.DB().Model(dao.table)
	for _, handler := range dao.handlers {
		model = handler(model)
	}
	return model.Safe().Ctx(ctx)
}

// Transaction wraps the transaction logic using function f.
// It rolls back the transaction and returns the error if function f returns a non-nil error.
// It commits the transaction and returns nil if function f returns nil.
//
// Note: Do not commit or roll back the transaction in function f,
// as it is automatically handled by this function.
func (dao *RelationDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
