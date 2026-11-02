// send_text_test.go 覆盖文本投递的参数校验、按企业扇出、三态寻址判定、失败隔离与分组约束。

package send

import (
	"context"
	"errors"
	"strings"
	"testing"

	"linapro-lark-sdk/larkim"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

func TestSendText_FanOutBothCompanies(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "服务器 CPU 超过 90%",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(res.Deliveries) != 2 {
		t.Fatalf("两企业各发一条，want 2 条明细, got %d: %+v", len(res.Deliveries), res.Deliveries)
	}
	if !res.AllSucceeded() {
		t.Fatalf("应全部成功: %+v", res.Deliveries)
	}
	if len(*calls) != 2 {
		t.Fatalf("want 2 次发送, got %d", len(*calls))
	}
	// 凭据交叉正确：appA 用 a 企业的 user_id，appB 用 b 企业的。
	byApp := map[string]string{}
	for _, c := range *calls {
		byApp[c.appID] = c.receiveID
		if c.receiveTyp != larkim.ReceiveIDTypeUserID {
			t.Fatalf("应以 user_id 投递, got %q", c.receiveTyp)
		}
	}
	if byApp["cli_push_a"] != "u_a_1001" || byApp["cli_push_b"] != "u_b_1001" {
		t.Fatalf("凭据与 user_id 交叉错误: %+v", byApp)
	}
}

// TestSendText_SingleCompanyIdentity 验证只在 A 企业有身份时只发一条，B 企业不计失败以外的错误。

func TestSendText_SingleCompanyIdentity(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("只应发 1 条, got %d", len(*calls))
	}
	if len(res.Deliveries) != 2 {
		t.Fatalf("明细应含两企业的结果, got %d", len(res.Deliveries))
	}
	var noIdentity, sent int
	for _, d := range res.Deliveries {
		switch d.Status {
		case larkpushcap.DeliveryStatusSent:
			sent++
		case larkpushcap.DeliveryStatusNoIdentity:
			noIdentity++
		}
	}
	if sent != 1 || noIdentity != 1 {
		t.Fatalf("want 1 sent + 1 no_identity, got %+v", res.Deliveries)
	}
}

// TestSendText_BlankUserID 验证本人在某企业有身份但 user_id 为空时，该企业判为 user_id 缺失，
// 并汇总各企业缺 user_id 的员工数。

func TestSendText_BlankUserID(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{}, BlankEmployeeNos: map[string]struct{}{"E1001": {}, "E2": {}, "E3": {}}},
	}
	svc, _ := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if res.BlankUserIDEmployees != 3 {
		t.Fatalf("应汇总缺 user_id 的员工数 3, got %d", res.BlankUserIDEmployees)
	}
	var missing int
	for _, d := range res.Deliveries {
		if d.Status == larkpushcap.DeliveryStatusUserIDMissing {
			missing++
		}
	}
	if missing != 1 {
		t.Fatalf("应有一条 user_id_missing, got %+v", res.Deliveries)
	}
}

// TestSendText_CollisionNotSent 验证碰撞工号不发送并标记 collision。

func TestSendText_CollisionNotSent(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {
			ByEmployeeNo:        map[string]string{},
			CollidedEmployeeNos: map[string]struct{}{"E1001": {}},
		},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	// A 企业碰撞不发、B 企业照发。
	if len(*calls) != 1 || (*calls)[0].appID != "cli_push_b" {
		t.Fatalf("A 企业碰撞不应发送, got %+v", *calls)
	}
	var collision int
	for _, d := range res.Deliveries {
		if d.Status == larkpushcap.DeliveryStatusCollision {
			collision++
		}
	}
	if collision != 1 {
		t.Fatalf("应有一条 collision, got %+v", res.Deliveries)
	}
	if res.AllSucceeded() {
		t.Fatal("存在碰撞时不应判为全部成功")
	}
}

