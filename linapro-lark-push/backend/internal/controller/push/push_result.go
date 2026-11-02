// push_result.go 承载文本 / 文件接口共用的收件人投影、投递结果投影与响应收尾逻辑。

package push

import (
	"context"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"

	v1 "lina-plugin-linapro-lark-push/backend/api/push/v1"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
)

// bizSourceExternal 是外部 HTTP 调用的业务来源标识，写进请求日志便于与插件内调用区分。
const bizSourceExternal = "http:external"

// noRecipientReason 是「没有个人收件人、凭据也未配置群聊」时的参数错误原因。
const noRecipientReason = "recipients 不能为空：该签名凭据未配置群聊，请传个人收件人或在后台为凭据配置群聊 chat_id"

// invalidParam 构造参数错误（HTTP 200，code 非 0）。
func invalidParam(reason string) error {
	return bizerr.NewCode(larkpushcode.CodeInvalidParam, bizerr.P("item", reason))
}

// toRecipients 把个人收件人与签名凭据配置的群聊合并为门面收件人列表（人在前、群在后）。
// 群聊只取自凭据配置，调用方不能指定；凭据未配置群聊时只含个人收件人。
func toRecipients(recipients []v1.RecipientReq, identity signature.Identity) []larkpushcap.Recipient {
	out := make([]larkpushcap.Recipient, 0, len(recipients)+1)
	for _, item := range recipients {
		out = append(out, larkpushcap.Recipient{EmployeeNo: item.EmployeeNo, UserID: item.UserID})
	}
	if identity.ChatID != "" {
		out = append(out, larkpushcap.Recipient{ChatID: identity.ChatID, PushAppID: identity.ChatPushAppID})
	}
	return out
}

// finishSend 把门面投递结果收尾为接口返回值并记录请求日志。
//
// 语义：请求级错误（参数、配置、能力不可用）返回 nil 结果与原错误，并按错误码设置 HTTP 状态码；
// 存在失败项时返回投递结果与 CodeSendPartialFailed（HTTP 200），失败明细在 data.failed；
// 全部成功返回投递结果与 nil 错误。
func finishSend(
	ctx context.Context,
	r *ghttp.Request,
	identity signature.Identity,
	bizRef string,
	recipientCount int,
	result larkpushcap.SendResult,
	err error,
) (*v1.SendResult, error) {
	requestID := gctx.CtxId(ctx)
	if err != nil {
		// 请求级失败（分组无可用推送应用、配置非法、能力未就绪等）没有投递明细，send 服务不会记录，
		// 在这里补一条带调用方标识的日志，否则签名通过后的失败无从对账。
		logger.Warningf(ctx,
			"lark-push http: 请求失败 request_id=%s path=%s key_id=%s app_key=%s group=%s biz_ref=%s 收件人=%d err=%v",
			requestID, r.URL.Path, identity.KeyID, identity.AppKey, identity.Group, bizRef, recipientCount, err)
		return nil, larkpushcode.WithHTTPStatus(r, err)
	}
	res := toResponse(result)
	// 日志带 request_id、凭据标识与 app_key（均为非敏感标识，不含 secret 与签名原文），
	// 便于与调用方对账。失败明细（含错误详情）由 send 服务记录，这里补记成功明细。
	for _, d := range result.Deliveries {
		if d.Succeeded() {
			logger.Infof(ctx,
				"lark-push http: 投递成功 request_id=%s push_app=%s(%s) 工号=%s user_id=%s chat_id=%s message_id=%s",
				requestID, d.PushAppName, d.PushAppID, d.EmployeeNo, d.UserID, d.ChatID, d.MessageID)
		}
	}
	logger.Infof(ctx,
		"lark-push http: request_id=%s path=%s key_id=%s app_key=%s group=%s biz_ref=%s 收件人=%d 明细=%d 失败项=%d",
		requestID, r.URL.Path, identity.KeyID, identity.AppKey, identity.Group, bizRef,
		recipientCount, len(result.Deliveries), len(res.Failed))
	if !res.AllSucceeded {
		return &res, bizerr.NewCode(larkpushcode.CodeSendPartialFailed, bizerr.P("count", len(res.Failed)))
	}
	return &res, nil
}

// toResponse 把门面结果投影为接口结果：只列失败项（FB-11）。
//
// 「在某企业无身份」（no_identity）是按企业扇出的正常结果：某人只在一家企业有身份时，
// 另一家必然是 no_identity，不能因此判失败。只有这个人在所有企业都无身份（一条也没收到）时，
// 才作为一个失败项返回。其它非 sent 状态都是失败。
func toResponse(result larkpushcap.SendResult) v1.SendResult {
	// reachable 记录在至少一家企业有身份（明细不是 no_identity）的工号。
	reachable := make(map[string]struct{})
	for _, d := range result.Deliveries {
		if d.EmployeeNo != "" && d.Status != larkpushcap.DeliveryStatusNoIdentity {
			reachable[d.EmployeeNo] = struct{}{}
		}
	}
	res := v1.SendResult{}
	noIdentityReported := make(map[string]struct{})
	for _, d := range result.Deliveries {
		if d.Succeeded() {
			continue
		}
		if d.Status == larkpushcap.DeliveryStatusNoIdentity {
			if _, ok := reachable[d.EmployeeNo]; ok {
				continue
			}
			if _, ok := noIdentityReported[d.EmployeeNo]; ok {
				continue
			}
			noIdentityReported[d.EmployeeNo] = struct{}{}
		}
		res.Failed = append(res.Failed, toFailedRes(d))
	}
	res.AllSucceeded = len(res.Failed) == 0 && len(result.Deliveries) > 0
	return res
}

// toFailedRes 把一条失败明细投影为失败项：收件人标识 + 飞书错误三元组。
func toFailedRes(d larkpushcap.Delivery) *v1.FailedRes {
	item := &v1.FailedRes{
		EmployeeNo: d.EmployeeNo,
		ChatID:     d.ChatID,
		LarkCode:   d.FeishuCode,
		LarkMsg:    d.FeishuMsg,
		LarkReqID:  d.FeishuReqID,
	}
	if d.EmployeeNo == "" && d.ChatID == "" {
		// user_id 寻址的收件人只能用 user_id 标识；工号寻址时的 user_id 是解析结果，不回给调用方。
		item.UserID = d.UserID
	}
	if item.LarkMsg == "" {
		// 非飞书侧原因（工号不存在、身份碰撞、网络失败等）没有飞书错误信息，用失败状态说明原因。
		item.LarkMsg = string(d.Status)
	}
	return item
}
