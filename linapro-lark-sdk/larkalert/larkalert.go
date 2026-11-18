// Package larkalert 提供跨插件复用的运维告警能力：读消费方自己的配置键 → 防抖 → 拼正文 →
// 经 larkim 发送失败只记日志。
//
// 它刻意与业务推送通道解耦：最需要告警的时刻，恰恰是业务通道配置损坏或装配失败的时刻。
// 因此消费方各自登记一份告警应用凭据（见宿主配置 `_anchors.ops_alert`），库内不写死任何
// 插件的配置命名空间，也不与 `linapro-lark-push` 建立依赖。
//
// 边界约束：
//   - 只做出站文本发送，不接收事件、不建订阅；
//   - 失败绝不回传、绝不递归告警——一次飞书故障不应演变成消息风暴；
//   - 日志统一走 lina-core/pkg/logger。
package larkalert

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"

	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/capability/cachecap"
	"lina-core/pkg/plugin/capability/hostconfigcap"

	"linapro-lark-sdk/larkim"
)

// DefaultInterval 是防抖窗口的默认值：同一告警 30 分钟内只发一条。
const DefaultInterval = 30 * time.Minute

// throttleNamespace 是防抖计数在消费方插件缓存里的命名空间。
const throttleNamespace = "larkalert-throttle"

// TextSender 向某个接收者发送一条文本。由 larkim.Client 满足。
type TextSender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
}

// ClientFactory 按告警应用凭据构造发送器。由消费方传入，库内不自行决定用哪个客户端。
type ClientFactory func(appID, appSecret string) TextSender

// Env 是一次告警投递所需的运行时依赖。
//
// 三个字段都允许为 nil，语义一致：对应的能力不可用时告警退化为「不发送」或「不防抖」，
// 绝不 panic、绝不影响调用方的业务结果。
type Env struct {
	// Config 是宿主配置能力，读消费方自己的配置键；nil 时通道视为未配置。
	Config hostconfigcap.Service
	// Cache 是宿主缓存能力，用于防抖计数；nil 时退化为每次都发（宁可多发不可漏发）。
	Cache cachecap.Service
	// NewClient 按告警应用凭据构造发送器；nil 时退化为不发送。
	NewClient ClientFactory
}

// Event 是一条待发送的告警。
type Event struct {
	// ConfigKey 是消费方自己的配置键，例如 plugin.linapro-monitor-apilog.alert。
	ConfigKey string
	// Kind 是稳定的事件种类，同时作为防抖键与正文「类型」行，取值必须稳定。
	Kind string
	// Detail 是补充说明，直接进正文；不要放密钥或业务正文。
	Detail string
}

// Notify 在需要时向告警群推送一条事件。
//
// 无返回值：发送失败在内部记 ERROR，绝不回传、绝不递归告警。配置不全时静默丢弃——
// 告警是可选的运维增强，未配置属预期状态，不刷日志。
func Notify(ctx context.Context, env Env, ev Event) {
	cfg, err := loadConfig(ctx, env.Config, ev.ConfigKey)
	if err != nil {
		// 配置读不出来就无法告警，只留痕。
		logger.Errorf(ctx, "larkalert: 读取告警配置失败，本条告警丢弃 config_key=%s kind=%s: %v", ev.ConfigKey, ev.Kind, err)
		return
	}
	if !cfg.ready() {
		return
	}
	if env.NewClient == nil {
		return
	}
	if !allow(ctx, env.Cache, ev, cfg.Interval) {
		return
	}

	sender := env.NewClient(cfg.AppID, cfg.SecretKey)
	messageID, sendErr := sender.SendText(ctx, larkim.ReceiveIDTypeChatID, cfg.ChatID, buildText(ev))
	if sendErr != nil {
		// 告警发送失败不再触发告警，只留痕，避免自激。
		logger.Errorf(ctx, "larkalert: 告警发送失败 config_key=%s kind=%s chat_id=%s: %v",
			ev.ConfigKey, ev.Kind, cfg.ChatID, sendErr)
		return
	}
	logger.Noticef(ctx, "larkalert: 已发送告警 config_key=%s kind=%s message_id=%s",
		ev.ConfigKey, ev.Kind, messageID)
}

