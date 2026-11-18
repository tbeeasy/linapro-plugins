// 本文件实现接口请求日志详情接口，以及列表与详情共用的时间参数解析。
package apilog

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/os/gtime"

	"lina-core/pkg/bizerr"
	v1 "lina-plugin-linapro-monitor-apilog/backend/api/apilog/v1"
	apilogsvc "lina-plugin-linapro-monitor-apilog/backend/internal/service/apilog"
)

// Get 返回一条日志的完整详情。requestAt 必填，用于分区剪枝。
func (c *ControllerV1) Get(ctx context.Context, req *v1.GetReq) (res *v1.GetRes, err error) {
	requestAt, parseErr := parseDetailRequestAt(req.RequestAt)
	if parseErr != nil {
		return nil, parseErr
	}
	record, err := c.apiLogSvc.Get(ctx, apilogsvc.GetInput{ID: req.Id, RequestAt: requestAt})
	if err != nil {
		return nil, err
	}
	return &v1.GetRes{ApiLogDetailItem: toAPIApiLogDetailItem(record)}, nil
}

// parseRequestTimeRange 解析列表的时间范围。
//
// 日期形式的结束边界扩展到当日 23:59:59：用户选「到 11 月 4 日」时预期包含当天，
// 而字面值 00:00:00 会把当天整天排除在外。
func parseRequestTimeRange(begin, end string) (time.Time, time.Time) {
	beginTime, beginErr := parseTimeBound(begin, false)
	endTime, endErr := parseTimeBound(end, true)
	if beginErr != nil || endErr != nil {
		// 解析失败交给服务层按「缺少时间范围」报参数错误，避免在这里返回两套语义。
		return time.Time{}, time.Time{}
	}
	return beginTime, endTime
}

// parseDetailRequestAt 解析详情的 requestAt。接受 Unix 毫秒（列表原样回传的形态）与 RFC3339。
func parseDetailRequestAt(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	if millis, ok := parseUnixMillis(trimmed); ok {
		return time.UnixMilli(millis), nil
	}
	parsed, err := gtime.StrToTime(trimmed)
	if err != nil {
		return time.Time{}, bizerr.WrapCode(err, apilogsvc.CodeApiLogInvalidTime)
	}
	return parsed.Time, nil
}

// parseTimeBound 解析一个时间边界；expandDateOnly 为 true 时把纯日期扩展到当日末尾。
func parseTimeBound(value string, expandDateOnly bool) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	if millis, ok := parseUnixMillis(trimmed); ok {
		return time.UnixMilli(millis), nil
	}
	parsed, err := gtime.StrToTime(trimmed)
	if err != nil {
		return time.Time{}, bizerr.WrapCode(err, apilogsvc.CodeApiLogInvalidTime)
	}
	result := parsed.Time
	if expandDateOnly && len(trimmed) == len("2006-01-02") {
		result = result.Add(24*time.Hour - time.Nanosecond)
	}
	return result, nil
}

// parseUnixMillis 尝试把纯数字串解析为 Unix 毫秒时间戳。
//
// 只认 12~14 位：10 位是秒级时间戳，交给下面的通用解析，避免把年份之类的短数字误判成时间。
func parseUnixMillis(value string) (int64, bool) {
	if len(value) < 12 || len(value) > 14 {
		return 0, false
	}
	var millis int64
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
		millis = millis*10 + int64(char-'0')
	}
	return millis, true
}
