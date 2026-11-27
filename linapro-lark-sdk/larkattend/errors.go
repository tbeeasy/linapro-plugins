// errors.go 定义飞书假勤调用失败的结构化错误，让调用方不必解析错误字符串就能拿到
// code / req_id 做日志、告警与重试判定。

package larkattend

import (
	"errors"
	"fmt"
)

// 飞书业务错误码中的限流与瞬时故障：命中后由 callWithBackoff 退避重试。
const (
	// codeRateLimited 是触发限流。
	codeRateLimited = 99991400
	// codeServiceTooBusy 是服务内部忙，可重试。
	codeServiceTooBusy = 99991401
	// codeInternalError 是飞书内部错误，可重试。
	codeInternalError = 999999
)

// ErrInvalidArgument 表示调用方传入的参数不合法（例如空的考勤组 ID、非 yyyyMMdd 的日期）。
var ErrInvalidArgument = errors.New("larkattend: invalid argument")

// FeishuError 是一次假勤接口调用失败的结构化形式。
//
// Code 为 0 表示调用在网络层或 HTTP 层就失败了（连接失败、超时、非 2xx），
// 此时 Msg 是底层错误文本；Code 非 0 表示飞书返回了业务错误码。
type FeishuError struct {
	// Action 是失败的操作名（例如 attendance.user_tasks.query），用于日志与告警定位。
	Action string
	// Code 是飞书业务错误码；0 表示非业务失败。
	Code int
	// Msg 是飞书错误描述或底层错误文本。
	Msg string
	// ReqID 是飞书请求 ID（X-Tt-Logid），向飞书侧排查时必带；可能为空。
	ReqID string
}

// Error 实现 error，文案保持稳定便于日志检索。
func (e *FeishuError) Error() string {
	if e == nil {
		return "larkattend: 飞书返回错误"
	}
	return fmt.Sprintf("larkattend: %s 失败 req_id=%s code=%d msg=%s", e.Action, e.ReqID, e.Code, e.Msg)
}

// newFeishuError 由失败响应或底层错误构造结构化错误。
func newFeishuError(action string, code int, msg, reqID string) *FeishuError {
	return &FeishuError{Action: action, Code: code, Msg: msg, ReqID: reqID}
}
