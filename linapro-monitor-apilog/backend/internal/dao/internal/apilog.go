// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ApilogDao is the data access object for the table plugin_linapro_monitor_apilog.
type ApilogDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  ApilogColumns      // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// ApilogColumns defines and stores column names for the table plugin_linapro_monitor_apilog.
type ApilogColumns struct {
	Id           string // 记录 ID，由序列生成（分区表不支持 identity 列）
	RequestAt    string // 分区键：请求开始时间，与 cost_ms 同一基准；列表筛选、排序与详情定位都用它
	RequestId    string // 请求 ID，与业务日志互查
	RouteOwner   string // 路由归属插件 ID，用于按插件筛选与排除名单匹配
	Method       string // HTTP 方法
	Path         string // 实际请求路径
	RoutePath    string // 路由模式，例如 /x/.../task/{id}；动态插件取真实路径
	Query        string // 原始查询串，敏感参数已脱敏
	ClientIp     string // 客户端 IP
	UserAgent    string // User-Agent
	CallerKind   string // 调用方类型：emp=员工 app_key=外部系统
	CallerId     string // 调用方标识：emp 时为 EmpID，app_key 时为 app_key；唯一的机器关联键
	CallerTenant string // 调用方租户 ID，无租户语义时为 0
	CallerName   string // 调用方姓名快照，只用于显示与按人搜索
	CallerNo     string // 调用方工号快照，只用于显示与按人搜索
	StatusCode   string // HTTP 状态码
	CostMs       string // 耗时，单位毫秒
	ReqHeaders   string // 请求头 JSON，已脱敏
	ReqBody      string // 完整请求体，已脱敏
	RespBody     string // 完整响应体，已脱敏
	ErrorMsg     string // 错误信息
	CreatedAt    string // 写库时间，由框架自动写入
}

// apilogColumns holds the columns for the table plugin_linapro_monitor_apilog.
var apilogColumns = ApilogColumns{
	Id:           "id",
	RequestAt:    "request_at",
	RequestId:    "request_id",
	RouteOwner:   "route_owner",
	Method:       "method",
	Path:         "path",
	RoutePath:    "route_path",
	Query:        "query",
	ClientIp:     "client_ip",
	UserAgent:    "user_agent",
	CallerKind:   "caller_kind",
	CallerId:     "caller_id",
	CallerTenant: "caller_tenant",
	CallerName:   "caller_name",
	CallerNo:     "caller_no",
	StatusCode:   "status_code",
	CostMs:       "cost_ms",
	ReqHeaders:   "req_headers",
	ReqBody:      "req_body",
	RespBody:     "resp_body",
	ErrorMsg:     "error_msg",
	CreatedAt:    "created_at",
}

// NewApilogDao creates and returns a new DAO object for table data access.
func NewApilogDao(handlers ...gdb.ModelHandler) *ApilogDao {
	return &ApilogDao{
		group:    "default",
		table:    "plugin_linapro_monitor_apilog",
		columns:  apilogColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ApilogDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ApilogDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ApilogDao) Columns() ApilogColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ApilogDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ApilogDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ApilogDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
