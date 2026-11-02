// send_card_test.go 覆盖交互卡片投递与推送应用清单：卡片按推送应用构造、以 user_id
// 经推送应用凭据投递、构造失败只记该条明细、参数与配置错误、清单不含密钥。
package send

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lina-core/pkg/bizerr"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// TestSendCard_BuildPerPushApp 验证两企业扇出时，卡片按推送应用分别构造（Build 拿到该应用的
// 公开投影，含 identity_app_id），并以 user_id 经对应推送应用凭据投递为交互卡片。
func TestSendCard_BuildPerPushApp(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Build: func(app larkpushcap.PushApp, r larkpushcap.Recipient) (string, error) {
			return `{"push":"` + app.AppID + `","identity":"` + app.IdentityAppID + `","no":"` + r.EmployeeNo + `"}`, nil
		},
		BizSource: "test",
	})
	if err != nil {
		t.Fatalf("SendCard: %v", err)
	}
	if !res.AllSucceeded() || len(res.Deliveries) != 2 {
		t.Fatalf("两企业各发一张且全部成功, got %+v", res.Deliveries)
	}
	want := map[string]struct{ userID, card string }{
		"cli_push_a": {"u_a_1001", `{"push":"cli_push_a","identity":"cli_a","no":"E1001"}`},
		"cli_push_b": {"u_b_1001", `{"push":"cli_push_b","identity":"cli_b","no":"E1001"}`},
	}
	if len(*calls) != 2 {
		t.Fatalf("want 2 次发送, got %d", len(*calls))
	}
	for _, c := range *calls {
		w := want[c.appID]
		if !c.card {
			t.Fatalf("应以交互卡片投递: %+v", c)
		}
		if c.receiveTyp != larkim.ReceiveIDTypeUserID || c.receiveID != w.userID {
			t.Fatalf("推送应用 %s 应以 user_id=%s 投递, got %s=%s", c.appID, w.userID, c.receiveTyp, c.receiveID)
		}
		if c.text != w.card {
			t.Fatalf("推送应用 %s 的卡片内容 want %s, got %s", c.appID, w.card, c.text)
		}
	}
}

// TestSendCard_BuildErrorOnlyFailsThatDelivery 验证某推送应用构造卡片失败时只记该条为 failed、
// 不发送，同批另一企业照常投递。
func TestSendCard_BuildErrorOnlyFailsThatDelivery(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Build: func(app larkpushcap.PushApp, _ larkpushcap.Recipient) (string, error) {
			if app.AppID == "cli_push_b" {
				return "", errors.New("该应用未配置卡片模板")
			}
			return `{}`, nil
		},
	})
	if err != nil {
		t.Fatalf("SendCard: %v", err)
	}
	statusByApp := map[string]larkpushcap.Delivery{}
	for _, d := range res.Deliveries {
		statusByApp[d.PushAppID] = d
	}
	if statusByApp["cli_push_a"].Status != larkpushcap.DeliveryStatusSent {
		t.Fatalf("企业 A 应成功: %+v", statusByApp["cli_push_a"])
	}
	b := statusByApp["cli_push_b"]
	if b.Status != larkpushcap.DeliveryStatusFailed || !strings.Contains(b.Error, "未配置卡片模板") {
		t.Fatalf("企业 B 应因构造失败记 failed 并带原因: %+v", b)
	}
	if len(*calls) != 1 || (*calls)[0].appID != "cli_push_a" {
		t.Fatalf("构造失败的那条不应发出, calls=%+v", *calls)
	}
}

// TestSendCard_NoIdentityNotBuilt 验证收件人在某企业无身份时不调用 Build（不为不可达者构造卡片）。
func TestSendCard_NoIdentityNotBuilt(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
	}
	svc, _ := newService(t, twoCompanyConfig("x"), idx, nil)

	built := map[string]int{}
	res, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Build: func(app larkpushcap.PushApp, _ larkpushcap.Recipient) (string, error) {
			built[app.AppID]++
			return `{}`, nil
		},
	})
	if err != nil {
		t.Fatalf("SendCard: %v", err)
	}
	if built["cli_push_a"] != 1 || built["cli_push_b"] != 0 {
		t.Fatalf("只应为有身份的企业构造卡片, built=%+v", built)
	}
	for _, d := range res.Deliveries {
		if d.PushAppID == "cli_push_b" && d.Status != larkpushcap.DeliveryStatusNoIdentity {
			t.Fatalf("企业 B 应为 no_identity: %+v", d)
		}
	}
}

// TestSendCard_InvalidInput 验证缺构造函数或收件人时返回参数错误、不发送。
func TestSendCard_InvalidInput(t *testing.T) {
	svc, calls := newService(t, twoCompanyConfig("x"), nil, nil)
	build := func(larkpushcap.PushApp, larkpushcap.Recipient) (string, error) { return `{}`, nil }
	cases := []larkpushcap.SendCardRequest{
		{Group: testGroup, Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}},
		{Group: testGroup, Build: build},
		{Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Build: build},
	}
	for i, req := range cases {
		_, err := svc.SendCard(context.Background(), req)
		if !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
			t.Fatalf("case %d: want CodeInvalidParam, got %v", i, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("参数错误不应发送, calls=%+v", *calls)
	}
}

// TestSendCard_NoPushApp 验证无推送应用时返回 CodeNoPushApp。
func TestSendCard_NoPushApp(t *testing.T) {
	svc, _ := newService(t, map[string]any{}, nil, nil)
	_, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Build:      func(larkpushcap.PushApp, larkpushcap.Recipient) (string, error) { return `{}`, nil },
	})
	if !bizerr.Is(err, larkpushcode.CodeNoPushApp) {
		t.Fatalf("want CodeNoPushApp, got %v", err)
	}
}

