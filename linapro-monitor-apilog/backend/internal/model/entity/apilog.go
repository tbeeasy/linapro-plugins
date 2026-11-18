// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Apilog is the golang structure for table apilog.
type Apilog struct {
	Id           int64      `json:"id"           orm:"id"            description:"记录 ID，由序列生成（分区表不支持 identity 列）"`
	RequestAt    *time.Time `json:"requestAt"    orm:"request_at"    description:"分区键：请求开始时间，与 cost_ms 同一基准；列表筛选、排序与详情定位都用它"`
	RequestId    string     `json:"requestId"    orm:"request_id"    description:"请求 ID，与业务日志互查"`
	RouteOwner   string     `json:"routeOwner"   orm:"route_owner"   description:"路由归属插件 ID，用于按插件筛选与排除名单匹配"`
	Method       string     `json:"method"       orm:"method"        description:"HTTP 方法"`
	Path         string     `json:"path"         orm:"path"          description:"实际请求路径"`
	RoutePath    string     `json:"routePath"    orm:"route_path"    description:"路由模式，例如 /x/.../task/{id}；动态插件取真实路径"`
	Query        string     `json:"query"        orm:"query"         description:"原始查询串，敏感参数已脱敏"`
	ClientIp     string     `json:"clientIp"     orm:"client_ip"     description:"客户端 IP"`
	UserAgent    string     `json:"userAgent"    orm:"user_agent"    description:"User-Agent"`
	CallerKind   string     `json:"callerKind"   orm:"caller_kind"   description:"调用方类型：emp=员工 app_key=外部系统"`
	CallerId     string     `json:"callerId"     orm:"caller_id"     description:"调用方标识：emp 时为 EmpID，app_key 时为 app_key；唯一的机器关联键"`
	CallerTenant int        `json:"callerTenant" orm:"caller_tenant" description:"调用方租户 ID，无租户语义时为 0"`
	CallerName   string     `json:"callerName"   orm:"caller_name"   description:"调用方姓名快照，只用于显示与按人搜索"`
	CallerNo     string     `json:"callerNo"     orm:"caller_no"     description:"调用方工号快照，只用于显示与按人搜索"`
	StatusCode   int        `json:"statusCode"   orm:"status_code"   description:"HTTP 状态码"`
	CostMs       int        `json:"costMs"       orm:"cost_ms"       description:"耗时，单位毫秒"`
	ReqHeaders   string     `json:"reqHeaders"   orm:"req_headers"   description:"请求头 JSON，已脱敏"`
	ReqBody      string     `json:"reqBody"      orm:"req_body"      description:"完整请求体，已脱敏"`
	RespBody     string     `json:"respBody"     orm:"resp_body"     description:"完整响应体，已脱敏"`
	ErrorMsg     string     `json:"errorMsg"     orm:"error_msg"     description:"错误信息"`
	CreatedAt    *time.Time `json:"createdAt"    orm:"created_at"    description:"写库时间，由框架自动写入"`
}
