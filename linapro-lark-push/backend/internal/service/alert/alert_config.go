// alert_config.go 读取并校验告警通道配置（plugin.linapro-lark-push.alert）。
//
// 配置整体缺失或任一必填项为空时，通道视为未配置：静默不告警，但推送能力照常工作。这不是错误
// ——告警是可选的运维增强，不能因为它没配就让业务推送起不来。

package alert

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"

	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// ConfigKey 是本插件的告警配置键（插件私有，与 push_groups 平级但互不依赖）。
const ConfigKey = "plugin.linapro-lark-push.alert"

// DefaultInterval 是防抖窗口的默认值：同一告警 30 分钟内只发一条。
const DefaultInterval = 30 * time.Minute

// ErrClientFactoryMissing 表示构造告警通道时未注入发送器工厂。
var ErrClientFactoryMissing = gerror.New("lina-plugin-linapro-lark-push: 构造告警通道失败：发送器工厂未注入")

// config 是告警通道的配置。
type config struct {
	// AppID 是发送告警的飞书应用 App ID。
	AppID string
	// SecretKey 是该应用的 App Secret。
	SecretKey string
	// ChatID 是告警群 chat_id（oc_ 开头）。
	ChatID string
	// Interval 是同一告警的防抖窗口，默认 DefaultInterval。
	Interval time.Duration
}

// configWire 是配置文件的原始形状：时间长度在配置里是带单位的字符串（如 "30m"），
// 由此结构接收后再解析为 time.Duration（见 .agents/rules/backend-go.md 语言和类型要求）。
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

// loadConfig 读取告警配置。宿主配置能力缺失返回「未配置」且不报错。
func loadConfig(ctx context.Context, hostConfig hostconfigcap.Service) (config, error) {
	if hostConfig == nil {
		return config{Interval: DefaultInterval}, nil
	}
	v, err := hostConfig.Get(ctx, ConfigKey, nil)
	if err != nil {
		return config{}, gerror.Wrapf(err, "alert: 读取 %s 配置失败", ConfigKey)
	}
	if v == nil || v.IsNil() {
		return config{Interval: DefaultInterval}, nil
	}
	return decodeConfig(v)
}

// decodeConfig 把配置值解析为 config。格式非法或 interval 无法解析时返回错误，
// 由调用方记日志后静默降级（不告警，但不影响推送）。
func decodeConfig(v *gvar.Var) (config, error) {
	var wire configWire
	if err := gconv.Struct(v.Val(), &wire); err != nil {
		return config{}, gerror.Wrapf(err, "alert: 解析 %s 配置失败", ConfigKey)
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
		return config{}, gerror.Wrapf(err, "alert: %s.interval 须为带单位的时长（如 30m、1h）", ConfigKey)
	}
	if interval > 0 {
		cfg.Interval = interval
	}
	return cfg, nil
}
