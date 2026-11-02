// send_test.go 提供 send 组件测试共用的假实现与配置构造助手。
//
// fakeHostConfig / fakeSender 分别替身宿主配置能力与飞书发送器；各组用例通过 newService
// 显式注入它们，不依赖真实宿主编排或飞书网络。

package send

import (
	"context"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

type fakeHostConfig struct {
	hostconfigcap.Service
	values map[string]any
}

func (f *fakeHostConfig) Get(_ context.Context, key string, defaultValue any) (*gvar.Var, error) {
	if v, ok := f.values[key]; ok && v != nil {
		return gvar.New(v), nil
	}
	if defaultValue != nil {
		return gvar.New(defaultValue), nil
	}
	return nil, nil
}

// sentCall 是一次假发送记录。text 为文本正文、卡片 JSON 或文件 key，card 标识卡片消息，
// kind 标识文件相关调用（upload-file / upload-image / file / image，文本与卡片为空）。

type sentCall struct {
	appID      string
	receiveID  string
	receiveTyp string
	text       string
	card       bool
	kind       string
}

// fakeSender 是 MessageSender 的假实现：按凭据记录调用，可指定某个 user_id 失败。

type fakeSender struct {
	appID   string
	calls   *[]sentCall
	failFor map[string]error
}

func (f *fakeSender) SendText(_ context.Context, receiveIDType, receiveID, text string) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, receiveID: receiveID, receiveTyp: receiveIDType, text: text})
	if err, bad := f.failFor[receiveID]; bad {
		return "", err
	}
	return "om_" + f.appID + "_" + receiveID, nil
}

func (f *fakeSender) SendInteractiveCard(_ context.Context, receiveIDType, receiveID, cardJSON string) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, receiveID: receiveID, receiveTyp: receiveIDType, text: cardJSON, card: true})
	if err, bad := f.failFor[receiveID]; bad {
		return "", err
	}
	return "om_" + f.appID + "_" + receiveID, nil
}

// UploadFile 记录一次文件上传；failFor["upload:<appID>"] 命中时返回错误。

func (f *fakeSender) UploadFile(_ context.Context, fileType, fileName string, _ []byte) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, text: fileType + ":" + fileName, kind: "upload-file"})
	if err, bad := f.failFor["upload:"+f.appID]; bad {
		return "", err
	}
	return "file_" + f.appID, nil
}

// UploadImage 记录一次图片上传；failFor["upload:<appID>"] 命中时返回错误。

func (f *fakeSender) UploadImage(_ context.Context, _ []byte) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, kind: "upload-image"})
	if err, bad := f.failFor["upload:"+f.appID]; bad {
		return "", err
	}
	return "img_" + f.appID, nil
}

// SendFile 记录一次文件消息发送。

func (f *fakeSender) SendFile(_ context.Context, receiveIDType, receiveID, fileKey string) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, receiveID: receiveID, receiveTyp: receiveIDType, text: fileKey, kind: "file"})
	if err, bad := f.failFor[receiveID]; bad {
		return "", err
	}
	return "om_" + f.appID + "_" + receiveID, nil
}

// SendImage 记录一次图片消息发送。

func (f *fakeSender) SendImage(_ context.Context, receiveIDType, receiveID, imageKey string) (string, error) {
	*f.calls = append(*f.calls, sentCall{appID: f.appID, receiveID: receiveID, receiveTyp: receiveIDType, text: imageKey, kind: "image"})
	if err, bad := f.failFor[receiveID]; bad {
		return "", err
	}
	return "om_" + f.appID + "_" + receiveID, nil
}

// testGroup 是用例默认使用的推送分组（调用方插件 ID）。

const testGroup = "linapro-eval-360"

// groupsConfig 把「分组名 → 推送应用数组」包装成宿主配置值。

func groupsConfig(groups map[string]any) map[string]any {
	return map[string]any{larkcfg.ConfigKey: groups}
}

// twoCompanyConfig 返回 testGroup 分组下两企业各一个推送应用的配置（identity_app_id 指向
// 各企业的身份应用，但身份应用的凭据不在本插件的配置里、本插件也从不读取）。

func twoCompanyConfig(hostConfigKey string) map[string]any {
	pushApps := []any{
		map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "系统通知(霍尔果斯)", "identity_app_id": "cli_a"},
		map[string]any{"app_id": "cli_push_b", "secret_key": "pb", "name": "系统通知(星嘉)", "identity_app_id": "cli_b"},
	}
	return map[string]any{
		larkcfg.ConfigKey: map[string]any{testGroup: pushApps},
		hostConfigKey:     nil,
	}
}

// fakeAlerter 是 alert.Service 的假实现：记录收到的告警事件。
type fakeAlerter struct {
	events *[]alert.Event
}

func (f *fakeAlerter) Notify(_ context.Context, ev alert.Event) {
	if f.events != nil {
		*f.events = append(*f.events, ev)
	}
}

func (f *fakeAlerter) Enabled(context.Context) bool { return true }

// newAlerter 返回一个记录告警事件的假通道。
func newAlerter() (*fakeAlerter, *[]alert.Event) {
	events := &[]alert.Event{}
	return &fakeAlerter{events: events}, events
}

// newService 构造注入了假配置、假索引、假发送器的 Service。
func newService(t *testing.T, cfgValues map[string]any, idx map[string]empcap.LarkUserIDIndex, failFor map[string]error) (Service, *[]sentCall) {
	t.Helper()
	return newServiceWithAlerter(t, cfgValues, idx, failFor, nil)
}

