// 本文件定义接口请求日志列表的请求与响应结构。
package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ListReq 是接口请求日志列表的查询请求。
//
// 时间范围必填：表按 request_at 做月度分区，不带范围会扫遍所有分区。
// 页面默认填最近 7 天；后端缺范围时直接拒绝，而不是悄悄放宽。
type ListReq struct {
	g.Meta `path:"/apilog/records" method:"get" tags:"接口请求日志" summary:"接口请求日志列表" dc:"按条件分页查询自动记录的非管理后台接口请求，按请求开始时间倒序；列表不返回请求头、请求体与响应体，查看需调用详情接口" permission:"monitor:apilog:list"`
	// 分页
	PageNum  int `json:"pageNum"  d:"1"  v:"min:1"         dc:"页码，不传默认第 1 页" eg:"1"`
	PageSize int `json:"pageSize" d:"10" v:"min:1|max:100" dc:"每页条数，不传默认 10，最大 100" eg:"10"`
	// 时间范围（必填）
	BeginTime string `json:"beginTime" v:"required" dc:"请求开始时间下界（含），支持 Unix 毫秒、RFC3339 或 YYYY-MM-DD；页面默认 7 天前" eg:"2026-10-28 00:00:00"`
	EndTime   string `json:"endTime"   v:"required" dc:"请求开始时间上界（不含），支持 Unix 毫秒、RFC3339 或 YYYY-MM-DD；只给日期时扩展到当天结束" eg:"2026-11-04 23:59:59"`
	// 筛选
	RouteOwner string `json:"routeOwner" dc:"按归属插件 ID 精确筛选；不传不过滤" eg:"linapro-eval-360"`
	Path       string `json:"path"       dc:"按请求路径前缀筛选；不传不过滤" eg:"/x/linapro-eval-360/"`
	CallerNo   string `json:"callerNo"   dc:"按调用方工号精确筛选；不传不过滤" eg:"GZ000001"`
	CallerName string `json:"callerName" dc:"按调用方姓名精确筛选；不传不过滤" eg:"刘鹏"`
	CallerId   string `json:"callerId"   dc:"按调用方标识（EmpID 或 app_key）精确筛选；不传不过滤" eg:"1840"`
	StatusCode *int   `json:"statusCode" dc:"按 HTTP 状态码精确筛选；不传包含所有状态码" eg:"200"`
	StatusType string `json:"statusType" v:"in:success,failure" dc:"按状态分类筛选：success=2xx，failure=非 2xx；与 statusCode 同时传时以本字段为准" eg:"failure"`
	RequestId  string `json:"requestId"  dc:"按请求 ID 精确筛选；不传不过滤" eg:"7f3c2a9b8e1d4f50a1b2c3d4e5f60718"`
}

// ListRes 是接口请求日志列表的响应。
type ListRes struct {
	Items []*ApiLogListItem `json:"items" dc:"接口请求日志列表" eg:"[]"`
	Total int               `json:"total" dc:"命中记录总数" eg:"500"`
}
