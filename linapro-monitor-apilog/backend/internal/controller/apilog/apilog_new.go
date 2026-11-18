// Package apilog 实现 linapro-monitor-apilog 插件的 HTTP 控制器。
package apilog

import (
	"lina-core/pkg/apitime"
	apilogapi "lina-plugin-linapro-monitor-apilog/backend/api/apilog"
	v1 "lina-plugin-linapro-monitor-apilog/backend/api/apilog/v1"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
)

// ControllerV1 是接口请求日志控制器。
type ControllerV1 struct {
	apiLogSvc apilogsvc.Service // 插件自己的记录查询服务
}

// NewV1 创建并返回接口请求日志控制器。
func NewV1(apiLogSvc apilogsvc.Service) apilogapi.IApilogV1 {
	return &ControllerV1{apiLogSvc: apiLogSvc}
}

// toAPIApiLogListItem 把服务层实体投影为列表 DTO（不含请求头与请求体）。
func toAPIApiLogListItem(entity *apilogsvc.ApiLogEntity) v1.ApiLogListItem {
	if entity == nil {
		return v1.ApiLogListItem{}
	}
	var requestAt int64
	if millis := apitime.Milli(entity.RequestAt); millis != nil {
		requestAt = *millis
	}
	return v1.ApiLogListItem{
		Id:           entity.Id,
		RequestAt:    requestAt,
		RequestId:    entity.RequestId,
		RouteOwner:   entity.RouteOwner,
		Method:       entity.Method,
		Path:         entity.Path,
		RoutePath:    entity.RoutePath,
		Query:        entity.Query,
		ClientIp:     entity.ClientIp,
		UserAgent:    entity.UserAgent,
		CallerKind:   entity.CallerKind,
		CallerId:     entity.CallerId,
		CallerTenant: entity.CallerTenant,
		CallerName:   entity.CallerName,
		CallerNo:     entity.CallerNo,
		StatusCode:   entity.StatusCode,
		CostMs:       entity.CostMs,
		ErrorMsg:     entity.ErrorMsg,
	}
}

// toAPIApiLogDetailItem 把服务层实体投影为详情 DTO（含已脱敏的载荷）。
func toAPIApiLogDetailItem(entity *apilogsvc.ApiLogEntity) v1.ApiLogDetailItem {
	if entity == nil {
		return v1.ApiLogDetailItem{}
	}
	return v1.ApiLogDetailItem{
		ApiLogListItem: toAPIApiLogListItem(entity),
		ReqHeaders:     entity.ReqHeaders,
		ReqBody:        entity.ReqBody,
		RespBody:       entity.RespBody,
	}
}
