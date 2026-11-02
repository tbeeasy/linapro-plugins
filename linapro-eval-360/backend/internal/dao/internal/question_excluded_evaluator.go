// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionExcludedEvaluatorDao is the data access object for the table plugin_linapro_eval_360_question_excluded_evaluator.
type QuestionExcludedEvaluatorDao struct {
	table    string                           // table is the underlying table name of the DAO.
	group    string                           // group is the database configuration group name of the current DAO.
	columns  QuestionExcludedEvaluatorColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler               // handlers for customized model modification.
}

// QuestionExcludedEvaluatorColumns defines and stores column names for the table plugin_linapro_eval_360_question_excluded_evaluator.
type QuestionExcludedEvaluatorColumns struct {
	Id             string //
	QuestionId     string // 所属题目 id
	EvaluatorEmpId string // 被排除的评价者 EmpID
	CreatedAt      string //
}

// questionExcludedEvaluatorColumns holds the columns for the table plugin_linapro_eval_360_question_excluded_evaluator.
var questionExcludedEvaluatorColumns = QuestionExcludedEvaluatorColumns{
	Id:             "id",
	QuestionId:     "question_id",
	EvaluatorEmpId: "evaluator_emp_id",
	CreatedAt:      "created_at",
}

// NewQuestionExcludedEvaluatorDao creates and returns a new DAO object for table data access.
func NewQuestionExcludedEvaluatorDao(handlers ...gdb.ModelHandler) *QuestionExcludedEvaluatorDao {
	return &QuestionExcludedEvaluatorDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_question_excluded_evaluator",
		columns:  questionExcludedEvaluatorColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *QuestionExcludedEvaluatorDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *QuestionExcludedEvaluatorDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *QuestionExcludedEvaluatorDao) Columns() QuestionExcludedEvaluatorColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *QuestionExcludedEvaluatorDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *QuestionExcludedEvaluatorDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *QuestionExcludedEvaluatorDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