// TestSendText_OthersBlankUserIDDoesNotAffectRecipient 是 FB-14 的回归用例：E1001 只在 A 企业有身份，
// B 企业有其他员工缺 user_id。修复前 B 企业按「企业里有空白行」把 E1001 判为 user_id_missing，对外接口
// 把已在 A 收到消息的人列为失败项、调用方重试即重复投递；修复后按本人判定为 no_identity。

func TestSendText_OthersBlankUserIDDoesNotAffectRecipient(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E2": "u_b_2"}, BlankEmployeeNos: map[string]struct{}{"E3": {}}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].appID != "cli_push_a" {
		t.Fatalf("只应经 A 企业发送一条, got %+v", *calls)
	}
	for _, d := range res.Deliveries {
		if d.PushAppID == "cli_push_b" && d.Status != larkpushcap.DeliveryStatusNoIdentity {
			t.Fatalf("B 企业应判 no_identity（他人缺 user_id 不影响本人）, got %s", d.Status)
		}
	}
}

// TestSendText_SingleFailureDoesNotAbort 验证单条失败不中断其余收件人。

func TestSendText_SingleFailureDoesNotAbort(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u1", "E2": "u2"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, map[string]error{
		"u1": &larkim.FeishuError{Kind: "文本", Code: 230002, Msg: "not in availability", ReqID: "req-1"},
	})

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, {EmployeeNo: "E2"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("单条失败不应让整个请求报错: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("两条都应尝试发送, got %d", len(*calls))
	}
	if res.AllSucceeded() {
		t.Fatal("存在失败条时不应判为全部成功")
	}
	var failed *larkpushcap.Delivery
	for i := range res.Deliveries {
		if res.Deliveries[i].Status == larkpushcap.DeliveryStatusFailed {
			failed = &res.Deliveries[i]
		}
	}
	if failed == nil {
		t.Fatalf("应有失败明细: %+v", res.Deliveries)
	}
	if failed.FeishuCode != 230002 || failed.FeishuReqID != "req-1" {
		t.Fatalf("失败明细应带飞书 code/req_id: %+v", failed)
	}
}

// TestSendText_UserIDRecipient 验证纯 user_id 寻址跳过查表直接投递。

func TestSendText_UserIDRecipient(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{UserID: "u_direct"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("两个推送应用各发一条（user_id 按企业签发，按应用遍历）, got %d", len(*calls))
	}
	for _, d := range res.Deliveries {
		if d.UserID != "u_direct" {
			t.Fatalf("user_id 寻址应保留原标识: %+v", d)
		}
	}
}

// TestSendText_InvalidInput 验证空收件人、空文本、超长文本在发送前被拦下。

func TestSendText_InvalidInput(t *testing.T) {
	svc, calls := newService(t, twoCompanyConfig("x"), nil, nil)
	cases := []struct {
		name string
		req  larkpushcap.SendTextRequest
	}{
		{name: "收件人为空", req: larkpushcap.SendTextRequest{Group: testGroup, Text: "hi"}},
		{name: "收件人无寻址键", req: larkpushcap.SendTextRequest{
			Group:      testGroup,
			Recipients: []larkpushcap.Recipient{{}}, Text: "hi"}},
		{name: "文本为空", req: larkpushcap.SendTextRequest{
			Group:      testGroup,
			Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "  "}},
		{name: "文本超长", req: larkpushcap.SendTextRequest{
			Group:      testGroup,
			Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
			Text:       strings.Repeat("字", larkim.MaxTextRunes+1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.SendText(context.Background(), c.req); err == nil {
				t.Fatal("非法输入应返回错误")
			}
		})
	}
	if len(*calls) != 0 {
		t.Fatalf("非法输入不应发起发送, got %d", len(*calls))
	}
}

// TestSendText_NoPushApp 验证没有可用推送应用时返回明确错误。

func TestSendText_NoPushApp(t *testing.T) {
	cfg := groupsConfig(map[string]any{testGroup: []any{}})
	svc, calls := newService(t, cfg, nil, nil)
	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
	}); err == nil {
		t.Fatal("无推送应用时应返回错误")
	}
	if len(*calls) != 0 {
		t.Fatalf("无推送应用不应发送, got %d", len(*calls))
	}
}

