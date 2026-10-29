// errors.go 定义飞书侧失败的结构化错误，让调用方不必解析错误字符串就能拿到
// code / req_id 做日志与告警。错误文本与旧版逐字一致，不影响既有日志检索。

package larkim

import "fmt"

// FeishuError 是飞书业务失败（resp.Success() == false）的结构化形式。
//
// 为什么需要它：外部调用方要把「飞书错误码 + 请求 ID」写进请求日志供事后向飞书侧追查，
// 从格式化字符串里正则提取既脆弱又难读。Error() 输出保持原有的
// "larkim: 发送<种类>失败 req_id=... code=... msg=..." 形态。
type FeishuError struct {
	// Kind 是消息种类（交互卡片 / 文本），用于错误文案。
	Kind string
	// Code 是飞书业务错误码。
	Code int
	// Msg 是飞书错误描述。
	Msg string
	// ReqID 是飞书请求 ID（X-Tt-Logid），向飞书侧排查时必带。
	ReqID string
	// Detail 是字段级违规详情（可为空）。
	Detail string
}

// Error 实现 error；文案与抽取前的 gerror 输出一致。
func (e *FeishuError) Error() string {
	if e == nil {
		return "larkim: 飞书返回错误"
	}
	return fmt.Sprintf("larkim: 发送%s失败 req_id=%s code=%d msg=%s%s", e.Kind, e.ReqID, e.Code, e.Msg, e.Detail)
}

// newFeishuError 由飞书响应构造结构化错误。
func newFeishuError(kind string, code int, msg, reqID, detail string) *FeishuError {
	return &FeishuError{Kind: kind, Code: code, Msg: msg, ReqID: reqID, Detail: detail}
}
