// apilog_query.go 实现列表与计数共用的筛选条件装配。

package apilog

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-plugin-linapro-monitor-apilog/backend/internal/dao"
)

// filteredModel 返回已套用全部筛选条件（不含时间范围以外差异）的模型。
// 列表与计数共用同一套条件，避免两处漂移。
func (s *serviceImpl) filteredModel(ctx context.Context, in ListInput) *gdb.Model {
	model := dao.Apilog.Ctx(ctx).
		WhereGTE(colRequestAt, in.BeginTime).
		WhereLT(colRequestAt, in.EndTime)
	if in.RouteOwner != "" {
		model = model.Where(colRouteOwner, in.RouteOwner)
	}
	if in.Path != "" {
		// 前缀匹配才能走 (path) 索引之外的时间索引组合；同时时间范围已在上面收窄，
		// 因此不会退化为全表扫描。
		model = model.WhereLike(colPath, in.Path+"%")
	}
	// 调用方三列互不覆盖：填几个就收紧几个（同一行描述同一个人，多填理应缩小结果集）。
	if in.CallerNo != "" {
		model = model.Where(colCallerNo, in.CallerNo)
	}
	if in.CallerName != "" {
		model = model.Where(colCallerName, in.CallerName)
	}
	if in.CallerID != "" {
		model = model.Where(colCallerID, in.CallerID)
	}
	switch in.StatusType {
	case StatusTypeSuccess:
		model = model.WhereGTE(colStatusCode, statusSuccessMin).WhereLT(colStatusCode, statusSuccessMax)
	case StatusTypeFailure:
		model = model.Where("("+colStatusCode+" < ? OR "+colStatusCode+" >= ?)", statusSuccessMin, statusSuccessMax)
	case "":
		if in.StatusCode != nil {
			model = model.Where(colStatusCode, *in.StatusCode)
		}
	}
	if in.RequestID != "" {
		model = model.Where(colRequestID, in.RequestID)
	}
	return model
}
