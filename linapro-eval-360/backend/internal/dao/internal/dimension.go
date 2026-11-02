// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// DimensionDao is the data access object for the table plugin_linapro_eval_360_dimension.
type DimensionDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  DimensionColumns   // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// DimensionColumns defines and stores column names for the table plugin_linapro_eval_360_dimension.
type DimensionColumns struct {
	Id              string //
	QuestionnaireId string // 所属问卷 id
	Name            string // 维度名称
	Sort            string // 维度排序
	CreatedAt       string //
	UpdatedAt       string //
}

// dimensionColumns holds the columns for the table plugin_linapro_eval_360_dimension.
var dimensionColumns = DimensionColumns{
	Id:              "id",
	QuestionnaireId: "questionnaire_id",
	Name:            "name",
	Sort:            "sort",
	CreatedAt:       "created_at",
	UpdatedAt:       "updated_at",
}

// NewDimensionDao creates and returns a new DAO object for table data access.
func NewDimensionDao(handlers ...gdb.ModelHandler) *DimensionDao {
	return &DimensionDao{
		group:    "default",
		table:    "plugin_linapro_eval_360_dimension",
		columns:  dimensionColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *DimensionDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *DimensionDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *DimensionDao) Columns() DimensionColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *DimensionDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *DimensionDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *DimensionDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