// TestListPushApps 验证清单按配置给出推送应用的公开投影（含 identity_app_id，不含密钥）。
func TestListPushApps(t *testing.T) {
	svc, _ := newService(t, twoCompanyConfig("x"), nil, nil)

	apps, err := svc.ListPushApps(context.Background(), testGroup)
	if err != nil {
		t.Fatalf("ListPushApps: %v", err)
	}
	want := []larkpushcap.PushApp{
		{AppID: "cli_push_a", Name: "系统通知(霍尔果斯)", IdentityAppID: "cli_a"},
		{AppID: "cli_push_b", Name: "系统通知(星嘉)", IdentityAppID: "cli_b"},
	}
	if len(apps) != len(want) || apps[0] != want[0] || apps[1] != want[1] {
		t.Fatalf("want %+v, got %+v", want, apps)
	}
}

// TestListPushApps_ConfigInvalid 验证配置非法时返回 CodeConfigInvalid：含 app_id 重复，
// 以及「凭据待补」的空占位条目（Validate 要求每条 app_id / secret_key 非空）。
func TestListPushApps_ConfigInvalid(t *testing.T) {
	cases := map[string][]any{
		"app_id 重复": {
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "cli_push_a", "secret_key": "pb", "identity_app_id": "cli_b"},
		},
		"空占位": {
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "", "secret_key": "", "identity_app_id": "cli_b"},
		},
	}
	for name, items := range cases {
		svc, _ := newService(t, groupsConfig(map[string]any{testGroup: items}), nil, nil)
		if _, err := svc.ListPushApps(context.Background(), testGroup); !bizerr.Is(err, larkpushcode.CodeConfigInvalid) {
			t.Fatalf("%s: want CodeConfigInvalid, got %v", name, err)
		}
	}
}

// TestListPushApps_ByGroup 验证清单只返回请求分组的推送应用；分组未配置返回空切片，
// 分组为空返回参数错误。
func TestListPushApps_ByGroup(t *testing.T) {
	svc, _ := newService(t, multiGroupConfig(), nil, nil)

	apps, err := svc.ListPushApps(context.Background(), "linapro-attend-poc")
	if err != nil {
		t.Fatalf("ListPushApps: %v", err)
	}
	if len(apps) != 1 || apps[0].AppID != "cli_push_c" {
		t.Fatalf("应只返回考勤分组的推送应用, got %+v", apps)
	}
	if apps, err = svc.ListPushApps(context.Background(), "linapro-unknown"); err != nil || len(apps) != 0 {
		t.Fatalf("未配置的分组应返回空切片, got %+v err=%v", apps, err)
	}
	if _, err = svc.ListPushApps(context.Background(), " "); !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
		t.Fatalf("分组为空应返回 CodeInvalidParam, got %v", err)
	}
}

// TestSendCard_DryRunDoesNotCallLark 验证干跑只做寻址与构造、不调用飞书：可投递的明细记为
// addressable（且不算成功），无身份与构造失败照常记各自状态，供调用方在发出任何卡片前判定全员可达。
func TestSendCard_DryRunDoesNotCallLark(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001", "E1002": "u_a_1002"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}, {EmployeeNo: "E1002"}},
		Build: func(_ larkpushcap.PushApp, r larkpushcap.Recipient) (string, error) {
			if r.EmployeeNo == "E1002" {
				return "", errors.New("模板变量缺失")
			}
			return `{}`, nil
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("SendCard dry-run: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("干跑不得调用飞书, calls=%+v", *calls)
	}
	got := map[string]larkpushcap.DeliveryStatus{}
	for _, d := range res.Deliveries {
		if d.Succeeded() {
			t.Fatalf("干跑明细不得记为已送达: %+v", d)
		}
		got[d.PushAppID+"/"+d.EmployeeNo] = d.Status
	}
	want := map[string]larkpushcap.DeliveryStatus{
		"cli_push_a/E1001": larkpushcap.DeliveryStatusAddressable,
		"cli_push_a/E1002": larkpushcap.DeliveryStatusFailed,
		"cli_push_b/E1001": larkpushcap.DeliveryStatusNoIdentity,
		"cli_push_b/E1002": larkpushcap.DeliveryStatusNoIdentity,
	}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("%s want %s, got %s（全部明细 %+v）", k, w, got[k], res.Deliveries)
		}
	}
}

// TestSendCard_ChatBuilderSeesChatID 验证卡片构造函数对群聊收件人调用时能拿到 chat_id，
// 且卡片以 chat_id 发出。
func TestSendCard_ChatBuilderSeesChatID(t *testing.T) {
	svc, calls := newService(t, singleAppConfig(), nil, nil)
	var seen []larkpushcap.Recipient
	res, err := svc.SendCard(context.Background(), larkpushcap.SendCardRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{ChatID: "oc_ops"}},
		Build: func(_ larkpushcap.PushApp, r larkpushcap.Recipient) (string, error) {
			seen = append(seen, r)
			return `{"type":"template"}`, nil
		},
	})
	if err != nil || !res.AllSucceeded() {
		t.Fatalf("SendCard: res=%+v err=%v", res, err)
	}
	if len(seen) != 1 || seen[0].ChatID != "oc_ops" || seen[0].PushAppID != "cli_push_a" {
		t.Fatalf("构造函数收到的收件人不符: %+v", seen)
	}
	if len(*calls) != 1 || !(*calls)[0].card || (*calls)[0].receiveTyp != larkim.ReceiveIDTypeChatID {
		t.Fatalf("卡片应以 chat_id 发出, got %+v", *calls)
	}
}
