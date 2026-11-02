package larkcfg

import (
	"context"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/errors/gerror"
)

// fakeGetter 是 Getter 的假实现：按预置值回答，模拟宿主配置能力。
type fakeGetter struct {
	value any
	err   error
	// key 记录最后一次被问到的键，用于确认 Load 读的是本插件的私有键。
	key string
}

func (f *fakeGetter) Get(_ context.Context, key string, _ any) (*gvar.Var, error) {
	f.key = key
	if f.err != nil {
		return nil, f.err
	}
	if f.value == nil {
		return nil, nil
	}
	return gvar.New(f.value), nil
}

// validApps 返回一份两企业各一个推送应用的合法配置，供校验用例做基线。
func validApps() []PushApp {
	return []PushApp{
		{AppID: "cli_push_a", SecretKey: "pa", Name: "系统通知(霍尔果斯)", IdentityAppID: "cli_a"},
		{AppID: "cli_push_b", SecretKey: "pb", Name: "系统通知(星嘉)", IdentityAppID: "cli_b"},
	}
}

// testGroup 是校验用例使用的分组名。
const testGroup = "linapro-eval-360"

// TestLoad_ParsesGroups 验证分组映射被解析，且读的是本插件的私有配置键。
func TestLoad_ParsesGroups(t *testing.T) {
	f := &fakeGetter{value: map[string]any{
		"linapro-eval-360": []any{
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "系统通知(霍尔果斯)", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "cli_push_b", "secret_key": "pb", "name": "系统通知(星嘉)", "identity_app_id": "cli_b"},
		},
		"linapro-attend-poc": []any{
			map[string]any{"app_id": "cli_push_c", "secret_key": "pc", "name": "考勤助手", "identity_app_id": "cli_a"},
		},
	}}
	got, err := Load(context.Background(), f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.key != ConfigKey {
		t.Fatalf("应读取 %q，实际读的是 %q", ConfigKey, f.key)
	}
	if ConfigKey != "plugin.linapro-lark-push.push_groups" {
		t.Fatalf("推送配置必须是本插件私有键，实际 %q", ConfigKey)
	}
	if len(got) != 2 || len(got["linapro-eval-360"]) != 2 || len(got["linapro-attend-poc"]) != 1 {
		t.Fatalf("分组解析不符: %+v", got)
	}
	app := got["linapro-eval-360"][1]
	if app.AppID != "cli_push_b" || app.Name != "系统通知(星嘉)" || app.IdentityAppID != "cli_b" {
		t.Fatalf("推送应用解析不符: %+v", app)
	}
}

