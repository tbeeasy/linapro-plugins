// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// EvaluateeDao is the data access object for the table plugin_linapro_eval_360_evaluatee.
type EvaluateeDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  EvaluateeColumns   // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// EvaluateeColumns defines and stores column names for the table plugin_linapro_eval_360_evaluatee.
type EvaluateeColumns struct {
	Id              string //
	ProjectId       string // 所属项目 id（冗余便于查询）
	QuestionnaireId string // 所属问卷 id
	EvaluateeEmpId  string // 被评估员工 EmpID（linapro-employee-core）
	CreatedAt       string //
	UpdatedAt       string //
	JobTitle        string // 被评估人职务（导入/新增弹窗录入，可空）
	Department      string // 被评估人部门（导入/新增弹窗录入，可空）
}

// evaluateeColumns holds the columns for the table plugin_linapro_eval_360_evaluatee.
var evaluateeColumns = EvaluateeColumns{
	Id:              "id",
	ProjectId:       "project_id",
	QuestionnaireId: "questionnaire_id",
	EvaluateeEmpId:  "evaluatee_emp_id",
	CreatedAt:       "created_at",
	UpdatedAt:       "updated_at",
	JobTitle:        "job_title",
	Department:      "department",
}

// NewEvaluateeDao creates and returns a new DAO object for table data access.
func NewEvaluateeDao(handlers ...gdb.ModelHandler) *EvaluateeDao {
	return &EvaluateeDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_evaluatee",
		columns:  evaluateeColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *EvaluateeDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *EvaluateeDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *EvaluateeDao) Columns() EvaluateeColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *EvaluateeDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *EvaluateeDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *EvaluateeDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
