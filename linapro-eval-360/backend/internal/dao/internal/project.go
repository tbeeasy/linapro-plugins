// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ProjectDao is the data access object for the table plugin_linapro_eval_360_project.
type ProjectDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  ProjectColumns     // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// ProjectColumns defines and stores column names for the table plugin_linapro_eval_360_project.
type ProjectColumns struct {
	Id          string //
	Name        string // 项目名称
	Description string // 项目描述
	StartAt     string // 评估开始时间
	EndAt       string // 项目统一截止时间；超过后拒绝提交
	ClosedAt    string // 提前结束时间戳，空=未提前结束；清空即重新开启；不改 end_at
	CreatedBy   string // 创建管理员 sys_user.id
	CreatedAt   string //
	UpdatedAt   string //
}

// projectColumns holds the columns for the table plugin_linapro_eval_360_project.
var projectColumns = ProjectColumns{
	Id:          "id",
	Name:        "name",
	Description: "description",
	StartAt:     "start_at",
	EndAt:       "end_at",
	ClosedAt:    "closed_at",
	CreatedBy:   "created_by",
	CreatedAt:   "created_at",
	UpdatedAt:   "updated_at",
}

// NewProjectDao creates and returns a new DAO object for table data access.
func NewProjectDao(handlers ...gdb.ModelHandler) *ProjectDao {
	return &ProjectDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_project",
		columns:  projectColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ProjectDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ProjectDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ProjectDao) Columns() ProjectColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ProjectDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ProjectDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ProjectDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
