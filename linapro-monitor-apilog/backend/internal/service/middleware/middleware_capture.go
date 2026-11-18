// middleware_capture.go 实现记录主流程：先让请求走完，再判定、组装并异步落库。
//
// 顺序是刻意的：判定条件里包含「路由归属哪个插件」与「是否已有后台身份」，两者都只能在
// 处理器执行完之后才确定（动态路由元数据在分发时才挂到请求上下文上）。因此这里不在
// Next() 之前包装 Body，不命中的请求一分拷贝都不做。

package middleware

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/grpool"

	"lina-core/pkg/logger"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
	"lina-plugin-linapro-monitor-apilog/backend/internal/service/partition"
)

// recordScope 是一次请求的记录判定结果与路由信息。
type recordScope struct {
	// record 报告本次请求是否应当记录。
	record bool
	// routeOwner 是归属插件 ID。
	routeOwner string
	// routePath 是路由模式；动态插件取真实路径。
	routePath string
	// responseBody / responseContentType 是动态路由元数据里的响应兜底。
	responseBody        string
	responseContentType string
}

// Record 捕获一次已完成的请求，并在命中条件时异步落库。
func (s *serviceImpl) Record(request *ghttp.Request) {
	if request == nil {
		return
	}

	// 请求开始时间在 Next() 之前取一次：request_at 与 cost_ms 共用它，
	// request_at + cost_ms 即请求结束时间。
	startTime := time.Now()
	request.Middleware.Next()

	scope := s.resolveScope(request)
	if !scope.record {
		return
	}

	input := s.buildCreateInput(request, scope, startTime)
	s.persist(request, input)
}

// persist 异步写入记录。失败只记一条带 request_id 的 ERROR 日志，绝不改变已经发出的响应。
func (s *serviceImpl) persist(request *ghttp.Request, input apilogsvc.CreateInput) {
	dispatchCtx := request.GetNeverDoneCtx()
	if dispatchCtx == nil {
		dispatchCtx = request.Context()
	}
	requestID := input.RequestID

	err := grpool.AddWithRecover(dispatchCtx, func(taskCtx context.Context) {
		if recordErr := s.apiLogSvc.Create(taskCtx, input); recordErr != nil {
			logPersistFailure(taskCtx, requestID, recordErr)
		}
	}, func(_ context.Context, panicErr error) {
		logger.Errorf(request.Context(), "linapro-monitor-apilog：记录任务 panic request_id=%s: %v", requestID, panicErr)
	})
	if err != nil {
		// 任务池拒绝接收时不去同步兜底：记录日志不能拖慢或拖垮业务请求，
		// 丢掉一条日志的代价远小于把响应卡在数据库上。
		logger.Errorf(request.Context(), "linapro-monitor-apilog：调度记录任务失败 request_id=%s: %v", requestID, err)
	}
}

// logPersistFailure 把写入失败记成 ERROR，并区分「分区不存在」这一特定原因。
//
// 分区缺失的后果是当月所有日志都写不进去，报一句泛泛的 insert 失败会让人找错方向，
// 因此这里点名原因并指向补齐任务。
func logPersistFailure(ctx context.Context, requestID string, err error) {
	if partition.IsMissingError(err) {
		logger.Errorf(ctx, "linapro-monitor-apilog：写入失败（分区缺失，请检查分区补齐任务）request_id=%s: %v", requestID, err)
		return
	}
	logger.Errorf(ctx, "linapro-monitor-apilog：写入失败 request_id=%s: %v", requestID, err)
}

// buildCreateInput 把一次已完成的请求组装成纯内存的写入内容。
func (s *serviceImpl) buildCreateInput(
	request *ghttp.Request,
	scope recordScope,
	startTime time.Time,
) apilogsvc.CreateInput {
	var (
		status     = request.Response.Status
		errorMsg   string
		caller     = parseCaller(request)
		maskFields = s.maskFields(request.Context())
	)
	if status == 0 {
		status = 200
	}
	if reqErr := request.GetError(); reqErr != nil {
		errorMsg = reqErr.Error()
	}

	return apilogsvc.CreateInput{
		RequestAt:    startTime,
		RequestID:    gctx.CtxId(request.Context()),
		RouteOwner:   scope.routeOwner,
		Method:       request.Method,
		Path:         request.URL.Path,
		RoutePath:    scope.routePath,
		Query:        buildRecordQuery(request, maskFields),
		ClientIP:     request.GetClientIp(),
		UserAgent:    request.UserAgent(),
		CallerKind:   caller.Kind,
		CallerID:     caller.ID,
		CallerTenant: caller.Tenant,
		CallerName:   caller.Name,
		CallerNo:     caller.No,
		StatusCode:   status,
		CostMs:       int(time.Since(startTime).Milliseconds()),
		ReqHeaders:   buildRecordHeaders(request, maskFields),
		ReqBody:      buildRecordRequestBody(request, maskFields),
		RespBody:     buildRecordResponseBody(request, scope, maskFields),
		ErrorMsg:     errorMsg,
	}
}
