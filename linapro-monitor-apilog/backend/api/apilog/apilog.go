// Package apilog 声明 linapro-monitor-apilog 源码插件对外暴露的 HTTP 控制器契约。
package apilog

import (
	"context"

	v1 "lina-plugin-linapro-monitor-apilog/backend/api/apilog/v1"
)

// IApilogV1 定义接口请求日志的 HTTP 处理器。
type IApilogV1 interface {
	// List 返回分页的接口请求日志列表（不含请求头、请求体与响应体）。
	List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error)
	// Get 返回一条日志的完整详情（含请求头、请求体与响应体）。
	Get(ctx context.Context, req *v1.GetReq) (res *v1.GetRes, err error)
}
