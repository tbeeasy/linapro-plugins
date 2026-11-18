// apilog_impl.go 实现接口请求日志的写入与查询。
//
// 记录只增不删：本文件刻意不含 Delete / Clean，也不拼装任何 DELETE 语句。
// 表按 request_at 做月度分区，因此列表必须带时间范围、详情必须带 request_at，
// 否则会退化为全分区扫描。

package apilog

import (
	"context"
	"time"

	"lina-core/pkg/bizerr"
	"lina-plugin-linapro-monitor-apilog/backend/internal/dao"
	"lina-plugin-linapro-monitor-apilog/backend/internal/model/do"
)

// Create 写入一条请求记录。
//
// request_at 显式赋值：列默认值只是兜底，依赖它会让异步排队的延迟变成时间筛选语义，
// 跨月边界的请求还会落进下一个月的分区。created_at 不赋值，由框架自动写入写库时间。
func (s *serviceImpl) Create(ctx context.Context, in CreateInput) error {
	requestAt := in.RequestAt
	_, err := dao.Apilog.Ctx(ctx).Data(do.Apilog{
		RequestAt:    &requestAt,
		RequestId:    in.RequestID,
		RouteOwner:   in.RouteOwner,
		Method:       in.Method,
		Path:         in.Path,
		RoutePath:    in.RoutePath,
		Query:        in.Query,
		ClientIp:     in.ClientIP,
		UserAgent:    in.UserAgent,
		CallerKind:   in.CallerKind,
		CallerId:     in.CallerID,
		CallerTenant: in.CallerTenant,
		CallerName:   in.CallerName,
		CallerNo:     in.CallerNo,
		StatusCode:   in.StatusCode,
		CostMs:       in.CostMs,
		ReqHeaders:   in.ReqHeaders,
		ReqBody:      in.ReqBody,
		RespBody:     in.RespBody,
		ErrorMsg:     in.ErrorMsg,
	}).Insert()
	return err
}

// List 查询分页的日志列表。筛选在数据库侧完成，列表不返回请求头与请求体。
func (s *serviceImpl) List(ctx context.Context, in ListInput) (*ListOutput, error) {
	if in.BeginTime.IsZero() || in.EndTime.IsZero() {
		return nil, bizerr.NewCode(CodeApiLogTimeRangeRequired)
	}
	pageNum := in.PageNum
	if pageNum <= 0 {
		pageNum = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	total, err := s.filteredModel(ctx, in).Count()
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiLogQueryFailed)
	}

	// 默认按时间倒序：排查问题总是从最近一条看起。同一时间戳再用 id 倒序保证稳定排序。
	list := make([]*ApiLogEntity, 0)
	err = s.filteredModel(ctx, in).
		OrderDesc(colRequestAt).
		OrderDesc(colID).
		Page(pageNum, pageSize).
		// 列表不返回请求头、请求体、响应体：它们由详情权限单独控制。
		FieldsEx(colReqHeaders, colReqBody, colRespBody).
		Scan(&list)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiLogQueryFailed)
	}
	return &ListOutput{List: list, Total: total}, nil
}

// Get 查询一条日志的完整详情。requestAt 必填，用于分区剪枝；缺少时直接报参数错误，
// 不退化成全分区扫描——让调用方显式面对这个契约，比让它悄悄变慢好。
func (s *serviceImpl) Get(ctx context.Context, in GetInput) (*ApiLogEntity, error) {
	if in.RequestAt.IsZero() {
		return nil, bizerr.NewCode(CodeApiLogRequestAtRequired)
	}
	// request_at 落库精度是微秒，接口只暴露毫秒：按 [requestAt, requestAt+1ms) 匹配，
	// 等值比较会因丢掉的微秒位永远查不到。区间依然只命中一个分区。
	requestAtFrom := in.RequestAt.Truncate(time.Millisecond)
	var record *ApiLogEntity
	err := dao.Apilog.Ctx(ctx).
		Where(colID, in.ID).
		WhereGTE(colRequestAt, requestAtFrom).
		WhereLT(colRequestAt, requestAtFrom.Add(time.Millisecond)).
		Scan(&record)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeApiLogQueryFailed)
	}
	if record == nil {
		return nil, bizerr.NewCode(CodeApiLogNotFound)
	}
	return record, nil
}
