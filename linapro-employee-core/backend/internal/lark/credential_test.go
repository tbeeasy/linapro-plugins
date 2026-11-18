package lark

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// fakeHostConfig 是 hostconfigcap.Service 的最小假实现：只回答静态配置键，
// 其余方法留空。配置值按 key 预置，nil 表示键不存在。
type fakeHostConfig struct {
	values map[string]any
	getErr error // 非 nil 时 Get 返回该错误，模拟配置后端故障
}

var _ hostconfigcap.Service = (*fakeHostConfig)(nil)

func (f *fakeHostConfig) Get(_ context.Context, key string, defaultValue any) (*gvar.Var, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if v, ok := f.values[key]; ok && v != nil {
		return gvar.New(v), nil
	}
	if defaultValue != nil {
		return gvar.New(defaultValue), nil
	}
	return nil, nil
}

func (f *fakeHostConfig) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.values[key]
	return ok, nil
}

func (f *fakeHostConfig) String(_ context.Context, key string, defaultValue string) (string, error) {
	v, _ := f.Get(context.Background(), key, nil)
	if v == nil {
		return defaultValue, nil
	}
	return v.String(), nil
}

func (f *fakeHostConfig) Bool(_ context.Context, key string, defaultValue bool) (bool, error) {
	return defaultValue, nil
}

func (f *fakeHostConfig) Int(_ context.Context, key string, defaultValue int) (int, error) {
	return defaultValue, nil
}

func (f *fakeHostConfig) Duration(_ context.Context, key string, defaultValue time.Duration) (time.Duration, error) {
	return defaultValue, nil
}

func (f *fakeHostConfig) SysConfig() hostconfigcap.SysConfigService { return nil }

// appIDs 取出清单里的 app_id 序列，便于断言顺序与内容。
func appIDs(apps []AppConfig) []string {
	out := make([]string, 0, len(apps))
	for _, app := range apps {
		out = append(out, app.AppID)
	}
	return out
}

// equalStrings 比较两个字符串切片（顺序敏感）。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLoadApps_ReadsOwnKey 验证从本插件私有键 lark_apps 读取，顺序与字段保持原样。
func TestLoadApps_ReadsOwnKey(t *testing.T) {
	hc := &fakeHostConfig{values: map[string]any{
		LarkAppsConfigKey: []any{
			map[string]any{"app_id": "cli_a", "secret_key": "sa"},
			map[string]any{"app_id": "cli_b", "secret_key": "sb", "assoc": true},
		},
	}}
	got, err := LoadApps(context.Background(), hc)
	if err != nil {
		t.Fatalf("LoadApps: %v", err)
	}
	if want := []string{"cli_a", "cli_b"}; !equalStrings(appIDs(got), want) {
		t.Fatalf("app_id 顺序不符: want %v, got %v", want, appIDs(got))
	}
	if got[1].SecretKey != "sb" || !got[1].Assoc {
		t.Fatalf("字段解析不符: %+v", got[1])
	}
}

// TestLoadApps_ConfigKeyIsPrivate 验证配置键是员工核心自己的私有键
// （推送应用凭据属于 linapro-lark-push，本插件不应读取）。
func TestLoadApps_ConfigKeyIsPrivate(t *testing.T) {
	if LarkAppsConfigKey != "plugin.linapro-employee-core.lark_apps" {
		t.Fatalf("配置键不符: %s", LarkAppsConfigKey)
	}
}

