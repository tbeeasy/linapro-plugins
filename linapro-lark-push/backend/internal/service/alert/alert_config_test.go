// alert_config_test.go 覆盖告警配置的解析与降级行为。

package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"
)

// TestLoadConfig_Ready 验证齐备配置被解析，且 interval 按带单位的字符串解析为时长。
func TestLoadConfig_Ready(t *testing.T) {
	cfg, err := loadConfig(context.Background(), &fakeHostConfig{values: readyConfig()})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.ready() {
		t.Fatalf("齐备配置应判为可用: %+v", cfg)
	}
	if cfg.Interval != 10*time.Minute {
		t.Fatalf("interval 应解析为 10m, got %v", cfg.Interval)
	}
}

// TestLoadConfig_Missing 验证配置整体缺失时不报错，且判定为未配置（不告警但推送照常）。
func TestLoadConfig_Missing(t *testing.T) {
	cfg, err := loadConfig(context.Background(), &fakeHostConfig{values: map[string]any{}})
	if err != nil {
		t.Fatalf("配置缺失不应报错: %v", err)
	}
	if cfg.ready() {
		t.Fatal("配置缺失时应判为未配置")
	}
	if cfg.Interval != DefaultInterval {
		t.Fatalf("缺失时应回落到默认间隔, got %v", cfg.Interval)
	}
}

// TestLoadConfig_NilHostConfig 验证宿主配置能力缺失时按未配置处理，不 panic。
func TestLoadConfig_NilHostConfig(t *testing.T) {
	cfg, err := loadConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("宿主配置缺失不应报错: %v", err)
	}
	if cfg.ready() {
		t.Fatal("宿主配置缺失时应判为未配置")
	}
}

// TestLoadConfig_PartialFields 验证缺任一必填项即判为未配置：三项标识缺一不可。
func TestLoadConfig_PartialFields(t *testing.T) {
	cases := map[string]map[string]any{
		"缺 app_id":     {"secret_key": "s", "chat_id": "oc_a"},
		"缺 secret_key": {"app_id": "cli_a", "chat_id": "oc_a"},
		"缺 chat_id":    {"app_id": "cli_a", "secret_key": "s"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := loadConfig(context.Background(),
				&fakeHostConfig{values: map[string]any{ConfigKey: body}})
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.ready() {
				t.Fatalf("%s 时应判为未配置: %+v", name, cfg)
			}
		})
	}
}

// TestLoadConfig_InvalidInterval 验证 interval 不是合法时长时返回错误（由调用方记日志降级）。
func TestLoadConfig_InvalidInterval(t *testing.T) {
	cfg := map[string]any{ConfigKey: map[string]any{
		"app_id": "cli_a", "secret_key": "s", "chat_id": "oc_a", "interval": "30",
	}}
	if _, err := loadConfig(context.Background(), &fakeHostConfig{values: cfg}); err == nil {
		t.Fatal("interval 缺单位时应返回错误")
	}
}

// TestLoadConfig_ReadError 验证配置读取失败时返回错误，而不是静默当作未配置。
func TestLoadConfig_ReadError(t *testing.T) {
	_, err := loadConfig(context.Background(), &fakeHostConfig{err: errors.New("config down")})
	if err == nil {
		t.Fatal("配置读取失败应返回错误")
	}
}

// TestDecodeConfig_DefaultsInterval 验证 interval 留空时取默认值，不是零值（零值会让防抖失效）。
func TestDecodeConfig_DefaultsInterval(t *testing.T) {
	v := gvar.New(map[string]any{
		"app_id": "cli_a", "secret_key": "s", "chat_id": "oc_a",
	})
	cfg, err := decodeConfig(v)
	if err != nil {
		t.Fatalf("decodeConfig: %v", err)
	}
	if cfg.Interval != DefaultInterval {
		t.Fatalf("interval 留空应取默认值, got %v", cfg.Interval)
	}
}
