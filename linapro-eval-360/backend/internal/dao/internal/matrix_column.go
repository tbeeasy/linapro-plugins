// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// MatrixColumnDao is the data access object for the table plugin_linapro_eval_360_matrix_column.
type MatrixColumnDao struct {
	table    string              // table is the underlying table name of the DAO.
	group    string              // group is the database configuration group name of the current DAO.
	columns  MatrixColumnColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler  // handlers for customized model modification.
}

// MatrixColumnColumns defines and stores column names for the table plugin_linapro_eval_360_matrix_column.
type MatrixColumnColumns struct {
	Id                 string //
	QuestionId         string // 所属矩阵题 id
	Label              string // 列文案
	Score              string // 列分值，NUMERIC
	Sort               string // 列排序
	CreatedAt          string //
	UpdatedAt          string //
	AllowComment       string // 是否允许附评语：0=否 1=是
	CommentRequired    string // 评语是否必填：0=否 1=是（仅允许评语时生效，提交时校验）
	CommentPlaceholder string // 评语输入框占位提示（提示语），可空
	CommentPresets     string // 预设填空候选，JSON 数组文本；空=无预设（自由输入）
	CommentAllowCustom string // 是否允许自定义输入：0=只能选预设 1=可自定义（仅在有预设时生效）
}

// matrixColumnColumns holds the columns for the table plugin_linapro_eval_360_matrix_column.
var matrixColumnColumns = MatrixColumnColumns{
	Id:                 "id",
	QuestionId:         "question_id",
	Label:              "label",
	Score:              "score",
	Sort:               "sort",
	CreatedAt:          "created_at",
	UpdatedAt:          "updated_at",
	AllowComment:       "allow_comment",
	CommentRequired:    "comment_required",
	CommentPlaceholder: "comment_placeholder",
	CommentPresets:     "comment_presets",
	CommentAllowCustom: "comment_allow_custom",
}

// NewMatrixColumnDao creates and returns a new DAO object for table data access.
func NewMatrixColumnDao(handlers ...gdb.ModelHandler) *MatrixColumnDao {
	return &MatrixColumnDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_matrix_column",
		columns:  matrixColumnColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *MatrixColumnDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *MatrixColumnDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *MatrixColumnDao) Columns() MatrixColumnColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *MatrixColumnDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *MatrixColumnDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *MatrixColumnDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
