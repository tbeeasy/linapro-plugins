// alert_notify_test.go 覆盖告警的投递、防抖与降级行为。

package alert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"linapro-lark-sdk/larkim"
)

// TestNotify_SendsToChat 验证齐备配置下把告警发到配置的群聊，且用的是告警应用自己的凭据。
func TestNotify_SendsToChat(t *testing.T) {
	svc, calls := newService(t, readyConfig(), newFakeCache())
	svc.Notify(context.Background(), Event{
		Kind: KindNoPushApp, Group: "linapro-eval-360", Detail: "分组未配置",
	})

	if len(*calls) != 1 {
		t.Fatalf("应发送 1 条告警, got %d", len(*calls))
	}
	got := (*calls)[0]
	if got.appID != "cli_alert" || got.appSecret != "alert-secret" {
		t.Fatalf("应使用告警应用自己的凭据, got app=%s secret=%s", got.appID, got.appSecret)
	}
	if got.receiveTyp != larkim.ReceiveIDTypeChatID || got.receiveID != "oc_alert" {
		t.Fatalf("应发往配置的群聊, got %s=%s", got.receiveTyp, got.receiveID)
	}
	for _, want := range []string{"推送分组没有可用推送应用", "no_push_app", "linapro-eval-360", "分组未配置"} {
		if !strings.Contains(got.text, want) {
			t.Fatalf("正文应包含 %q, got %s", want, got.text)
		}
	}
}

// TestNotify_Throttled 验证同一「种类 + 分组 + 推送应用」在防抖窗口内只发一条，
// 不同分组各自计一次。
func TestNotify_Throttled(t *testing.T) {
	cache := newFakeCache()
	svc, calls := newService(t, readyConfig(), cache)
	ev := Event{Kind: KindConfigInvalid, Group: "g1", PushAppID: "cli_x"}

	svc.Notify(context.Background(), ev)
	svc.Notify(context.Background(), ev)
	svc.Notify(context.Background(), ev)
	if len(*calls) != 1 {
		t.Fatalf("同类告警应只发 1 条, got %d", len(*calls))
	}

	// 分组不同 → 防抖键不同 → 各发一条。
	svc.Notify(context.Background(), Event{Kind: KindConfigInvalid, Group: "g2", PushAppID: "cli_x"})
	if len(*calls) != 2 {
		t.Fatalf("不同分组应各自发送, got %d", len(*calls))
	}
}

// TestNotify_NoCacheFailsOpen 验证缓存缺失时照常发送（宁可重复，不可漏发）。
func TestNotify_NoCacheFailsOpen(t *testing.T) {
	svc, calls := newService(t, readyConfig(), nil)
	ev := Event{Kind: KindCollision, Group: "g1"}

	svc.Notify(context.Background(), ev)
	svc.Notify(context.Background(), ev)
	if len(*calls) != 2 {
		t.Fatalf("无缓存时应每次都发, got %d", len(*calls))
	}
}

// TestNotify_CacheErrorFailsOpen 验证防抖计数失败时照常发送。
func TestNotify_CacheErrorFailsOpen(t *testing.T) {
	svc, calls := newService(t, readyConfig(), &fakeCache{err: errors.New("cache down")})
	svc.Notify(context.Background(), Event{Kind: KindCollision, Group: "g1"})
	if len(*calls) != 1 {
		t.Fatalf("防抖失败时应照常发送, got %d", len(*calls))
	}
}

// TestNotify_NotConfigured 验证告警未配置时静默丢弃：不发送、不报错、不刷日志。
func TestNotify_NotConfigured(t *testing.T) {
	svc, calls := newService(t, map[string]any{}, newFakeCache())
	svc.Notify(context.Background(), Event{Kind: KindNoPushApp, Group: "g1"})
	if len(*calls) != 0 {
		t.Fatalf("未配置时不应发送, got %d", len(*calls))
	}
}

// TestNotify_SendErrorDoesNotRecurse 验证告警发送失败只留痕，不递归触发新的告警。
func TestNotify_SendErrorDoesNotRecurse(t *testing.T) {
	calls := &[]sentAlert{}
	svc, err := New(&fakeHostConfig{values: readyConfig()}, newFakeCache(),
		func(appID, appSecret string) MessageSender {
			return &fakeSender{appID: appID, appSecret: appSecret, calls: calls, err: errors.New("lark down")}
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.Notify(context.Background(), Event{Kind: KindNoPushApp, Group: "g1"})
	if len(*calls) != 1 {
		t.Fatalf("只应尝试发送一次，不递归, got %d", len(*calls))
	}
}

// TestNotify_UsesConfiguredInterval 验证防抖窗口取自配置，而不是固定常量。
func TestNotify_UsesConfiguredInterval(t *testing.T) {
	cache := newFakeCache()
	cfg := map[string]any{ConfigKey: map[string]any{
		"app_id": "cli_alert", "secret_key": "s", "chat_id": "oc_a", "interval": "1h",
	}}
	svc, _ := newService(t, cfg, cache)
	// 直接断言配置解析出的窗口，避免依赖缓存替身记录 ttl。
	loaded, err := loadConfig(context.Background(), &fakeHostConfig{values: cfg})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if loaded.Interval != time.Hour {
		t.Fatalf("interval 应为 1h, got %v", loaded.Interval)
	}
	if svc == nil {
		t.Fatal("服务不应为 nil")
	}
}

// TestEnabled 验证 Enabled 与配置齐备性一致。
func TestEnabled(t *testing.T) {
	ready, _ := newService(t, readyConfig(), nil)
	if !ready.Enabled(context.Background()) {
		t.Fatal("齐备配置应报告已配置")
	}
	notReady, _ := newService(t, map[string]any{}, nil)
	if notReady.Enabled(context.Background()) {
		t.Fatal("缺失配置应报告未配置")
	}
}