// TestSendText_ConfigInvalid 验证配置非法（同一身份应用被两个推送应用引用）时拒绝发送。

func TestSendText_ConfigInvalid(t *testing.T) {
	cfg := groupsConfig(map[string]any{testGroup: []any{
		map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "identity_app_id": "cli_a"},
		map[string]any{"app_id": "cli_push_b", "secret_key": "pb", "identity_app_id": "cli_a"},
	}})
	svc, calls := newService(t, cfg, nil, nil)
	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
	}); err == nil {
		t.Fatal("配置非法时应返回错误")
	}
	if len(*calls) != 0 {
		t.Fatalf("配置非法不应发送, got %d", len(*calls))
	}
}

// TestSendText_DuplicateRecipientsCollapsed 验证重复收件人只发一次。

func TestSendText_DuplicateRecipientsCollapsed(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u1"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)
	_, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, {EmployeeNo: "E1"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("重复收件人应折叠, got %d 次发送", len(*calls))
	}
}

// TestSendText_IndexError 验证取索引失败时整体返回错误（寻址是全员共用前置条件）。
func TestSendText_IndexError(t *testing.T) {
	svc, err := New(&fakeHostConfig{values: twoCompanyConfig("x")},
		func(string, string) MessageSender {
			t.Fatal("索引失败时不应构造发送器")
			return nil
		},
		func(context.Context, string) (empcap.LarkUserIDIndex, error) {
			return empcap.LarkUserIDIndex{}, errors.New("db down")
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
	}); err == nil {
		t.Fatal("索引失败应返回错误")
	}
}

// multiGroupConfig 返回两个分组的配置：testGroup 两企业各一个推送应用，
// linapro-attend-poc 只有企业 A 的考勤推送应用 cli_push_c。

func TestSendText_OnlyRequestGroupApps(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newService(t, multiGroupConfig(), idx, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      "linapro-attend-poc",
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1001"}},
		Text:       "今日未打卡",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].appID != "cli_push_c" || (*calls)[0].receiveID != "u_a_1001" {
		t.Fatalf("应只经考勤分组的 cli_push_c 投递, got %+v", *calls)
	}
	if len(res.Deliveries) != 1 || res.Deliveries[0].PushAppID != "cli_push_c" {
		t.Fatalf("明细应只含考勤分组的推送应用, got %+v", res.Deliveries)
	}
}

// TestSendText_GroupRequired 验证分组为空时返回参数错误、不回退为全部推送应用。

func TestSendText_GroupRequired(t *testing.T) {
	svc, calls := newService(t, multiGroupConfig(), nil, nil)
	for _, group := range []string{"", "   "} {
		_, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
			Group:      group,
			Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
			Text:       "hi",
		})
		if !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
			t.Fatalf("分组 %q: want CodeInvalidParam, got %v", group, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("分组为空不应发送, got %+v", *calls)
	}
}

// TestSendText_GroupNotConfigured 验证分组未配置时返回 CodeNoPushApp，绝不改用其它分组。

func TestSendText_GroupNotConfigured(t *testing.T) {
	svc, calls := newService(t, multiGroupConfig(), nil, nil)
	_, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      "linapro-unknown",
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		Text:       "hi",
	})
	if !bizerr.Is(err, larkpushcode.CodeNoPushApp) {
		t.Fatalf("want CodeNoPushApp, got %v", err)
	}
	if !strings.Contains(err.Error(), "linapro-unknown") {
		t.Fatalf("错误应带分组名, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("分组未配置不应回退发送, got %+v", *calls)
	}
}

// TestSendText_OtherGroupInvalidDoesNotBlock 验证一个分组非法只影响该分组：考勤分组含空
// secret_key 时考勤请求被拒，测评分组照常投递。