// config 是告警通道配置。
type config struct {
	// AppID 是发送告警的飞书应用 App ID。
	AppID string
	// SecretKey 是该应用的 App Secret。
	SecretKey string
	// ChatID 是告警群 chat_id（oc_ 开头）。
	ChatID string
	// Interval 是同一告警的防抖窗口。
	Interval time.Duration
}

// configWire 是配置文件的原始形状：时长在配置里是带单位的字符串（如 "30m"）。
type configWire struct {
	AppID    string `json:"app_id"`
	Secret   string `json:"secret_key"`
	ChatID   string `json:"chat_id"`
	Interval string `json:"interval"`
}

// ready 报告配置是否齐备：三项标识缺一不可（interval 有默认值，不参与判定）。
func (c config) ready() bool {
	return c.AppID != "" && c.SecretKey != "" && c.ChatID != ""
}

// loadConfig 读取告警配置。宿主配置能力缺失或键缺失时返回「未配置」且不报错。
func loadConfig(ctx context.Context, reader hostconfigcap.Service, configKey string) (config, error) {
	if reader == nil || strings.TrimSpace(configKey) == "" {
		return config{Interval: DefaultInterval}, nil
	}
	v, err := reader.Get(ctx, configKey, nil)
	if err != nil {
		return config{}, gerror.Wrapf(err, "larkalert: 读取 %s 配置失败", configKey)
	}
	if v == nil || v.IsNil() {
		return config{Interval: DefaultInterval}, nil
	}
	return decodeConfig(v, configKey)
}

// decodeConfig 把配置值解析为 config。格式非法或 interval 无法解析时返回错误，
// 由 Notify 记日志后静默降级。
func decodeConfig(v *gvar.Var, configKey string) (config, error) {
	var wire configWire
	if err := gconv.Struct(v.Val(), &wire); err != nil {
		return config{}, gerror.Wrapf(err, "larkalert: 解析 %s 配置失败", configKey)
	}
	cfg := config{
		AppID:     strings.TrimSpace(wire.AppID),
		SecretKey: strings.TrimSpace(wire.Secret),
		ChatID:    strings.TrimSpace(wire.ChatID),
		Interval:  DefaultInterval,
	}
	raw := strings.TrimSpace(wire.Interval)
	if raw == "" {
		return cfg, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return config{}, gerror.Wrapf(err, "larkalert: %s.interval 须为带单位的时长（如 30m、1h）", configKey)
	}
	if interval > 0 {
		cfg.Interval = interval
	}
	return cfg, nil
}

// allow 判定本条告警是否应当发送：同一「配置键 + 种类」在窗口内只发一条。
//
// 缓存不可用或防抖失败时放行（fail-open）：多发一条的代价远小于漏发的代价。
// 计数用 Incr 而非「Get 判空 + Set」，否则并发下多个调用会同时判定为首次而齐发。
func allow(ctx context.Context, cache cachecap.Service, ev Event, interval time.Duration) bool {
	if cache == nil {
		return true
	}
	item, err := cache.Incr(ctx, throttleNamespace, throttleKey(ev), 1, interval)
	if err != nil {
		logger.Warningf(ctx, "larkalert: 防抖计数失败，本条告警照常发送 config_key=%s kind=%s: %v",
			ev.ConfigKey, ev.Kind, err)
		return true
	}
	return item != nil && item.IntValue == 1
}

// throttleKey 拼装防抖键。配置键与种类都来自配置/常量，长度远小于缓存键上限（256 字节）。
func throttleKey(ev Event) string {
	return ev.ConfigKey + ":" + ev.Kind
}

// buildText 拼装告警正文。只放可对账的标识，不放密钥、不放大段业务内容。
func buildText(ev Event) string {
	var b strings.Builder
	b.WriteString("【LinaPro 运维告警】\n")
	b.WriteString("类型：" + ev.Kind + "\n")
	if ev.ConfigKey != "" {
		b.WriteString("配置键：" + ev.ConfigKey + "\n")
	}
	if ev.Detail != "" {
		b.WriteString("详情：" + ev.Detail + "\n")
	}
	b.WriteString("说明：同类告警在防抖窗口内只推送一次。")
	return b.String()
}
