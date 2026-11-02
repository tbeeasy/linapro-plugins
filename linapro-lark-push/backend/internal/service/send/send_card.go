// send_card.go 实现交互卡片投递与推送应用清单查询。
//
// 卡片投递与文本投递共用同一条寻址与扇出编排（dispatch / deliverOne），区别只在
// 最后一步：卡片内容由调用方的 CardBuilder 按「推送应用 × 收件人」构造，再经推送应用
// 凭据以 user_id（群聊为 chat_id）发出。构造失败只记该条明细为 failed，不中断同批其余投递。
// DryRun 请求走同一条编排、只在调用飞书前停下，保证「干跑可达」与「正式投递会发出」口径一致。

package send

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
)

// SendCard 执行一次交互卡片投递，错误语义同 SendText：返回 error 仅表示「无法完成本次请求」，
// 收件人级别的失败（含卡片构造失败）体现在 Deliveries 里。
func (s *serviceImpl) SendCard(ctx context.Context, req larkpushcap.SendCardRequest) (larkpushcap.SendResult, error) {
	group, err := normalizeGroup(req.Group)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	if req.Build == nil {
		return larkpushcap.SendResult{}, invalidParam("卡片构造函数不能为空")
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

	deliver := func(ctx context.Context, sender MessageSender, app larkcfg.PushApp, r larkpushcap.Recipient, receiveIDType, receiveID string) (string, error) {
		cardJSON, bErr := req.Build(toPublicPushApp(app), r)
		if bErr != nil {
			return "", gerror.Wrap(bErr, "lark-push: 构造卡片失败")
		}
		if req.DryRun {
			// 干跑：寻址与构造都已通过，不调用飞书。
			return "", nil
		}
		return sender.SendInteractiveCard(ctx, receiveIDType, receiveID, cardJSON)
	}
	result, err := s.dispatch(ctx, group, pushApps, recipients, deliver)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	if req.DryRun {
		// 干跑没有真实投递：把「本会发出」的明细改记为可投递，不写投递日志（无飞书调用可对账）。
		markAddressable(result.Deliveries)
		return result, nil
	}
	s.logRequest(ctx, group, req.BizSource, req.BizRef, len(req.Recipients), pushApps, result)
	return result, nil
}

// markAddressable 把干跑中 deliverOne 记为 sent 的明细改为 addressable。
func markAddressable(ds []larkpushcap.Delivery) {
	for i := range ds {
		if ds[i].Status == larkpushcap.DeliveryStatusSent {
			ds[i].Status = larkpushcap.DeliveryStatusAddressable
		}
	}
}

// ListPushApps 返回 group 分组内参与投递的推送应用公开投影（不含密钥）。分组为空返回
// 参数错误；该分组配置非法时返回错误；分组未配置或全部凭据不全时返回空切片。
func (s *serviceImpl) ListPushApps(ctx context.Context, group string) ([]larkpushcap.PushApp, error) {
	group, err := normalizeGroup(group)
	if err != nil {
		return nil, err
	}
	pushApps, err := s.loadPushApps(ctx, group)
	if err != nil {
		return nil, err
	}
	out := make([]larkpushcap.PushApp, 0, len(pushApps))
	for _, app := range pushApps {
		out = append(out, toPublicPushApp(app))
	}
	return out, nil
}

// toPublicPushApp 把内部配置投影为门面类型，剥离密钥。
func toPublicPushApp(app larkcfg.PushApp) larkpushcap.PushApp {
	return larkpushcap.PushApp{AppID: app.AppID, Name: app.Name, IdentityAppID: app.IdentityAppID}
}