// TestLoad_GroupNameTrimmedAndNullValue 验证分组名去首尾空白，空值分组视为空清单。
func TestLoad_GroupNameTrimmedAndNullValue(t *testing.T) {
	got, err := Load(context.Background(), &fakeGetter{value: map[any]any{
		" linapro-eval-360 ": []any{map[string]any{"app_id": "cli_push_a"}},
		"linapro-attend-poc": nil,
	}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got["linapro-eval-360"]) != 1 {
		t.Fatalf("分组名应去首尾空白: %+v", got)
	}
	if apps, ok := got["linapro-attend-poc"]; !ok || len(apps) != 0 {
		t.Fatalf("空值分组应解析为空清单: %+v", got)
	}
}

// TestLoad_MissingConfig 验证配置缺失时返回空映射且不报错。
func TestLoad_MissingConfig(t *testing.T) {
	got, err := Load(context.Background(), &fakeGetter{})
	if err != nil {
		t.Fatalf("缺少配置不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("缺失时应返回空映射: %+v", got)
	}
}

// TestLoad_EmptyMap 验证显式空映射与缺失等价。
func TestLoad_EmptyMap(t *testing.T) {
	got, err := Load(context.Background(), &fakeGetter{value: map[string]any{}})
	if err != nil {
		t.Fatalf("空映射不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空映射应解析为空: %+v", got)
	}
}

// TestLoad_InvalidFormat 验证结构非法时返回错误且不返回部分结果；旧的平铺清单
// （push_apps 形态）同样判为非法，不做兼容。
func TestLoad_InvalidFormat(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{name: "不是映射（字符串）", value: "oops"},
		{name: "不是映射（平铺数组）", value: []any{map[string]any{"app_id": "cli_push_a"}}},
		{name: "分组值不是数组", value: map[string]any{"linapro-eval-360": map[string]any{"app_id": "cli_push_a"}}},
		{name: "分组元素不是对象", value: map[string]any{"linapro-eval-360": []any{"oops"}}},
		{name: "空分组名", value: map[string]any{"  ": []any{}}},
		{name: "去空白后分组名重复", value: map[string]any{"g": []any{}, " g ": []any{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Load(context.Background(), &fakeGetter{value: c.value})
			if err == nil {
				t.Fatalf("非法结构应返回错误，实际得到 %+v", got)
			}
			if len(got) != 0 {
				t.Fatalf("报错时不应返回部分结果: %+v", got)
			}
		})
	}
}

// TestLoad_GetterError 验证读取器报错时向上传递。
func TestLoad_GetterError(t *testing.T) {
	if _, err := Load(context.Background(), &fakeGetter{err: gerror.New("boom")}); err == nil {
		t.Fatal("读取失败时应返回错误")
	}
}

// TestLoad_NilGetter 验证未注入读取器时明确报错而不是空清单。
func TestLoad_NilGetter(t *testing.T) {
	if _, err := Load(context.Background(), nil); err == nil {
		t.Fatal("读取器为空时应返回错误")
	}
}

// TestValidate_Valid 验证合法配置不产问题项。
func TestValidate_Valid(t *testing.T) {
	if issues := Validate(testGroup, validApps()); len(issues) != 0 {
		t.Fatalf("合法配置不应有问题项: %v", issues)
	}
}

// TestValidate_Issues 验证各类非法配置都被识别，且问题项能定位到具体字段。
func TestValidate_Issues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(apps []PushApp)
		field  string // 期望出现在问题项里的字段路径
	}{
		{
			name:   "app_id 为空",
			mutate: func(a []PushApp) { a[0].AppID = "" },
			field:  "push_groups.linapro-eval-360[0].app_id",
		},
		{
			name:   "secret_key 为空",
			mutate: func(a []PushApp) { a[0].SecretKey = " " },
			field:  "push_groups.linapro-eval-360[0].secret_key",
		},
		{
			name:   "app_id 重复",
			mutate: func(a []PushApp) { a[1].AppID = "cli_push_a" },
			field:  "push_groups.linapro-eval-360[1].app_id",
		},
		{
			name:   "identity_app_id 为空",
			mutate: func(a []PushApp) { a[0].IdentityAppID = "" },
			field:  "push_groups.linapro-eval-360[0].identity_app_id",
		},
		{
			name:   "同一身份应用被两个推送应用引用",
			mutate: func(a []PushApp) { a[1].IdentityAppID = "cli_a" },
			field:  "push_groups.linapro-eval-360[1].identity_app_id",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apps := validApps()
			c.mutate(apps)
			issues := Validate(testGroup, apps)
			if len(issues) == 0 {
				t.Fatal("非法配置应产生问题项")
			}
			var found bool
			for _, it := range issues {
				if it.Field == c.field {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("期望问题项定位到 %q，实际: %v", c.field, issues)
			}
			if err := ErrValidate(issues); err == nil || !strings.Contains(err.Error(), c.field) {
				t.Fatalf("ErrValidate 应携带字段路径，实际: %v", err)
			}
		})
	}
}

// TestValidate_EmptyGroupHasNoIssues 验证未配置的分组（空清单）不产问题项：
// 「分组无推送应用」由发送侧以 CodeNoPushApp 报告，不是配置非法。
func TestValidate_EmptyGroupHasNoIssues(t *testing.T) {
	if issues := Validate(testGroup, nil); len(issues) != 0 {
		t.Fatalf("空分组不应有问题项: %v", issues)
	}
}

// TestValidate_DanglingIdentityAppIDIsNotDetected 说明一处刻意的能力缺口：
// identity_app_id 指向 employee-core 配置里的身份应用，本插件读不到那份配置，
// 故悬空引用在配置期无法识别——只能靠运行期 Status 的可寻址人数暴露。
func TestValidate_DanglingIdentityAppIDIsNotDetected(t *testing.T) {
	apps := validApps()
	apps[0].IdentityAppID = "cli_nope"
	if issues := Validate(testGroup, apps); len(issues) != 0 {
		t.Fatalf("悬空引用本就不在配置期校验范围内: %v", issues)
	}
}

// TestErrValidate_NoIssues 验证无问题项时不返回错误。
func TestErrValidate_NoIssues(t *testing.T) {
	if err := ErrValidate(nil); err != nil {
		t.Fatalf("无问题项不应返回错误: %v", err)
	}
}

// TestPushable 验证凭据不全的推送应用被剔除，其余照常可用。
func TestPushable(t *testing.T) {
	apps := validApps()
	apps[1].SecretKey = ""
	got := Pushable(apps)
	if len(got) != 1 || got[0].AppID != "cli_push_a" {
		t.Fatalf("应只保留凭据齐备的条目: %+v", got)
	}
	if len(Validate(testGroup, apps)) == 0 {
		t.Fatal("被剔除的条目仍应出现在校验问题项中")
	}
}
