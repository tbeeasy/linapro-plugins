// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionVisibilityDao is the data access object for the table plugin_linapro_eval_360_question_visibility.
type QuestionVisibilityDao struct {
	table    string                    // table is the underlying table name of the DAO.
	group    string                    // group is the database configuration group name of the current DAO.
	columns  QuestionVisibilityColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler        // handlers for customized model modification.
}

// QuestionVisibilityColumns defines and stores column names for the table plugin_linapro_eval_360_question_visibility.
type QuestionVisibilityColumns struct {
	Id             string //
	QuestionId     string // 所属题目 id
	VisibleRoleIds string // 可见角色 id 逗号分隔，空=所有角色可见
	CreatedAt      string //
	UpdatedAt      string //
}

// questionVisibilityColumns holds the columns for the table plugin_linapro_eval_360_question_visibility.
var questionVisibilityColumns = QuestionVisibilityColumns{
	Id:             "id",
	QuestionId:     "question_id",
	VisibleRoleIds: "visible_role_ids",
	CreatedAt:      "created_at",
	UpdatedAt:      "updated_at",
}

// NewQuestionVisibilityDao creates and returns a new DAO object for table data access.
func NewQuestionVisibilityDao(handlers ...gdb.ModelHandler) *QuestionVisibilityDao {
	return &QuestionVisibilityDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_question_visibility",
		columns:  questionVisibilityColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *QuestionVisibilityDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *QuestionVisibilityDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *QuestionVisibilityDao) Columns() QuestionVisibilityColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *QuestionVisibilityDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *QuestionVisibilityDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *QuestionVisibilityDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
