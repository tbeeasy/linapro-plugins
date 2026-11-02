// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// AnswerDao is the data access object for the table plugin_linapro_eval_360_answer.
type AnswerDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  AnswerColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// AnswerColumns defines and stores column names for the table plugin_linapro_eval_360_answer.
type AnswerColumns struct {
	Id              string //
	SubmissionId    string // 所属作答 id
	QuestionnaireId string // 所属问卷 id
	QuestionId      string // 所属题目 id
	RowId           string // 矩阵题行 id，非矩阵为 0
	OptionId        string // 单选选项 id / 矩阵列 id / 文本为 0
	TextValue       string // 文本题答案 或 选项级评语（allow_text）
	CreatedAt       string //
}

// answerColumns holds the columns for the table plugin_linapro_eval_360_answer.
var answerColumns = AnswerColumns{
	Id:              "id",
	SubmissionId:    "submission_id",
	QuestionnaireId: "questionnaire_id",
	QuestionId:      "question_id",
	RowId:           "row_id",
	OptionId:        "option_id",
	TextValue:       "text_value",
	CreatedAt:       "created_at",
}

// NewAnswerDao creates and returns a new DAO object for table data access.
func NewAnswerDao(handlers ...gdb.ModelHandler) *AnswerDao {
	return &AnswerDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_answer",
		columns:  answerColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *AnswerDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *AnswerDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *AnswerDao) Columns() AnswerColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *AnswerDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *AnswerDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *AnswerDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
