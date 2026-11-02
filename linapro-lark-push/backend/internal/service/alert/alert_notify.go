// alert_notify.go 实现告警的防抖、正文拼装与投递。
//
// 投递刻意直接走飞书、不经过 send 组件：业务推送通道正是它要监控的对象，复用会让告警在
// 最需要它的时候一起失效。发送失败只记 ERROR，绝不递归告警——否则一次飞书故障会演变成
// 无限自激的消息风暴。

package alert

import (
	"context"
	"strings"
	"time"

	"lina-core/pkg/logger"

	"linapro-lark-sdk/larkim"
)

// throttleNamespace 是防抖计数在插件缓存里的命名空间，与签名 nonce 去重的命名空间区分开。
const throttleNamespace = "alert-throttle"

// Notify 在需要时向告警群推送一条事件。
func (s *serviceImpl) Notify(ctx context.Context, ev Event) {
	cfg, err := loadConfig(ctx, s.hostConfig)
	if err != nil {
		// 配置读不出来就无法告警，只记日志：这正是外部探针要覆盖的场景。
		logger.Errorf(ctx, "lark-push alert: 读取告警配置失败，本条告警丢弃: %v", err)
		return
	}
	if !cfg.ready() {
		// 未配置告警通道属预期状态，不刷日志——启动时已由装配层提示过一次。
		return
	}
	if !s.allow(ctx, ev, cfg.Interval) {
		return
	}

	sender := s.newClient(cfg.AppID, cfg.SecretKey)
	messageID, err := sender.SendText(ctx, larkim.ReceiveIDTypeChatID, cfg.ChatID, buildText(ev))
	if err != nil {
		// 告警发送失败不再触发告警，只留痕。飞书侧错误已由 larkim 包装成可读文案。
		logger.Errorf(ctx, "lark-push alert: 告警发送失败 kind=%s group=%s push_app=%s chat_id=%s: %v",
			ev.Kind, ev.Group, ev.PushAppID, cfg.ChatID, err)
		return
	}
	logger.Noticef(ctx, "lark-push alert: 已发送告警 kind=%s group=%s push_app=%s message_id=%s",
		ev.Kind, ev.Group, ev.PushAppID, messageID)
}

// Enabled 报告告警通道是否已配置齐备。
func (s *serviceImpl) Enabled(ctx context.Context) bool {
	cfg, err := loadConfig(ctx, s.hostConfig)
	if err != nil {
		return false
	}
	return cfg.ready()
}

// allow 判定本条告警是否应当发送：同一「种类 + 分组 + 推送应用」在窗口内只发一条。
//
// 缓存不可用或防抖失败时放行（fail-open）：告警的代价是多发一条，漏发的代价是故障无人知晓。
// 计数用 Incr 而非「Get 判空 + Set」，否则并发下多个请求会同时判定为首次而齐发。
func (s *serviceImpl) allow(ctx context.Context, ev Event, interval time.Duration) bool {
	if s.cache == nil {
		return true
	}
	item, err := s.cache.Incr(ctx, throttleNamespace, throttleKey(ev), 1, interval)
	if err != nil {
		logger.Warningf(ctx, "lark-push alert: 防抖计数失败，本条告警照常发送 kind=%s group=%s: %v",
			ev.Kind, ev.Group, err)
		return true
	}
	return item != nil && item.IntValue == 1
}

// throttleKey 拼装防抖键。分组名与 app_id 都来自配置，长度远小于缓存键上限（256 字节）。
func throttleKey(ev Event) string {
	return string(ev.Kind) + ":" + ev.Group + ":" + ev.PushAppID
}

// buildText 拼装告警正文。只放可对账的标识，不放密钥、不放大段消息内容。
func buildText(ev Event) string {
	var b strings.Builder
	b.WriteString("【LinaPro 飞书推送告警】\n")
	b.WriteString("类型：" + ev.Kind.Label() + "（" + string(ev.Kind) + "）\n")
	if ev.Group != "" {
		b.WriteString("分组：" + ev.Group + "\n")
	}
	if ev.PushAppID != "" {
		b.WriteString("推送应用：" + ev.PushAppID + "\n")
	}
	if ev.Detail != "" {
		b.WriteString("详情：" + ev.Detail + "\n")
	}
	b.WriteString("说明：同类告警在防抖窗口内只推送一次；插件未启用或进程异常时本通道无法自报，请以 /open/v1/status 返回 503 的外部探针告警为准。")
	return b.String()
}