func TestSendText_OtherGroupInvalidDoesNotBlock(t *testing.T) {
	cfg := groupsConfig(map[string]any{
		testGroup: []any{
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "identity_app_id": "cli_a"},
		},
		"linapro-attend-poc": []any{
			map[string]any{"app_id": "cli_push_c", "secret_key": "", "identity_app_id": "cli_a"},
		},
	})
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{"E1": "u1"}}}
	svc, calls := newService(t, cfg, idx, nil)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group: "linapro-attend-poc", Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
	}); !bizerr.Is(err, larkpushcode.CodeConfigInvalid) {
		t.Fatalf("非法分组应返回 CodeConfigInvalid, got %v", err)
	}
	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group: testGroup, Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
	})
	if err != nil || !res.AllSucceeded() {
		t.Fatalf("合法分组应照常投递: res=%+v err=%v", res, err)
	}
	if len(*calls) != 1 || (*calls)[0].appID != "cli_push_a" {
		t.Fatalf("只应由测评分组发出一条, got %+v", *calls)
	}
}

// TestSendText_SameAppAcrossGroups 验证同一推送应用出现在多个分组是合法配置，两个分组都能用它投递。

func TestSendText_SameAppAcrossGroups(t *testing.T) {
	shared := map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "identity_app_id": "cli_a"}
	cfg := groupsConfig(map[string]any{
		testGroup:            []any{shared},
		"linapro-attend-poc": []any{shared},
	})
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{"E1": "u1"}}}
	svc, calls := newService(t, cfg, idx, nil)

	for _, group := range []string{testGroup, "linapro-attend-poc"} {
		res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
			Group: group, Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}}, Text: "hi",
		})
		if err != nil || !res.AllSucceeded() {
			t.Fatalf("分组 %s 应可用共享推送应用投递: res=%+v err=%v", group, res, err)
		}
	}
	if len(*calls) != 2 {
		t.Fatalf("两个分组各发一条, got %+v", *calls)
	}
}

// singleAppConfig 返回 testGroup 分组只有一个推送应用（cli_push_a）的配置。

func TestSendText_ChatSingleAppDefault(t *testing.T) {
	calls := &[]sentCall{}
	svc, err := New(
		&fakeHostConfig{values: singleAppConfig()},
		func(appID, _ string) MessageSender {
			return &fakeSender{appID: appID, calls: calls}
		},
		func(context.Context, string) (empcap.LarkUserIDIndex, error) {
			t.Fatal("纯群聊请求不应查询寻址索引")
			return empcap.LarkUserIDIndex{}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{ChatID: " oc_ops "}},
		Text:       "产线告警",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(res.Deliveries) != 1 || !res.AllSucceeded() {
		t.Fatalf("want 1 条成功明细, got %+v", res.Deliveries)
	}
	d := res.Deliveries[0]
	if d.ChatID != "oc_ops" || d.PushAppID != "cli_push_a" || d.UserID != "" {
		t.Fatalf("群聊明细字段不符: %+v", d)
	}
	if len(*calls) != 1 || (*calls)[0].receiveTyp != larkim.ReceiveIDTypeChatID || (*calls)[0].receiveID != "oc_ops" {
		t.Fatalf("应以 chat_id 发送一次, got %+v", *calls)
	}
}

// TestSendText_ChatPushAppValidation 验证群聊选应用的参数校验：多应用分组必须指定 push_app_id、
// push_app_id 必须属于本分组；任一不合法都整体拒绝，同请求的个人收件人也不发送。

func TestSendText_ChatPushAppValidation(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}}}
	cases := map[string]larkpushcap.Recipient{
		"多应用缺 push_app_id":   {ChatID: "oc_1"},
		"push_app_id 不属于本分组": {ChatID: "oc_1", PushAppID: "cli_push_x"},
	}
	for name, chat := range cases {
		t.Run(name, func(t *testing.T) {
			svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)
			_, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
				Group:      testGroup,
				Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, chat},
				Text:       "hi",
			})
			if !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
				t.Fatalf("want CodeInvalidParam, got %v", err)
			}
			if len(*calls) != 0 {
				t.Fatalf("参数错误不应发出任何消息, got %+v", *calls)
			}
		})
	}
}

// TestSendText_RecipientFieldConflicts 验证群聊与人的字段不能混用，push_app_id 不能用于个人收件人。