// newServiceWithAlerter 与 newService 相同，但可注入告警通道并返回它记录的事件。
func newServiceWithAlerter(
	t *testing.T,
	cfgValues map[string]any,
	idx map[string]empcap.LarkUserIDIndex,
	failFor map[string]error,
	alerter alert.Service,
) (Service, *[]sentCall) {
	t.Helper()
	calls := &[]sentCall{}
	svc, err := New(
		&fakeHostConfig{values: cfgValues},
		func(appID, appSecret string) MessageSender {
			// 断言凭据来源：推送应用必须用自己的 secret 构造客户端，绝不取身份应用。
			want := map[string]string{"cli_push_a": "pa", "cli_push_b": "pb", "cli_push_c": "pc"}[appID]
			if appSecret != want {
				t.Fatalf("推送应用 %s 的凭据取错：want secret %q, got %q", appID, want, appSecret)
			}
			return &fakeSender{appID: appID, calls: calls, failFor: failFor}
		},
		fakeIndexPath(idx),
		alerter,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, calls
}

// fakeIndexPath 返回按 identity_app_id 取内存索引的寻址来源；未登记的企业返回空索引。
func fakeIndexPath(idx map[string]empcap.LarkUserIDIndex) IndexPath {
	return func(_ context.Context, identityAppID string) (empcap.LarkUserIDIndex, error) {
		v, ok := idx[identityAppID]
		if !ok {
			return empcap.LarkUserIDIndex{ByEmployeeNo: map[string]string{}}, nil
		}
		return v, nil
	}
}

// TestNew_RequiresRuntimeDependencies 验证发送器工厂与寻址来源是必填依赖：缺失时构造失败，
// 而不是在运行期退化为自行创建客户端。
func TestNew_RequiresRuntimeDependencies(t *testing.T) {
	hostCfg := &fakeHostConfig{values: twoCompanyConfig("x")}
	noopFactory := func(string, string) MessageSender { return &fakeSender{calls: &[]sentCall{}} }
	noopIndex := fakeIndexPath(nil)

	cases := []struct {
		name  string
		build func() (Service, error)
	}{
		{name: "缺发送器工厂", build: func() (Service, error) { return New(hostCfg, nil, noopIndex, nil) }},
		{name: "缺寻址来源", build: func() (Service, error) { return New(hostCfg, noopFactory, nil, nil) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, err := c.build()
			if err == nil {
				t.Fatal("缺必填依赖时应返回错误")
			}
			if svc != nil {
				t.Fatalf("构造失败时应返回 nil 实现, got %T", svc)
			}
		})
	}
}

// TestStatus 验证自检报告给出「凭据齐备」「可寻址人数」，并把配置问题列出来。
func TestStatus(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u1", "E2": "u2"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	cfg := groupsConfig(map[string]any{
		testGroup: []any{
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "A", "identity_app_id": "cli_a"},
			// 与上一条引用同一个身份应用 → 配置非法（同一人会收到两条）。
			map[string]any{"app_id": "cli_push_bad", "secret_key": "pb", "name": "B", "identity_app_id": "cli_a"},
		},
		// 另一分组复用同一个推送应用：合法，自检按分组各列一条。
		"linapro-attend-poc": []any{
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "A", "identity_app_id": "cli_a"},
		},
	})
	svc, _ := newService(t, cfg, idx, nil)
	report := svc.Status(context.Background())

	if !report.PluginReady {
		t.Fatal("PluginReady 应为 true")
	}
	if report.ConfigValid {
		t.Fatalf("同一身份应用被两个推送应用引用应判配置非法: %+v", report.ConfigIssues)
	}
	for _, issue := range report.ConfigIssues {
		if !strings.HasPrefix(issue, "push_groups."+testGroup+"[") {
			t.Fatalf("问题项应只来自非法分组且带分组路径: %v", report.ConfigIssues)
		}
	}
	if len(report.PushApps) != 3 {
		t.Fatalf("应按「分组 × 推送应用」报告 3 条, got %d", len(report.PushApps))
	}
	// 排序：分组名优先（linapro-attend-poc < linapro-eval-360），组内按 App ID。
	first := report.PushApps[0]
	if first.Group != "linapro-attend-poc" || first.AppID != "cli_push_a" {
		t.Fatalf("排序或分组标注不符: %+v", first)
	}
	second := report.PushApps[1]
	if second.Group != testGroup || second.AppID != "cli_push_a" || !second.CredentialReady || second.AddressableEmployees != 2 {
		t.Fatalf("cli_push_a 自检不符: %+v", second)
	}
	// 同一 identity_app_id 只探一次，各行拿到同一份索引。
	third := report.PushApps[2]
	if third.AppID != "cli_push_bad" || third.AddressableEmployees != 2 || third.ProbeError != "" {
		t.Fatalf("cli_push_bad 自检不符: %+v", third)
	}
}

// TestStatus_NilHostConfig 验证宿主配置能力缺失时自检报告给出原因而不 panic：
// 宿主未暴露配置能力是规范定义的降级状态，不是装配错误，所以 New 仍应成功。
func TestStatus_NilHostConfig(t *testing.T) {
	svc, err := New(nil,
		func(string, string) MessageSender { return &fakeSender{calls: &[]sentCall{}} },
		func(context.Context, string) (empcap.LarkUserIDIndex, error) {
			return empcap.LarkUserIDIndex{}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	report := svc.Status(context.Background())
	if report.ConfigValid || len(report.ConfigIssues) == 0 {
		t.Fatalf("宿主配置缺失时应报问题: %+v", report)
	}
}

// TestSendText_IndexError 验证取索引失败时整体返回错误（寻址是全员共用前置条件）。
