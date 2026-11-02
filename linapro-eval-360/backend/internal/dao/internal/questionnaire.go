// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionnaireDao is the data access object for the table plugin_linapro_eval_360_questionnaire.
type QuestionnaireDao struct {
	table    string               // table is the underlying table name of the DAO.
	group    string               // group is the database configuration group name of the current DAO.
	columns  QuestionnaireColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler   // handlers for customized model modification.
}

// QuestionnaireColumns defines and stores column names for the table plugin_linapro_eval_360_questionnaire.
type QuestionnaireColumns struct {
	Id          string //
	ProjectId   string // 所属项目 id
	Title       string //
	Description string //
	Published   string // 发布状态：0=未发布（不进评价者待办） 1=已发布
	Sort        string // 问卷排序
	CreatedAt   string //
	UpdatedAt   string //
}

// questionnaireColumns holds the columns for the table plugin_linapro_eval_360_questionnaire.
var questionnaireColumns = QuestionnaireColumns{
	Id:          "id",
	ProjectId:   "project_id",
	Title:       "title",
	Description: "description",
	Published:   "published",
	Sort:        "sort",
	CreatedAt:   "created_at",
	UpdatedAt:   "updated_at",
}

// NewQuestionnaireDao creates and returns a new DAO object for table data access.
func NewQuestionnaireDao(handlers ...gdb.ModelHandler) *QuestionnaireDao {
	return &QuestionnaireDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_questionnaire",
		columns:  questionnaireColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *QuestionnaireDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *QuestionnaireDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *QuestionnaireDao) Columns() QuestionnaireColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *QuestionnaireDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *QuestionnaireDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *QuestionnaireDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
