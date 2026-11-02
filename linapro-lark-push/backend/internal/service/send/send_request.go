// send_request.go 承载文本 / 卡片 / 文件共用的发送前置准备：请求参数校验、群聊发送应用绑定，
// 以及请求分组的推送应用清单读取。
//
// 三个投递入口都在发起任何飞书请求之前走完这里：参数非法一律整体返回错误，不先发出半批消息；
// 配置只校验本次请求涉及的分组，一个坏分组不会拖垮全部调用方。

package send

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// normalizeGroup 校验推送分组：去首尾空白后不得为空。
func normalizeGroup(group string) (string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return "", invalidParam("group 不能为空（进程内调用方须填自己的插件 ID）")
	}
	return group, nil
}

// normalizeRecipients 去首尾空白、去重（同工号 / 同 user_id / 同群聊只发一次）并校验寻址方式。
func normalizeRecipients(in []larkpushcap.Recipient) ([]larkpushcap.Recipient, error) {
	if len(in) == 0 {
		return nil, invalidParam("收件人不能为空")
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]larkpushcap.Recipient, 0, len(in))
	for _, r := range in {
		r = larkpushcap.Recipient{
			EmployeeNo: strings.TrimSpace(r.EmployeeNo),
			UserID:     strings.TrimSpace(r.UserID),
			ChatID:     strings.TrimSpace(r.ChatID),
			PushAppID:  strings.TrimSpace(r.PushAppID),
		}
		kind := r.Kind()
		if kind == "" {
			return nil, invalidParam("每个收件人须且只能给出 employee_no / user_id（人）或 chat_id（群聊）之一")
		}
		if kind != larkpushcap.RecipientKindChatID && r.PushAppID != "" {
			return nil, invalidParam("push_app_id 只能用于群聊收件人")
		}
		key := string(kind) + "\x00" + r.EmployeeNo + "\x00" + r.UserID + "\x00" + r.ChatID + "\x00" + r.PushAppID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out, nil
}

// bindChatPushApps 为群聊收件人确定发送应用，并在发起任何飞书请求前校验：
//   - 分组只有一个推送应用时，未指定 push_app_id 的群聊默认用它；
//   - 分组有多个推送应用时，群聊必须指定 push_app_id；
//   - push_app_id 必须属于本分组（只认凭据齐备的推送应用）。
//
// 任一群聊不合法即整体返回参数错误，避免先发出半批消息。返回的切片是副本，不修改入参。
func bindChatPushApps(recipients []larkpushcap.Recipient, pushApps []larkcfg.PushApp) ([]larkpushcap.Recipient, error) {
	inGroup := make(map[string]struct{}, len(pushApps))
	for _, app := range pushApps {
		inGroup[app.AppID] = struct{}{}
	}
	out := make([]larkpushcap.Recipient, len(recipients))
	copy(out, recipients)
	for i, r := range out {
		if r.Kind() != larkpushcap.RecipientKindChatID {
			continue
		}
		if r.PushAppID == "" {
			if len(pushApps) != 1 {
				return nil, invalidParam("分组内有多个推送应用，群聊 " + r.ChatID + " 必须指定 push_app_id")
			}
			out[i].PushAppID = pushApps[0].AppID
			continue
		}
		if _, ok := inGroup[r.PushAppID]; !ok {
			return nil, invalidParam("群聊 " + r.ChatID + " 指定的 push_app_id " + r.PushAppID + " 不属于本分组")
		}
	}
	return out, nil
}

// invalidParam 构造参数错误。
func invalidParam(reason string) error {
	return bizerr.NewCode(larkpushcode.CodeInvalidParam, bizerr.P("item", reason))
}

// loadPushApps 读取并校验 group 分组的推送应用清单，返回凭据齐备的条目。
// 只校验该分组：其它分组非法不影响本次请求；分组未配置时返回空切片。
func (s *serviceImpl) loadPushApps(ctx context.Context, group string) ([]larkcfg.PushApp, error) {
	if s.hostConfig == nil {
		// 配置能力缺失时告警配置同样读不出来，这里不尝试告警（自指矛盾），
		// 由外部探针轮询 /open/v1/status 的 503 发现。
		return nil, bizerr.NewCode(larkpushcode.CodeHostConfigMissing)
	}
	groups, err := larkcfg.Load(ctx, s.hostConfig)
	if err != nil {
		return nil, gerror.Wrap(err, "lark-push: 读取推送应用配置失败")
	}
	apps := groups[group]
	if issues := larkcfg.Validate(group, apps); len(issues) > 0 {
		s.notify(ctx, alert.Event{
			Kind:   alert.KindConfigInvalid,
			Group:  group,
			Detail: issues[0].String(),
		})
		return nil, bizerr.NewCode(larkpushcode.CodeConfigInvalid,
			bizerr.P("item", issues[0].String()))
	}
	return larkcfg.Pushable(apps), nil
}

// requirePushApps 在 loadPushApps 基础上要求分组至少有一个可用推送应用，
// 否则返回 CodeNoPushApp（带分组名），供发送路径使用。
func (s *serviceImpl) requirePushApps(ctx context.Context, group string) ([]larkcfg.PushApp, error) {
	pushApps, err := s.loadPushApps(ctx, group)
	if err != nil {
		return nil, err
	}
	if len(pushApps) == 0 {
		// 忘配分组或全部条目凭据不全：整批发不出去，调用方只看得到错误码，需要主动告警。
		s.notify(ctx, alert.Event{
			Kind:   alert.KindNoPushApp,
			Group:  group,
			Detail: "push_groups." + group + " 未配置，或该分组的条目都缺少 app_id / secret_key",
		})
		return nil, bizerr.NewCode(larkpushcode.CodeNoPushApp, bizerr.P("group", group))
	}
	return pushApps, nil
}
