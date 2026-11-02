// alert_test.go 提供 alert 组件测试共用的假实现与配置构造助手。

package alert

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/cachecap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// fakeHostConfig 是宿主配置能力的替身：按预置值回答。
type fakeHostConfig struct {
	hostconfigcap.Service
	values map[string]any
	err    error
}

func (f *fakeHostConfig) Get(_ context.Context, key string, defaultValue any) (*gvar.Var, error) {
	if f.err != nil {
		return nil, f.err
	}
	if v, ok := f.values[key]; ok && v != nil {
		return gvar.New(v), nil
	}
	if defaultValue != nil {
		return gvar.New(defaultValue), nil
	}
	return nil, nil
}

// sentAlert 是一次假发送记录。
type sentAlert struct {
	appID      string
	appSecret  string
	receiveTyp string
	receiveID  string
	text       string
}

// fakeSender 是 MessageSender 的替身：记录发送，可指定失败。
type fakeSender struct {
	appID     string
	appSecret string
	calls     *[]sentAlert
	err       error
}

func (f *fakeSender) SendText(_ context.Context, receiveIDType, receiveID, text string) (string, error) {
	*f.calls = append(*f.calls, sentAlert{
		appID: f.appID, appSecret: f.appSecret, receiveTyp: receiveIDType, receiveID: receiveID, text: text,
	})
	if f.err != nil {
		return "", f.err
	}
	return "om_alert", nil
}

// fakeCache 是防抖缓存的替身：记录 Incr 调用次数，可指定失败。
type fakeCache struct {
	cachecap.Service
	counts map[string]int64
	err    error
	calls  int
}

func (c *fakeCache) Incr(
	_ context.Context,
	namespace, key string,
	delta int64,
	_ time.Duration,
) (*cachecap.CacheItem, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	full := namespace + "/" + key
	c.counts[full] += delta
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: c.counts[full]}, nil
}

// newFakeCache 构造空的内存防抖缓存替身。
func newFakeCache() *fakeCache {
	return &fakeCache{counts: make(map[string]int64)}
}

// readyConfig 返回一份齐备的告警配置值。
func readyConfig() map[string]any {
	return map[string]any{ConfigKey: map[string]any{
		"app_id":     "cli_alert",
		"secret_key": "alert-secret",
		"chat_id":    "oc_alert",
		"interval":   "10m",
	}}
}

// newService 构造注入了假配置、假缓存、假发送器的服务，并返回发送记录。
func newService(t *testing.T, cfg map[string]any, cache cachecap.Service) (Service, *[]sentAlert) {
	t.Helper()
	calls := &[]sentAlert{}
	svc, err := New(
		&fakeHostConfig{values: cfg},
		cache,
		func(appID, appSecret string) MessageSender {
			return &fakeSender{appID: appID, appSecret: appSecret, calls: calls}
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, calls
}

// TestNew_RequiresClientFactory 验证发送器工厂是必填依赖：缺失时构造失败，
// 而不是在运行期退化为自行创建客户端。
func TestNew_RequiresClientFactory(t *testing.T) {
	svc, err := New(nil, nil, nil)
	if err == nil {
		t.Fatal("缺发送器工厂时应返回错误")
	}
	if svc != nil {
		t.Fatalf("构造失败时应返回 nil 实现, got %T", svc)
	}
}

// TestNew_AllowsNilCacheAndHostConfig 验证 hostConfig 与 cache 可为 nil：
// 前者表示告警不可用（预期降级），后者表示无法防抖（退化为每次都发），都不是装配缺陷。
func TestNew_AllowsNilCacheAndHostConfig(t *testing.T) {
	svc, err := New(nil, nil, func(string, string) MessageSender {
		return &fakeSender{calls: &[]sentAlert{}}
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.Enabled(context.Background()) {
		t.Fatal("宿主配置缺失时告警通道应报告未配置")
	}
}
