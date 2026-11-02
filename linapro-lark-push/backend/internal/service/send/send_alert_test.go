// send_alert_test.go 覆盖「部署缺陷与数据缺陷触发告警」的判定：只在该报警的场景发，
// 业务常态（工号不存在、单个 user_id 缺失、飞书侧单条失败）不发。

package send

import (
	"context"
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// alertKinds 收集事件种类，便于按种类断言。
func alertKinds(events []alert.Event) []alert.Kind {
	out := make([]alert.Kind, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}

// TestSendText_AlertsNoPushApp 验证分组没有可用推送应用时告警一次（整批发不出去）。
func TestSendText_AlertsNoPushApp(t *testing.T) {
	alerter, events := newAlerter()
	// 分组未配置 → requirePushApps 返回 CodeNoPushApp。
	svc, _ := newServiceWithAlerter(t, groupsConfig(map[string]any{}), nil, nil, alerter)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("分组无推送应用时应返回错误")
	}
	if len(*events) != 1 || (*events)[0].Kind != alert.KindNoPushApp {
		t.Fatalf("应告警 KindNoPushApp 一次, got %v", alertKinds(*events))
	}
	if (*events)[0].Group != testGroup {
		t.Fatalf("告警应带分组名, got %q", (*events)[0].Group)
	}
}

// TestSendText_AlertsConfigInvalid 验证分组配置非法时告警一次（含重复 app_id 这类结构错误）。
func TestSendText_AlertsConfigInvalid(t *testing.T) {
	alerter, events := newAlerter()
	cfg := groupsConfig(map[string]any{
		testGroup: []any{
			map[string]any{"app_id": "cli_dup", "secret_key": "p", "name": "A", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "cli_dup", "secret_key": "p", "name": "B", "identity_app_id": "cli_b"},
		},
	})
	svc, _ := newServiceWithAlerter(t, cfg, nil, nil, alerter)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("配置非法时应返回错误")
	}
	if len(*events) != 1 || (*events)[0].Kind != alert.KindConfigInvalid {
		t.Fatalf("应告警 KindConfigInvalid 一次, got %v", alertKinds(*events))
	}
}

// TestSendText_AlertsEmpcapUnbound 验证工号寻址依赖未绑定时告警一次。
func TestSendText_AlertsEmpcapUnbound(t *testing.T) {
	alerter, events := newAlerter()
	svc, err := New(
		&fakeHostConfig{values: twoCompanyConfig("x")},
		func(string, string) MessageSender {
			t.Fatal("寻址失败时不应构造发送器")
			return nil
		},
		func(context.Context, string) (empcap.LarkUserIDIndex, error) {
			return empcap.LarkUserIDIndex{}, empcap.ErrServiceUnbound
		},
		alerter,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("empcap 未绑定时应返回错误")
	}
	if len(*events) != 1 || (*events)[0].Kind != alert.KindEmpcapUnbound {
		t.Fatalf("应告警 KindEmpcapUnbound 一次, got %v", alertKinds(*events))
	}
}

// TestSendText_AlertsBlankUserID 验证某企业整表 user_id 为空时告警（部署缺陷：通常缺权限）。
func TestSendText_AlertsBlankUserID(t *testing.T) {
	alerter, events := newAlerter()
	idx := map[string]empcap.LarkUserIDIndex{
		// cli_a：一个人都投不出去，但空行集非空 → 判为部署缺陷。
		"cli_a": {ByEmployeeNo: map[string]string{}, BlankEmployeeNos: map[string]struct{}{"E1": {}}},
		// cli_b：正常。
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b"}},
	}
	svc, _ := newServiceWithAlerter(t, twoCompanyConfig("x"), idx, nil, alerter)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*events) != 1 || (*events)[0].Kind != alert.KindBlankUserID {
		t.Fatalf("应告警 KindBlankUserID 一次, got %v", alertKinds(*events))
	}
	if (*events)[0].PushAppID != "cli_push_a" {
		t.Fatalf("告警应指向缺 user_id 的推送应用, got %q", (*events)[0].PushAppID)
	}
}

// TestSendText_AlertsCollision 验证出现工号碰撞时告警（无法确定该发给哪个账号）。
func TestSendText_AlertsCollision(t *testing.T) {
	alerter, events := newAlerter()
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{}, CollidedEmployeeNos: map[string]struct{}{"E1": {}}},
	}
	svc, _ := newServiceWithAlerter(t, twoCompanyConfig("x"), idx, nil, alerter)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	var found bool
	for _, ev := range *events {
		if ev.Kind == alert.KindCollision {
			found = true
		}
	}
	if !found {
		t.Fatalf("应告警 KindCollision, got %v", alertKinds(*events))
	}
}

// TestSendText_NoAlertForBusinessFailures 验证业务常态不告警：单个 user_id 缺失、
// 某人某企业无身份、飞书侧单条投递失败都不进告警通道。
func TestSendText_NoAlertForBusinessFailures(t *testing.T) {
	alerter, events := newAlerter()
	idx := map[string]empcap.LarkUserIDIndex{
		// cli_a：E9 缺 user_id，但同企业仍有可投递的人 → 不是部署缺陷。
		"cli_a": {
			ByEmployeeNo:     map[string]string{"E1": "u_a"},
			BlankEmployeeNos: map[string]struct{}{"E9": {}},
		},
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b"}},
	}
	svc, _ := newServiceWithAlerter(t, twoCompanyConfig("x"), idx, nil, alerter)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group: testGroup,
		Recipients: []larkpushcap.Recipient{
			{EmployeeNo: "E1"},
			{EmployeeNo: "E9"},
			{EmployeeNo: "E_NOPE"},
		},
		Text: "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if res.AllSucceeded() {
		t.Fatal("E9 与 E_NOPE 应至少有失败明细")
	}
	if len(*events) != 0 {
		t.Fatalf("业务常态不应告警, got %v", alertKinds(*events))
	}
}

// TestSendText_NilAlerter 验证告警通道为 nil 时一切照常，不 panic。
func TestSendText_NilAlerter(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b"}},
	}
	svc, calls := newServiceWithAlerter(t, twoCompanyConfig("x"), idx, nil, nil)
	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if !res.AllSucceeded() || len(*calls) != 2 {
		t.Fatalf("无告警通道时投递应不受影响, got %+v", res.Deliveries)
	}
}

// TestSendText_GroupBoundAlertCarriesGroup 验证告警事件带上实际请求的分组，
// 使运维能定位到哪个调用方的配置坏了。
func TestSendText_GroupBoundAlertCarriesGroup(t *testing.T) {
	alerter, events := newAlerter()
	svc, _ := newServiceWithAlerter(t, groupsConfig(map[string]any{}), nil, nil, alerter)
	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      "other-plugin",
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("分组无推送应用时应返回错误")
	}
	if len(*events) != 1 || (*events)[0].Group != "other-plugin" {
		t.Fatalf("告警应带实际分组名, got %+v", *events)
	}
}
