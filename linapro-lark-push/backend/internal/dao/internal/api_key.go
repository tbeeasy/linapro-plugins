// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ApiKeyDao is the data access object for the table plugin_linapro_lark_push_api_key.
type ApiKeyDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  ApiKeyColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// ApiKeyColumns defines and stores column names for the table plugin_linapro_lark_push_api_key.
type ApiKeyColumns struct {
	Id            string // Credential ID
	Name          string // Caller display name
	AppKey        string // Public app key carried in the X-App-Key header
	Secret        string // HMAC-SHA256 signing secret
	PushGroup     string // Bound push group name in push_groups config
	Status        string // Status from dict sys_normal_disable: 1=normal 0=disabled
	Remark        string // Remark
	CreatedAt     string // Creation time
	UpdatedAt     string // Update time
	DeletedAt     string // Deletion time
	ChatId        string // Feishu group chat_id pushed with every request of this credential; empty means no group push
	ChatPushAppId string // Push app ID in the bound group used for the chat; empty means the group default (single push app)
}

// apiKeyColumns holds the columns for the table plugin_linapro_lark_push_api_key.
var apiKeyColumns = ApiKeyColumns{
	Id:            "id",
	Name:          "name",
	AppKey:        "app_key",
	Secret:        "secret",
	PushGroup:     "push_group",
	Status:        "status",
	Remark:        "remark",
	CreatedAt:     "created_at",
	UpdatedAt:     "updated_at",
	DeletedAt:     "deleted_at",
	ChatId:        "chat_id",
	ChatPushAppId: "chat_push_app_id",
}

// NewApiKeyDao creates and returns a new DAO object for table data access.
func NewApiKeyDao(handlers ...gdb.ModelHandler) *ApiKeyDao {
	return &ApiKeyDao{
		group:    "default",
		table:    "plugin_linapro_lark_push_api_key",
		columns:  apiKeyColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ApiKeyDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ApiKeyDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ApiKeyDao) Columns() ApiKeyColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ApiKeyDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ApiKeyDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ApiKeyDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
