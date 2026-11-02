// send_text.go 实现文本投递与文本参数校验。
//
// 文本投递与卡片、文件共用同一条寻址与扇出编排（dispatch / deliverOne），区别只在最后一步：
// 把已校验的正文发给收件人。正文只校验不截断——超长或为空都返回参数错误，让调用方自己改。

package send

import (
	"context"
	"strings"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
)

// SendText 执行一次投递。
//
// 返回 error 仅表示「无法完成本次请求」（参数非法 / 配置非法 / 无可用推送应用 /
// 能力未就绪）；收件人级别的失败体现在 Deliveries 里，不中断同批其余收件人。
func (s *serviceImpl) SendText(ctx context.Context, req larkpushcap.SendTextRequest) (larkpushcap.SendResult, error) {
	group, err := normalizeGroup(req.Group)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	text, err := normalizeText(req.Text)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	recipients, err := normalizeRecipients(req.Recipients)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	pushApps, err := s.requirePushApps(ctx, group)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	if recipients, err = bindChatPushApps(recipients, pushApps); err != nil {
		return larkpushcap.SendResult{}, err
	}

	deliver := func(ctx context.Context, sender MessageSender, _ larkcfg.PushApp, _ larkpushcap.Recipient, receiveIDType, receiveID string) (string, error) {
		return sender.SendText(ctx, receiveIDType, receiveID, text)
	}
	result, err := s.dispatch(ctx, group, pushApps, recipients, deliver)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	s.logRequest(ctx, group, req.BizSource, req.BizRef, len(req.Recipients), pushApps, result)
	return result, nil
}

// normalizeText 校验文本并在超长时直接报参数错误（不截断）。
func normalizeText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", invalidParam("text 不能为空")
	}
	if _, err := larkim.BuildTextContent(text); err != nil {
		return "", invalidParam(err.Error())
	}
	return text, nil
}
