// 本文件实现接口请求日志列表接口。
//
// 数据权限例外（design D10）：记录为平台级审计数据，调用方是员工或外部系统、不属于任何部门，
// 因此不接入部门数据范围，访问只由 monitor:apilog:list 权限点控制。
package apilog

import (
	"context"

	v1 "lina-plugin-linapro-monitor-apilog/backend/api/apilog/v1"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
)

// List 返回分页的接口请求日志列表。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	var (
		beginTime, endTime = parseRequestTimeRange(req.BeginTime, req.EndTime)
	)
	out, err := c.apiLogSvc.List(ctx, apilogsvc.ListInput{
		PageNum:    req.PageNum,
		PageSize:   req.PageSize,
		BeginTime:  beginTime,
		EndTime:    endTime,
		RouteOwner: req.RouteOwner,
		Path:       req.Path,
		CallerNo:   req.CallerNo,
		CallerName: req.CallerName,
		CallerID:   req.CallerId,
		StatusCode: req.StatusCode,
		StatusType: req.StatusType,
		RequestID:  req.RequestId,
	})
	if err != nil {
		return nil, err
	}

	items := make([]*v1.ApiLogListItem, 0, len(out.List))
	for _, item := range out.List {
		dto := toAPIApiLogListItem(item)
		items = append(items, &dto)
	}
	return &v1.ListRes{Items: items, Total: out.Total}, nil
}