// TestLoadApps_NoConfig 验证配置缺失时返回空清单且不报错。
func TestLoadApps_NoConfig(t *testing.T) {
	got, err := LoadApps(context.Background(), &fakeHostConfig{})
	if err != nil {
		t.Fatalf("缺失配置不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("无配置时应返回空清单，实际 %v", appIDs(got))
	}
}

// TestLoadApps_InvalidFormat 验证结构非法时报错（不静默当作没配）。
func TestLoadApps_InvalidFormat(t *testing.T) {
	cases := map[string]any{
		"清单给了字符串":  "oops",
		"清单给了单个对象": map[string]any{"app_id": "cli_a"},
		"清单元素不是对象": []any{"oops"},
	}
	for name, value := range cases {
		hc := &fakeHostConfig{values: map[string]any{LarkAppsConfigKey: value}}
		_, err := LoadApps(context.Background(), hc)
		if !bizerr.Is(err, CodeLarkAppsConfigInvalid) {
			t.Fatalf("%s：结构非法时应返回 CodeLarkAppsConfigInvalid，实际 %v", name, err)
		}
	}
}

// TestLoadApps_ReadFailure 验证配置后端读取失败时返回 bizerr（会经登录接口返回
// 调用方，不能是裸错误文案），且原始原因作为 cause 保留。
func TestLoadApps_ReadFailure(t *testing.T) {
	cause := errors.New("config backend down")
	_, err := LoadApps(context.Background(), &fakeHostConfig{getErr: cause})
	if !bizerr.Is(err, CodeLarkAppsConfigReadFailed) {
		t.Fatalf("读取失败应返回 CodeLarkAppsConfigReadFailed，实际 %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("应保留原始原因作为 cause，实际 %v", err)
	}
}

// TestLoadApps_ExcludesLoginApps 验证配置了免登应用（有 identity_app_id）后，LoadApps 返回的
// 身份应用清单与未配置时完全一致——同步（newLarkFetchers）、后台下拉与员工详情都走 LoadApps，
// 不会因为多登记一个免登应用就去枚举它的通讯录或在下拉里露出它。
func TestLoadApps_ExcludesLoginApps(t *testing.T) {
	identityOnly := []any{
		map[string]any{"app_id": "cli_a", "secret_key": "sa", "assoc": true},
		map[string]any{"app_id": "cli_b", "secret_key": "sb"},
	}
	withLoginApps := []any{
		identityOnly[0],
		identityOnly[1],
		map[string]any{"app_id": "cli_eval_a", "secret_key": "ea", "identity_app_id": "cli_a"},
		map[string]any{"app_id": "cli_eval_b", "secret_key": "eb", "identity_app_id": "cli_b"},
	}
	ctx := context.Background()
	before, err := LoadApps(ctx, &fakeHostConfig{values: map[string]any{LarkAppsConfigKey: identityOnly}})
	if err != nil {
		t.Fatalf("LoadApps(仅身份应用): %v", err)
	}
	after, err := LoadApps(ctx, &fakeHostConfig{values: map[string]any{LarkAppsConfigKey: withLoginApps}})
	if err != nil {
		t.Fatalf("LoadApps(含免登应用): %v", err)
	}
	if !equalStrings(appIDs(before), appIDs(after)) {
		t.Fatalf("配置免登应用后身份应用清单不应变化: before %v, after %v", appIDs(before), appIDs(after))
	}
	if !after[0].Assoc || after[1].Assoc || after[0].IdentityAppID != "" {
		t.Fatalf("身份应用字段不应受影响: %+v", after)
	}
}

// TestLoadApps_ValidatesLoginApps 验证清单校验：app_id 重复、identity_app_id 指向自身 /
// 另一个免登应用 / 清单外应用都返回 CodeLarkAppsConfigInvalid，且登录（NewCredentialLoader）
// 同样拒绝，不带病空跑。
func TestLoadApps_ValidatesLoginApps(t *testing.T) {
	identity := map[string]any{"app_id": "cli_a", "secret_key": "sa"}
	cases := map[string][]any{
		"app_id 重复": {
			identity,
			map[string]any{"app_id": "cli_a", "secret_key": "sa2"},
		},
		"身份应用与免登应用 app_id 重复": {
			identity,
			map[string]any{"app_id": "cli_a", "secret_key": "x", "identity_app_id": "cli_a"},
		},
		"identity_app_id 指向自身": {
			identity,
			map[string]any{"app_id": "cli_eval", "secret_key": "e", "identity_app_id": "cli_eval"},
		},
		"identity_app_id 指向另一个免登应用": {
			identity,
			map[string]any{"app_id": "cli_eval1", "secret_key": "e1", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "cli_eval2", "secret_key": "e2", "identity_app_id": "cli_eval1"},
		},
		"identity_app_id 指向清单外应用": {
			identity,
			map[string]any{"app_id": "cli_eval", "secret_key": "e", "identity_app_id": "cli_missing"},
		},
	}
	ctx := context.Background()
	for name, value := range cases {
		hc := &fakeHostConfig{values: map[string]any{LarkAppsConfigKey: value}}
		if _, err := LoadApps(ctx, hc); !bizerr.Is(err, CodeLarkAppsConfigInvalid) {
			t.Fatalf("%s：LoadApps 应返回 CodeLarkAppsConfigInvalid，实际 %v", name, err)
		}
		if _, err := NewCredentialLoader(hc)(ctx, "cli_a"); !bizerr.Is(err, CodeLarkAppsConfigInvalid) {
			t.Fatalf("%s：登录取凭据应返回 CodeLarkAppsConfigInvalid，实际 %v", name, err)
		}
	}
}

// TestNewCredentialLoader_LoginApp 验证登录能取到免登应用自己的凭据并带上 IdentityAppID，
// 身份应用的凭据 IdentityAppID 为空。
func TestNewCredentialLoader_LoginApp(t *testing.T) {
	hc := &fakeHostConfig{values: map[string]any{
		LarkAppsConfigKey: []any{
			map[string]any{"app_id": "cli_a", "secret_key": "sa"},
			map[string]any{"app_id": "cli_eval", "secret_key": "se", "identity_app_id": "cli_a"},
		},
	}}
	load := NewCredentialLoader(hc)
	ctx := context.Background()

	got, err := load(ctx, "cli_eval")
	if err != nil {
		t.Fatalf("免登应用应解析成功: %v", err)
	}
	if got.AppID != "cli_eval" || got.AppSecret != "se" || got.IdentityAppID != "cli_a" {
		t.Fatalf("免登应用凭据不符: %+v", got)
	}
	got, err = load(ctx, "cli_a")
	if err != nil {
		t.Fatalf("身份应用应解析成功: %v", err)
	}
	if got.AppSecret != "sa" || got.IdentityAppID != "" {
		t.Fatalf("身份应用凭据不符: %+v", got)
	}
}

// TestNewCredentialLoader 验证凭据解析：命中、密钥为空、app_id 未知、app_id 空串。
func TestNewCredentialLoader(t *testing.T) {
	hc := &fakeHostConfig{values: map[string]any{
		LarkAppsConfigKey: []any{
			map[string]any{"app_id": "cli_a", "secret_key": "sa"},
			map[string]any{"app_id": "cli_blank", "secret_key": ""},
		},
	}}
	load := NewCredentialLoader(hc)
	ctx := context.Background()

	got, err := load(ctx, "cli_a")
	if err != nil {
		t.Fatalf("已知应用应解析成功: %v", err)
	}
	if got.AppID != "cli_a" || got.AppSecret != "sa" {
		t.Fatalf("凭据不符: %+v", got)
	}

	cases := map[string]string{
		"密钥为空":      "cli_blank",
		"app_id 未知": "cli_unknown",
		"app_id 空串": "",
	}
	for name, id := range cases {
		if _, err := load(ctx, id); err == nil {
			t.Fatalf("%s 时应按失败关闭返回错误", name)
		}
	}
}
