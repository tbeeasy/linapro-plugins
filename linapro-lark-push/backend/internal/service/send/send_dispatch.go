// send_dispatch.go 承载三个投递入口共用的扇出与逐条投递编排，以及请求级日志。
//
// 编排顺序：按推送应用逐个扇出 → 需要寻址时取该企业索引 → 逐收件人定位 → 调飞书 → 汇总明细。
// 某个推送应用的寻址索引取不到时整体返回错误：寻址是全员共用的前置条件，继续发另一半只会让
// 调用方误以为已覆盖。

package send

import (
	"context"
	"errors"
	"strconv"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gctx"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// deliverFunc 用某推送应用的发送器把一条消息投递给一个接收者（人为企业作用域下的 user_id，
// 群聊为 chat_id），返回飞书 message_id。文本、卡片与文件只在这一步不同，寻址与扇出编排共用。
type deliverFunc func(ctx context.Context, sender MessageSender, app larkcfg.PushApp, r larkpushcap.Recipient, receiveIDType, receiveID string) (string, error)

// dispatch 按推送应用逐个扇出。
func (s *serviceImpl) dispatch(
	ctx context.Context,
	group string,
	pushApps []larkcfg.PushApp,
	recipients []larkpushcap.Recipient,
	deliver deliverFunc,
) (larkpushcap.SendResult, error) {
	result := larkpushcap.SendResult{
		Deliveries: make([]larkpushcap.Delivery, 0, len(recipients)*len(pushApps)),
	}
	// 直接给 user_id 的收件人无需查表，但 user_id 按企业签发，仍需按推送应用遍历；
	// 群聊只在其绑定的推送应用那一轮投递。只有存在人类收件人时才需要寻址索引，
	// 纯群聊请求不依赖 empcap。
	for _, app := range pushApps {
		targets, hasPerson := targetsForApp(recipients, app.AppID)
		if len(targets) == 0 {
			continue
		}
		var idx empcap.LarkUserIDIndex
		if hasPerson {
			var err error
			idx, err = s.indexPath(ctx, app.IdentityAppID)
			if err != nil {
				if errors.Is(err, empcap.ErrServiceUnbound) {
					s.notify(ctx, alert.Event{
						Kind:      alert.KindEmpcapUnbound,
						Group:     group,
						PushAppID: app.AppID,
						Detail:    "工号寻址依赖 linapro-employee-core 的 empcap，该能力未绑定",
					})
					return larkpushcap.SendResult{}, bizerr.NewCode(larkpushcode.CodeEmpcapUnbound)
				}
				return larkpushcap.SendResult{}, gerror.Wrapf(err,
					"lark-push: 取身份应用 %s 的 user_id 映射失败", app.IdentityAppID)
			}
			result.BlankUserIDEmployees += len(idx.BlankEmployeeNos)
			result.Collisions += len(idx.CollidedEmployeeNos)
			// 整表为空但空行集非空：该企业一个都发不出去，通常是身份应用缺「获取 user_id」权限。
			if len(idx.ByEmployeeNo) == 0 && len(idx.BlankEmployeeNos) > 0 {
				s.notify(ctx, alert.Event{
					Kind:      alert.KindBlankUserID,
					Group:     group,
					PushAppID: app.AppID,
					Detail: "身份应用 " + app.IdentityAppID + " 下 " +
						strconv.Itoa(len(idx.BlankEmployeeNos)) +
						" 名员工有身份但 lark_user_id 为空，请检查该应用是否具备获取 user_id 的权限",
				})
			}
			if len(idx.CollidedEmployeeNos) > 0 {
				s.notify(ctx, alert.Event{
					Kind:      alert.KindCollision,
					Group:     group,
					PushAppID: app.AppID,
					Detail: "身份应用 " + app.IdentityAppID + " 下 " +
						strconv.Itoa(len(idx.CollidedEmployeeNos)) +
						" 个工号对应多个飞书账号，这些收件人已被拒绝投递",
				})
			}
		}

		sender := s.newClient(app.AppID, app.SecretKey)
		for _, r := range targets {
			result.Deliveries = append(result.Deliveries,
				s.deliverOne(ctx, sender, app, idx, r, deliver))
		}
	}
	return result, nil
}

// targetsForApp 返回某推送应用这一轮要投递的收件人：全部人类收件人，加上绑定到该应用的群聊。
// hasPerson 报告其中是否有人类收件人（决定是否需要查寻址索引）。
func targetsForApp(recipients []larkpushcap.Recipient, appID string) (targets []larkpushcap.Recipient, hasPerson bool) {
	targets = make([]larkpushcap.Recipient, 0, len(recipients))
	for _, r := range recipients {
		if r.Kind() == larkpushcap.RecipientKindChatID {
			if r.PushAppID == appID {
				targets = append(targets, r)
			}
			continue
		}
		hasPerson = true
		targets = append(targets, r)
	}
	return targets, hasPerson
}

// deliverOne 处理一个「收件人 × 推送应用」：
//   - 群聊：以 chat_id 直接投递（调用方已保证该群绑定到本应用）；
//   - user_id 寻址：直接投递；
//   - 工号寻址：按本人工号查该企业的索引——可投递则发送；碰撞判 collision；缺 user_id 判
//     user_id_missing；都不是则此人不在该企业，判 no_identity。只看本人，不受企业里其他人影响；
//   - 命中后经 deliver 调用飞书，失败只记该条明细。
func (s *serviceImpl) deliverOne(
	ctx context.Context,
	sender MessageSender,
	app larkcfg.PushApp,
	idx empcap.LarkUserIDIndex,
	r larkpushcap.Recipient,
	deliver deliverFunc,
) larkpushcap.Delivery {
	d := larkpushcap.Delivery{PushAppID: app.AppID, PushAppName: app.Name, EmployeeNo: r.EmployeeNo, ChatID: r.ChatID}

	if r.Kind() == larkpushcap.RecipientKindChatID {
		return finishDelivery(ctx, d, sender, app, r, larkim.ReceiveIDTypeChatID, r.ChatID, deliver)
	}

	userID := r.UserID // 直接给 user_id：调用方已确认标识属于本企业空间，跳过查表。
	if r.Kind() == larkpushcap.RecipientKindEmployeeNo {
		if _, collided := idx.CollidedEmployeeNos[r.EmployeeNo]; collided {
			d.Status = larkpushcap.DeliveryStatusCollision
			d.Error = "该工号在此企业对应多个飞书账号，拒绝发送以避免送错人"
			return d
		}
		if _, blank := idx.BlankEmployeeNos[r.EmployeeNo]; blank {
			d.Status = larkpushcap.DeliveryStatusUserIDMissing
			d.Error = "此人在该企业有身份但 lark_user_id 为空（身份应用可能缺少获取 user_id 的权限，或待下次同步回填）"
			return d
		}
		hit, ok := idx.ByEmployeeNo[r.EmployeeNo]
		if !ok {
			d.Status = larkpushcap.DeliveryStatusNoIdentity
			d.Error = "此人在该推送应用对应的企业没有身份"
			return d
		}
		userID = hit
	}

	d.UserID = userID
	return finishDelivery(ctx, d, sender, app, r, larkim.ReceiveIDTypeUserID, userID, deliver)
}

// finishDelivery 调用 deliver 完成飞书投递，并把结果（message_id 或飞书错误）写入明细。
func finishDelivery(
	ctx context.Context,
	d larkpushcap.Delivery,
	sender MessageSender,
	app larkcfg.PushApp,
	r larkpushcap.Recipient,
	receiveIDType, receiveID string,
	deliver deliverFunc,
) larkpushcap.Delivery {
	messageID, err := deliver(ctx, sender, app, r, receiveIDType, receiveID)
	if err != nil {
		d.Status = larkpushcap.DeliveryStatusFailed
		d.Error = err.Error()
		var fe *larkim.FeishuError
		if errors.As(err, &fe) {
			d.FeishuCode = fe.Code
			d.FeishuMsg = fe.Msg
			d.FeishuReqID = fe.ReqID
		}
		return d
	}
	d.Status = larkpushcap.DeliveryStatusSent
	d.MessageID = messageID
	return d
}

// logRequest 输出一条结构化请求日志（宿主审计对无宿主身份的调用方不落记录）。
//
// 只记可对账的标识：调用方、业务单号、推送应用、工号、状态与飞书错误码/请求 ID。
// 不记正文（消息体可能含敏感信息），也不记任何密钥。
func (s *serviceImpl) logRequest(
	ctx context.Context,
	group, bizSource, bizRef string,
	recipientCount int,
	pushApps []larkcfg.PushApp,
	result larkpushcap.SendResult,
) {
	names := make([]string, 0, len(pushApps))
	for _, app := range pushApps {
		names = append(names, app.Name+"("+app.AppID+")")
	}
	for _, d := range result.Deliveries {
		if d.Succeeded() {
			continue
		}
		logger.Warningf(ctx,
			"lark-push: 投递失败 request_id=%s group=%s biz_source=%s biz_ref=%s push_app=%s(%s) 工号=%s user_id=%s chat_id=%s 状态=%s feishu_code=%d feishu_req_id=%s err=%s",
			gctx.CtxId(ctx), group, bizSource, bizRef, d.PushAppName, d.PushAppID, d.EmployeeNo, d.UserID, d.ChatID,
			d.Status, d.FeishuCode, d.FeishuReqID, d.Error)
	}
	logger.Infof(ctx,
		"lark-push: 投递完成 group=%s biz_source=%s biz_ref=%s 推送应用=%v 收件人=%d 明细=%d 成功=%d blank_user_id_employees=%d collisions=%d",
		group, bizSource, bizRef, names, recipientCount, len(result.Deliveries),
		countSucceeded(result.Deliveries), result.BlankUserIDEmployees, result.Collisions)
}

// countSucceeded 统计成功条数。
func countSucceeded(ds []larkpushcap.Delivery) int {
	n := 0
	for _, d := range ds {
		if d.Succeeded() {
			n++
		}
	}
	return n
}
