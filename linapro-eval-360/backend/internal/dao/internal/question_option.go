// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionOptionDao is the data access object for the table plugin_linapro_eval_360_question_option.
type QuestionOptionDao struct {
	table    string                // table is the underlying table name of the DAO.
	group    string                // group is the database configuration group name of the current DAO.
	columns  QuestionOptionColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler    // handlers for customized model modification.
}

// QuestionOptionColumns defines and stores column names for the table plugin_linapro_eval_360_question_option.
type QuestionOptionColumns struct {
	Id         string //
	QuestionId string // 所属单选题 id
	Label      string // 选项文案
	Score      string // 选项分值，NUMERIC
	AllowText  string // 是否允许附文本评语：1=是
	Sort       string // 选项排序
	CreatedAt  string //
	UpdatedAt  string //
}

// questionOptionColumns holds the columns for the table plugin_linapro_eval_360_question_option.
var questionOptionColumns = QuestionOptionColumns{
	Id:         "id",
	QuestionId: "question_id",
	Label:      "label",
	Score:      "score",
	AllowText:  "allow_text",
	Sort:       "sort",
	CreatedAt:  "created_at",
	UpdatedAt:  "updated_at",
}

// NewQuestionOptionDao creates and returns a new DAO object for table data access.
func NewQuestionOptionDao(handlers ...gdb.ModelHandler) *QuestionOptionDao {
	return &QuestionOptionDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_question_option",
		columns:  questionOptionColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *QuestionOptionDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *QuestionOptionDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *QuestionOptionDao) Columns() QuestionOptionColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *QuestionOptionDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *QuestionOptionDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *QuestionOptionDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
