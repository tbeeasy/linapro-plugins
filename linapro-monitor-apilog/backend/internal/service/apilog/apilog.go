// Package apilog 实现 linapro-monitor-apilog 的记录读写服务：中间件写入一条请求记录，
// 管理端按时间、归属插件、路径与调用方检索。
//
// 边界：
//   - 只增不删：本包不提供任何 Delete / Clean 方法，全仓也不得出现针对本记录表的 DELETE；
//   - 记录为平台级审计数据，不接入部门数据范围（见 design D10 的数据权限例外）；
//   - 本包不依赖任何业务插件，调用方身份只从上下文约定键与写入时快照读取。
package apilog

import (
	"context"
	"time"

	entitymodel "lina-plugin-linapro-monitor-apilog/backend/internal/model/entity"
)

// 记录表的列名常量。查询构造只引用这里，避免散落的字符串字面量。
const (
	colID           = "id"
	colRequestAt    = "request_at"
	colRequestID    = "request_id"
	colRouteOwner   = "route_owner"
	colMethod       = "method"
	colPath         = "path"
	colRoutePath    = "route_path"
	colQuery        = "query"
	colClientIP     = "client_ip"
	colUserAgent    = "user_agent"
	colCallerKind   = "caller_kind"
	colCallerID     = "caller_id"
	colCallerTenant = "caller_tenant"
	colCallerName   = "caller_name"
	colCallerNo     = "caller_no"
	colStatusCode   = "status_code"
	colCostMs       = "cost_ms"
	colReqHeaders   = "req_headers"
	colReqBody      = "req_body"
	colRespBody     = "resp_body"
	colErrorMsg     = "error_msg"
)

// HTTP 状态分类筛选取值。
const (
	// StatusTypeSuccess 只查 2xx。
	StatusTypeSuccess = "success"
	// StatusTypeFailure 只查非 2xx。
	StatusTypeFailure = "failure"
)

// 列表查询的数量上限与默认时间窗口。
const (
	// maxPageSize 是单页上限，防止一次拉走整月分区。
	maxPageSize = 100
	// statusSuccessMin / statusSuccessMax 是 2xx 判定的下界（含）与上界（不含）。
	statusSuccessMin = 200
	statusSuccessMax = 300
)

// Service 定义接口请求日志的写入与查询契约。刻意不含任何删除方法。
type Service interface {
	// Create 写入一条请求记录。request_at 由调用方显式给出（请求开始时间），
	// 不依赖列默认值——异步排队的延迟不能让时间筛选变成写库时间语义。
	// created_at 是写库时间，交给框架自动写入。
	Create(ctx context.Context, in CreateInput) error
	// List 返回分页的日志列表（不含请求头与请求体）。
	List(ctx context.Context, in ListInput) (*ListOutput, error)
	// Get 返回一条日志的完整详情。RequestAt 必填，用于分区剪枝。
	Get(ctx context.Context, in GetInput) (*ApiLogEntity, error)
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的实现。它不持有任何宿主业务能力——记录读写只碰本插件的表。
type serviceImpl struct{}

// New 构造接口请求日志服务。
func New() Service {
	return &serviceImpl{}
}

// ApiLogEntity 是插件本地生成的记录实体别名。
type ApiLogEntity = entitymodel.Apilog

// CreateInput 定义一条请求记录的写入内容。字段与表列一一对应。
type CreateInput struct {
	// RequestAt 是请求开始时间（与 CostMs 同一基准），必填。
	RequestAt    time.Time
	RequestID    string
	RouteOwner   string
	Method       string
	Path         string
	RoutePath    string
	Query        string
	ClientIP     string
	UserAgent    string
	CallerKind   string
	CallerID     string
	CallerTenant int
	CallerName   string
	CallerNo     string
	StatusCode   int
	CostMs       int
	ReqHeaders   string
	ReqBody      string
	RespBody     string
	ErrorMsg     string
}

// ListInput 定义列表筛选条件。时间范围必填。
type ListInput struct {
	PageNum    int
	PageSize   int
	BeginTime  time.Time
	EndTime    time.Time
	RouteOwner string
	Path       string
	CallerNo   string
	CallerName string
	CallerID   string
	StatusCode *int
	StatusType string
	RequestID  string
}

// ListOutput 定义列表查询结果。
type ListOutput struct {
	List  []*ApiLogEntity
	Total int
}

// GetInput 定义详情查询条件。RequestAt 必填，用于分区剪枝。
type GetInput struct {
	ID        int64
	RequestAt time.Time
}
