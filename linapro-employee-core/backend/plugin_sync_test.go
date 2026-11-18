// plugin_sync_test.go 验证同步入口 newLarkFetchers 只为身份应用构造通讯录枚举器：
// lark_apps 里平铺登记的免登应用（有 identity_app_id）不参与同步。

package backend

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
)

// syncHostConfig 是 hostconfigcap.Service 的最小假实现，只回答 lark_apps 一个键。
type syncHostConfig struct {
	larkApps any
}

var _ hostconfigcap.Service = (*syncHostConfig)(nil)

func (f *syncHostConfig) Get(_ context.Context, key string, _ any) (*gvar.Var, error) {
	if key == lark.LarkAppsConfigKey && f.larkApps != nil {
		return gvar.New(f.larkApps), nil
	}
	return nil, nil
}

func (f *syncHostConfig) Exists(_ context.Context, key string) (bool, error) {
	return key == lark.LarkAppsConfigKey && f.larkApps != nil, nil
}

func (f *syncHostConfig) String(_ context.Context, _ string, defaultValue string) (string, error) {
	return defaultValue, nil
}

func (f *syncHostConfig) Bool(_ context.Context, _ string, defaultValue bool) (bool, error) {
	return defaultValue, nil
}

func (f *syncHostConfig) Int(_ context.Context, _ string, defaultValue int) (int, error) {
	return defaultValue, nil
}

func (f *syncHostConfig) Duration(_ context.Context, _ string, defaultValue time.Duration) (time.Duration, error) {
	return defaultValue, nil
}

func (f *syncHostConfig) SysConfig() hostconfigcap.SysConfigService { return nil }

// fetcherSummary 把枚举器清单摘要为「app_id + assoc」序列，便于比较。
func fetcherSummary(t *testing.T, apps any) []string {
	t.Helper()
	fetchers, err := newLarkFetchers(context.Background(), &syncHostConfig{larkApps: apps})
	if err != nil {
		t.Fatalf("newLarkFetchers: %v", err)
	}
	out := make([]string, 0, len(fetchers))
	for _, f := range fetchers {
		adapter, ok := f.(*larkAppFetcherAdapter)
		if !ok {
			t.Fatalf("枚举器类型不符: %T", f)
		}
		summary := adapter.client.AppID()
		if adapter.withAssoc {
			summary += "+assoc"
		}
		out = append(out, summary)
	}
	return out
}

// TestNewLarkFetchers_IgnoresLoginApps 验证在现有身份应用之外平铺登记两个免登应用后，
// 同步构造的枚举器清单与未登记时完全一致（同样的应用、同样的顺序与 assoc 开关）。
func TestNewLarkFetchers_IgnoresLoginApps(t *testing.T) {
	identityOnly := []any{
		map[string]any{"app_id": "cli_hegs", "secret_key": "s1", "assoc": true},
		map[string]any{"app_id": "cli_xj", "secret_key": "s2"},
	}
	withLoginApps := []any{
		identityOnly[0],
		identityOnly[1],
		map[string]any{"app_id": "cli_eval_hegs", "secret_key": "e1", "identity_app_id": "cli_hegs"},
		map[string]any{"app_id": "cli_eval_xj", "secret_key": "e2", "identity_app_id": "cli_xj"},
	}

	before := fetcherSummary(t, identityOnly)
	after := fetcherSummary(t, withLoginApps)
	want := []string{"cli_hegs+assoc", "cli_xj"}
	if len(before) != len(want) || len(after) != len(want) {
		t.Fatalf("枚举器数量不符: want %v, before %v, after %v", want, before, after)
	}
	for i := range want {
		if before[i] != want[i] || after[i] != want[i] {
			t.Fatalf("配置免登应用后同步清单不应变化: want %v, before %v, after %v", want, before, after)
		}
	}
}

// TestNewLarkFetchers_RejectsInvalidLoginApp 验证免登应用配置非法（identity_app_id 指向
// 清单外应用）时同步直接报错，而不是跳过坏条目继续空跑。
func TestNewLarkFetchers_RejectsInvalidLoginApp(t *testing.T) {
	apps := []any{
		map[string]any{"app_id": "cli_hegs", "secret_key": "s1"},
		map[string]any{"app_id": "cli_eval", "secret_key": "e", "identity_app_id": "cli_unknown"},
	}
	if _, err := newLarkFetchers(context.Background(), &syncHostConfig{larkApps: apps}); err == nil {
		t.Fatal("免登应用配置非法时同步应报错")
	}
}