func TestSendText_RecipientFieldConflicts(t *testing.T) {
	svc, calls := newService(t, twoCompanyConfig("x"), nil, nil)
	for i, r := range []larkpushcap.Recipient{
		{EmployeeNo: "E1", ChatID: "oc_1"},
		{UserID: "u_1", ChatID: "oc_1"},
		{EmployeeNo: "E1", PushAppID: "cli_push_a"},
		{PushAppID: "cli_push_a"},
	} {
		_, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
			Group: testGroup, Recipients: []larkpushcap.Recipient{r}, Text: "hi",
		})
		if !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
			t.Fatalf("case %d: want CodeInvalidParam, got %v", i, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("参数错误不应发出任何消息, got %+v", *calls)
	}
}

// TestSendText_MixedPersonAndChat 验证人与群聊混发：工号按两企业扇出，群聊只经指定应用投递一次。

func TestSendText_MixedPersonAndChat(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b_1"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)
	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group: testGroup,
		Recipients: []larkpushcap.Recipient{
			{EmployeeNo: "E1"},
			{ChatID: "oc_ops", PushAppID: "cli_push_b"},
			{ChatID: "oc_ops", PushAppID: "cli_push_b"}, // 重复群聊只发一次
		},
		Text: "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(res.Deliveries) != 3 || !res.AllSucceeded() {
		t.Fatalf("want 3 条成功明细（两企业各 1 + 群 1）, got %+v", res.Deliveries)
	}
	var chatCalls []sentCall
	for _, c := range *calls {
		if c.receiveTyp == larkim.ReceiveIDTypeChatID {
			chatCalls = append(chatCalls, c)
		}
	}
	if len(chatCalls) != 1 || chatCalls[0].appID != "cli_push_b" {
		t.Fatalf("群聊应只经 cli_push_b 发一次, got %+v", chatCalls)
	}
}

// TestSendText_ChatRejectedByFeishu 验证机器人不在群内等飞书拒绝只记该条为 failed。

func TestSendText_ChatRejectedByFeishu(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}}}
	fail := map[string]error{"oc_x": &larkim.FeishuError{Code: 232011, Msg: "bot not in chat", ReqID: "r1"}}
	svc, _ := newService(t, singleAppConfig(), idx, fail)
	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, {ChatID: "oc_x"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	var chat, person larkpushcap.Delivery
	for _, d := range res.Deliveries {
		if d.ChatID != "" {
			chat = d
		} else {
			person = d
		}
	}
	if chat.Status != larkpushcap.DeliveryStatusFailed || chat.FeishuCode != 232011 {
		t.Fatalf("群聊应 failed 且带飞书错误码, got %+v", chat)
	}
	if person.Status != larkpushcap.DeliveryStatusSent {
		t.Fatalf("个人收件人应照常发送, got %+v", person)
	}
}

func multiGroupConfig() map[string]any {
	return groupsConfig(map[string]any{
		testGroup: []any{
			map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "360测评(霍尔果斯)", "identity_app_id": "cli_a"},
			map[string]any{"app_id": "cli_push_b", "secret_key": "pb", "name": "360测评(星嘉)", "identity_app_id": "cli_b"},
		},
		"linapro-attend-poc": []any{
			map[string]any{"app_id": "cli_push_c", "secret_key": "pc", "name": "考勤助手(霍尔果斯)", "identity_app_id": "cli_a"},
		},
	})
}

// TestSendText_OnlyRequestGroupApps 验证只用请求分组的推送应用投递：考勤分组的请求
// 只经 cli_push_c 发出，测评分组的推送应用不参与（不同调用方用不同机器人）。

func singleAppConfig() map[string]any {
	return groupsConfig(map[string]any{testGroup: []any{
		map[string]any{"app_id": "cli_push_a", "secret_key": "pa", "name": "系统通知(霍尔果斯)", "identity_app_id": "cli_a"},
	}})
}

// TestSendText_ChatSingleAppDefault 验证分组只有一个推送应用时群聊可省略 push_app_id，
// 以 chat_id 投递一次，且纯群聊请求不查寻址索引（不依赖 empcap）。
