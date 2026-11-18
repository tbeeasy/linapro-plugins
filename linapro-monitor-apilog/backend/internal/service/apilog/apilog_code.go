// 本文件集中定义接口请求日志的业务错误码；中英文案在 manifest/i18n/{zh-CN,en-US}/error.json。

package apilog

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeApiLogNotFound 请求日志不存在。
	CodeApiLogNotFound = bizerr.MustDefine(
		"MONITOR_APILOG_NOT_FOUND",
		"Interface request log does not exist",
		gcode.CodeNotFound,
	)
	// CodeApiLogTimeRangeRequired 列表缺少必填的请求时间范围。
	CodeApiLogTimeRangeRequired = bizerr.MustDefine(
		"MONITOR_APILOG_TIME_RANGE_REQUIRED",
		"A request time range is required",
		gcode.CodeInvalidParameter,
	)
	// CodeApiLogRequestAtRequired 详情缺少 requestAt。
	// 表按 request_at 分区，缺它会扫遍所有分区，因此直接拒绝而不是悄悄退化。
	CodeApiLogRequestAtRequired = bizerr.MustDefine(
		"MONITOR_APILOG_REQUEST_AT_REQUIRED",
		"requestAt is required to read the detail: records are partitioned by it",
		gcode.CodeInvalidParameter,
	)
	// CodeApiLogInvalidTime 请求时间参数无法解析。
	CodeApiLogInvalidTime = bizerr.MustDefine(
		"MONITOR_APILOG_INVALID_TIME",
		"Invalid request time value",
		gcode.CodeInvalidParameter,
	)
	// CodeApiLogQueryFailed 查询记录表失败（数据库错误）。
	CodeApiLogQueryFailed = bizerr.MustDefine(
		"MONITOR_APILOG_QUERY_FAILED",
		"Failed to query interface request logs",
		gcode.CodeInternalError,
	)
)
