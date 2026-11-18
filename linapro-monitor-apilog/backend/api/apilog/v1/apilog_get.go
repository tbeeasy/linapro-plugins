// 本文件定义接口请求日志详情的请求与响应结构。
package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// GetReq 是接口请求日志详情的查询请求。
//
// RequestAt 必填：表按 request_at 分区，只按 id 查会退化成扫遍所有分区。
// 列表行本来就带 requestAt，调用方原样回传即可。
type GetReq struct {
	g.Meta    `path:"/apilog/records/{id}" method:"get" tags:"接口请求日志" summary:"接口请求日志详情" dc:"查询一条请求日志的完整内容，包括已脱敏的请求头、请求体与响应体；须同时传入列表行的 requestAt 用于分区剪枝" permission:"monitor:apilog:query"`
	Id        int64  `json:"id"        v:"required" dc:"日志记录 ID" eg:"1"`
	RequestAt string `json:"requestAt" v:"required" dc:"请求开始时间，取列表行的 requestAt（Unix 毫秒），也接受 RFC3339；用于分区剪枝" eg:"1793793677133"`
}

// GetRes 是接口请求日志详情的响应。
type GetRes struct {
	ApiLogDetailItem
}
