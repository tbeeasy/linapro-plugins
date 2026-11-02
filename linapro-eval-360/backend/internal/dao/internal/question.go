// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionDao is the data access object for the table plugin_linapro_eval_360_question.
type QuestionDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  QuestionColumns    // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// QuestionColumns defines and stores column names for the table plugin_linapro_eval_360_question.
type QuestionColumns struct {
	Id              string //
	QuestionnaireId string // 所属问卷 id
	DimensionId     string // 所属维度 id（每题必须挂维度）
	Type            string // 题型：single_choice|text_short|text_long|matrix
	Title           string // 题目标题（富文本 HTML）
	Description     string // 题目描述（富文本 HTML，可空）
	Required        string // 是否必填：0=否 1=是
	Sort            string // 题目排序
	CreatedAt       string //
	UpdatedAt       string //
}

// questionColumns holds the columns for the table plugin_linapro_eval_360_question.
var questionColumns = QuestionColumns{
	Id:              "id",
	QuestionnaireId: "questionnaire_id",
	DimensionId:     "dimension_id",
	Type:            "type",
	Title:           "title",
	Description:     "description",
	Required:        "required",
	Sort:            "sort",
	CreatedAt:       "created_at",
	UpdatedAt:       "updated_at",
}

// NewQuestionDao creates and returns a new DAO object for table data access.
func NewQuestionDao(handlers ...gdb.ModelHandler) *QuestionDao {
	return &QuestionDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_question",
		columns:  questionColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *QuestionDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *QuestionDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *QuestionDao) Columns() QuestionColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *QuestionDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *QuestionDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *QuestionDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
