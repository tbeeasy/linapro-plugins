// Package job 提供招聘流水线定时任务的共享工具函数。
package job

import (
	"strconv"
	"strings"
	"time"

	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// asString 从写路径的 lark.WriteRow 值（any）取字符串：WriteRow 在 typeFields 前承载的是
// 插件构造的字符串值，命中 string 直接取用、其余类型（防御性）返回空串。用于把写行里的某列
// 值当键使用的场景（如 mergeReportCreates 按 applicationId 合并）。读路径的 lark.Row 值恒为
// string，无需本函数。
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// typeFields 把承载字符串值的 lark.WriteRow 按飞书列类型就地转成强类型，供共享库 verbatim 序列化：
//   - TypeDateTime → int64 毫秒（ParseEpochMillisUTC，ISO 无时区按 UTC、带 Z 按绝对时刻）；
//   - TypeNumber → float64（ParseNumberPlain）；
//   - 其余列保持 string 原样。
//
// 解析失败或空值的日期/数字列被删除（跳列），与改造前共享库默认 Encoder（UTC 日期 + Plain
// 数字）在 rowToFields 中的跳列行为逐值等价——recruit 各写入点均走默认日期/数字解析。
// 人员/附件列不在 WriteRow 中（各走 Persons/Attachments 旁路），无需处理；已是非字符串的值原样保留。
func typeFields(row lark.WriteRow, fieldTypes map[string]int) {
	for k, v := range row {
		s, ok := v.(string)
		if !ok {
			continue // 已是强类型（或非字符串），原样保留。
		}
		switch fieldTypes[k] {
		case larkbitablesdk.TypeDateTime:
			if ms, ok := lark.ParseEpochMillisUTC(s); ok {
				row[k] = ms
			} else {
				delete(row, k) // 空串或无法解析的时间跳列，避免 DatetimeFieldConvFail。
			}
		case larkbitablesdk.TypeNumber:
			if n, ok := lark.ParseNumberPlain(s); ok {
				row[k] = n
			} else {
				delete(row, k) // 无法解析的数字跳列。
			}
		}
	}
}

// typeCreateFields 对一批 CreateOp 的 Fields 依次做 typeFields 类型化（就地）。
func typeCreateFields(creates []lark.CreateOp, fieldTypes map[string]int) {
	for _, op := range creates {
		typeFields(op.Fields, fieldTypes)
	}
}

// typeUpdateFields 对一批 UpdateOp 的 Fields 依次做 typeFields 类型化（就地）。
func typeUpdateFields(updates []lark.UpdateOp, fieldTypes map[string]int) {
	for _, op := range updates {
		typeFields(op.Fields, fieldTypes)
	}
}

// normalizeID 归一化 applicationId（兼容科学计数法）
func normalizeID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return strconv.FormatInt(int64(f), 10)
	}
	return s
}

// formatMokaTimeRange 将开始和结束时刻格式化为 Moka API 所需的 ISO 8601 格式。
// 用于 updateAtStartTime/updateAtEndTime 参数。
// Moka 官方示例为 YYYY-MM-DDTHH:mm:ss.sssZ（UTC，Z 结尾），
// 其日期解析不接受 +08:00 偏移形式，故此处统一转为 UTC 并以 Z 结尾输出。
func formatMokaTimeRange(start, end time.Time) (startStr, endStr string) {
	const layout = "2006-01-02T15:04:05.000Z07:00"
	return start.UTC().Format(layout), end.UTC().Format(layout)
}

// formatMokaTimestampRange 将时间转换为 Unix 毫秒时间戳字符串。
// 用于 applicationAppliedAtStartTime/applicationAppliedAtEndTime 参数。
func formatMokaTimestampRange(start, end time.Time) (startStr, endStr string) {
	return strconv.FormatInt(start.UnixMilli(), 10), strconv.FormatInt(end.UnixMilli(), 10)
}

// yesterdayStartInBeijing 计算北京时间的昨日 0 点。
func yesterdayStartInBeijing(now time.Time) time.Time {
	loc := time.FixedZone("CST", 8*3600)
	n := now.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day()-1, 0, 0, 0, 0, loc)
}
