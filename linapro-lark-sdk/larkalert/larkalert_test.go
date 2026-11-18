// larkalert_test.go 锁定告警库的四条边界：未配置静默、窗口内只发一次、发送失败不 panic、
// 正文含 Kind 与 Detail。

package larkalert

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/cachecap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// 测试用配置键与告警类型。
const (
	testConfigKey = "plugin.test.alert"
	testKind      = "test_kind"
)

// fakeConfig 是宿主配置能力替身：内嵌接口满足方法集，只重写库内用到的 Get；
// 调到其余方法会因内嵌 nil 而 panic，借此暴露库内新增的未预期依赖。
// value 为 nil 表示键缺失。
type fakeConfig struct {
	hostconfigcap.Service
	value any
}

// Get 返回配置值；未设置时返回 nil，模拟键缺失。
func (f *fakeConfig) Get(_ context.Context, _ string, _ any) (*gvar.Var, error) {
	if f.value == nil {
		return nil, nil
	}
	return gvar.New(f.value), nil
}

// fakeCache 是宿主缓存能力替身：同样内嵌接口，只重写 Incr，记录调用次数并返回累计值。
type fakeCache struct {
	cachecap.Service
	mu     sync.Mutex
	counts map[string]int64
}

// Incr 累加计数并返回结果。
func (f *fakeCache) Incr(_ context.Context, _ string, key string, delta int64, _ time.Duration) (*cachecap.CacheItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = make(map[string]int64)
	}
	f.counts[key] += delta
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: f.counts[key]}, nil
}

// fakeSender 记录发出的文本。
type fakeSender struct {
	mu    sync.Mutex
	texts []string
	err   error
}

// SendText 记录文本并返回固定 message_id 或错误。
func (f *fakeSender) SendText(_ context.Context, _, _, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	if f.err != nil {
		return "", f.err
	}
	return "om_test", nil
}

// sentTexts 返回已发送的文本副本。
func (f *fakeSender) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	texts := make([]string, len(f.texts))
	copy(texts, f.texts)
	return texts
}

// readyConfig 返回一份三项标识齐备的配置。
func readyConfig() *fakeConfig {
	return &fakeConfig{value: map[string]any{
		"app_id":     "cli_test",
		"secret_key": "secret",
		"chat_id":    "oc_test",
		"interval":   "30m",
	}}
}

// TestNotifySilentWhenConfigMissing 锁定：配置键缺失时静默丢弃，不发送、不 panic。
func TestNotifySilentWhenConfigMissing(t *testing.T) {
	sender := &fakeSender{}
	Notify(context.Background(), Env{
		Config:    &fakeConfig{},
		Cache:     &fakeCache{},
		NewClient: func(_, _ string) TextSender { return sender },
	}, Event{ConfigKey: testConfigKey, Kind: testKind, Detail: "d"})

	if len(sender.sentTexts()) != 0 {
		t.Fatalf("未配置告警通道时不应发送，实际发送 %d 条", len(sender.sentTexts()))
	}
}

// TestNotifySilentWhenCredentialBlank 锁定：三项标识缺任一项即静默。
func TestNotifySilentWhenCredentialBlank(t *testing.T) {
	sender := &fakeSender{}
	Notify(context.Background(), Env{
		Config: &fakeConfig{value: map[string]any{
			"app_id":     "cli_test",
			"secret_key": "",
			"chat_id":    "oc_test",
		}},
		Cache:     &fakeCache{},
		NewClient: func(_, _ string) TextSender { return sender },
	}, Event{ConfigKey: testConfigKey, Kind: testKind})

	if len(sender.sentTexts()) != 0 {
		t.Fatalf("chat_id 或 secret 为空时不应发送，实际发送 %d 条", len(sender.sentTexts()))
	}
}

// TestNotifySendsOnceWithinWindow 锁定：同一「配置键 + 种类」在窗口内只发一次。
func TestNotifySendsOnceWithinWindow(t *testing.T) {
	sender := &fakeSender{}
	env := Env{
		Config:    readyConfig(),
		Cache:     &fakeCache{},
		NewClient: func(_, _ string) TextSender { return sender },
	}
	event := Event{ConfigKey: testConfigKey, Kind: testKind, Detail: "first"}

	Notify(context.Background(), env, event)
	Notify(context.Background(), env, event)
	Notify(context.Background(), env, event)

	if got := len(sender.sentTexts()); got != 1 {
		t.Fatalf("防抖窗口内应只发送 1 条，实际 %d 条", got)
	}
}

// TestNotifyBuildsBodyWithKindAndDetail 锁定：正文包含 Kind 与 Detail。
func TestNotifyBuildsBodyWithKindAndDetail(t *testing.T) {
	sender := &fakeSender{}
	Notify(context.Background(), Env{
		Config:    readyConfig(),
		Cache:     &fakeCache{},
		NewClient: func(_, _ string) TextSender { return sender },
	}, Event{ConfigKey: testConfigKey, Kind: "apilog_partition_missing", Detail: "分区 p202705 创建失败"})

	texts := sender.sentTexts()
	if len(texts) != 1 {
		t.Fatalf("应发送 1 条，实际 %d 条", len(texts))
	}
	if !strings.Contains(texts[0], "apilog_partition_missing") {
		t.Fatalf("正文应包含告警类型，实际为 %q", texts[0])
	}
	if !strings.Contains(texts[0], "分区 p202705 创建失败") {
		t.Fatalf("正文应包含详情，实际为 %q", texts[0])
	}
}

// TestNotifySwallowsSendFailure 锁定：发送失败只记日志，不 panic、不回传。
func TestNotifySwallowsSendFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("飞书不可达")}
	Notify(context.Background(), Env{
		Config:    readyConfig(),
		Cache:     &fakeCache{},
		NewClient: func(_, _ string) TextSender { return sender },
	}, Event{ConfigKey: testConfigKey, Kind: testKind})
}

// TestNotifyFailsOpenWhenCacheUnavailable 锁定：缓存不可用时放行（宁可多发不可漏发）。
func TestNotifyFailsOpenWhenCacheUnavailable(t *testing.T) {
	sender := &fakeSender{}
	env := Env{
		Config:    readyConfig(),
		Cache:     nil,
		NewClient: func(_, _ string) TextSender { return sender },
	}
	event := Event{ConfigKey: testConfigKey, Kind: testKind}

	Notify(context.Background(), env, event)
	Notify(context.Background(), env, event)

	if got := len(sender.sentTexts()); got != 2 {
		t.Fatalf("缓存不可用时应每次都发，实际 %d 条", got)
	}
}
