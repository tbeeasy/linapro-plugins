// send_name_test.go 覆盖按姓名寻址的解析与投递：解析后按企业扇出、找不到 / 重名判失败、
// 离职不参与匹配、跨租户同人只发一次、与显式工号去重，以及员工能力未绑定的失败关闭。

package send

import (
	"context"
	"strconv"
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// activeEmp 构造一名在职员工行。
func activeEmp(empID int64, name, employeeNo string) *empcap.Employee {
	return &empcap.Employee{EmpID: empID, Name: name, EmployeeNo: employeeNo, Status: empcap.StatusActive}
}

// departedEmp 构造一名离职员工行。
func departedEmp(empID int64, name, employeeNo string) *empcap.Employee {
	return &empcap.Employee{EmpID: empID, Name: name, EmployeeNo: employeeNo, Status: empcap.StatusDeparted}
}

// failureFor 取出某个姓名对应的姓名解析失败明细（不存在时返回 nil）。
// 只认姓名解析阶段产生的失败项：它们尚未投递任何推送应用，所以 PushAppID 为空；
// 「某企业在无身份」的 no_identity 明细不属于解析失败，不在此列。
func failureFor(ds []larkpushcap.Delivery, name string) *larkpushcap.Delivery {
	for i := range ds {
		if ds[i].Name == name && ds[i].PushAppID == "" && !ds[i].Succeeded() {
			return &ds[i]
		}
	}
	return nil
}

// successCount 统计真正送达飞书的明细条数（服务层 AllSucceeded 会把「某企业无身份」也算失败，
// 用例断言「有没有发出去」时应看这个）。
func successCount(ds []larkpushcap.Delivery) int {
	n := 0
	for _, d := range ds {
		if d.Succeeded() {
			n++
		}
	}
	return n
}

// TestSendText_NameFanOutBothCompanies 验证姓名解析为工号后按企业扇出，两条消息都用推送应用凭据。
func TestSendText_NameFanOutBothCompanies(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1001": "u_a_1001"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1001": "u_b_1001"}},
	}
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {activeEmp(1, "张三", "E1001")}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "服务器 CPU 超过 90%",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(res.Deliveries) != 2 || !res.AllSucceeded() {
		t.Fatalf("两企业各发一条并全部成功, got %+v", res.Deliveries)
	}
	if len(*calls) != 2 {
		t.Fatalf("want 2 次发送, got %d", len(*calls))
	}
	for _, d := range res.Deliveries {
		if d.EmployeeNo != "E1001" || d.Name != "张三" {
			t.Fatalf("明细应同时带解析出的工号与原姓名: %+v", d)
		}
	}
}

// TestSendText_NameNotFound 验证查不到姓名时只让该条失败，同批其它收件人照常投递。
func TestSendText_NameNotFound(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, _ := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {activeEmp(1, "张三", "E1")}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}, {Name: "李四"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if res.AllSucceeded() {
		t.Fatal("有找不到的姓名时不应判全部成功")
	}
	f := failureFor(res.Deliveries, "李四")
	if f == nil {
		t.Fatalf("应有李四的失败项: %+v", res.Deliveries)
	}
	if f.Status != larkpushcap.DeliveryStatusNameNotFound {
		t.Fatalf("状态应为 name_not_found, got %s", f.Status)
	}
	if f.EmployeeNo != "" || f.PushAppID != "" {
		t.Fatalf("姓名解析失败项不应带工号或推送应用: %+v", f)
	}
	if failureFor(res.Deliveries, "张三") != nil {
		t.Fatal("张三应投递成功，不应出现在失败项")
	}
}

// TestSendText_NameDuplicate 验证同名两名在职员工时拒绝发送，不静默取其一。
func TestSendText_NameDuplicate(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1", "E2": "u_a_2"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {
			activeEmp(1, "张三", "E1"),
			activeEmp(2, "张三", "E2"),
		}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("重名不应发出任何消息, got %d 次", len(*calls))
	}
	f := failureFor(res.Deliveries, "张三")
	if f == nil || f.Status != larkpushcap.DeliveryStatusNameDuplicate {
		t.Fatalf("应有一条 name_duplicate 失败项: %+v", res.Deliveries)
	}
}

// TestSendText_NameIgnoresDeparted 验证离职的同名员工不参与判定，只在职的那名收消息。
func TestSendText_NameIgnoresDeparted(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1", "E9": "u_a_9"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {
			activeEmp(1, "张三", "E1"),
			departedEmp(9, "张三", "E9"),
		}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if successCount(res.Deliveries) != 1 || len(*calls) != 1 {
		t.Fatalf("应只发给在职的那名: deliveries=%+v calls=%d", res.Deliveries, len(*calls))
	}
	if (*calls)[0].receiveID != "u_a_1" {
		t.Fatalf("应发给在职员工的 user_id, got %q", (*calls)[0].receiveID)
	}
}

// TestSendText_NameCrossTenantSamePerson 验证同一自然人在两个租户各有一行（工号相同）时算一人。
func TestSendText_NameCrossTenantSamePerson(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {
			activeEmp(1, "张三", "E1"),
			activeEmp(2, "张三", "E1"), // 另一个租户的同一人，工号相同
		}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if successCount(res.Deliveries) != 1 || len(*calls) != 1 {
		t.Fatalf("跨租户同一人不应判重名: deliveries=%+v calls=%d", res.Deliveries, len(*calls))
	}
}

// TestSendText_NameAndEmployeeNoSamePerson 验证显式工号与解析到同一人的姓名只发一次。
func TestSendText_NameAndEmployeeNoSamePerson(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"), idx,
		map[string][]*empcap.Employee{"张三": {activeEmp(1, "张三", "E1")}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, {Name: "张三"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("同一人应只发一条, got %d", len(*calls))
	}
	if failureFor(res.Deliveries, "张三") != nil {
		t.Fatalf("姓名项应被显式工号覆盖、不产生解析失败: %+v", res.Deliveries)
	}
}

// TestSendText_NameWithoutEmployeeNo 验证姓名匹配到员工但该员工没有工号时记 failed（无法按工号寻址）。
func TestSendText_NameWithoutEmployeeNo(t *testing.T) {
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"),
		map[string]empcap.LarkUserIDIndex{"cli_a": {}, "cli_b": {}},
		map[string][]*empcap.Employee{"张三": {activeEmp(1, "张三", "")}}, nil)

	res, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "hi",
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("无工号不应发送, got %d", len(*calls))
	}
	f := failureFor(res.Deliveries, "张三")
	if f == nil || f.Status != larkpushcap.DeliveryStatusFailed {
		t.Fatalf("应有一条 failed 失败项: %+v", res.Deliveries)
	}
}

// TestSendText_NameMixedWithEmployeeNoRejected 验证同一收件人同时给出姓名与工号属于参数错误。
func TestSendText_NameMixedWithEmployeeNoRejected(t *testing.T) {
	svc, calls := newServiceWithNames(t, twoCompanyConfig("x"),
		map[string]empcap.LarkUserIDIndex{"cli_a": {}, "cli_b": {}}, nil, nil)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1", Name: "张三"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("姓名与工号混用应返回参数错误")
	}
	if len(*calls) != 0 {
		t.Fatalf("参数错误不应发送任何消息, got %d", len(*calls))
	}
}

// TestSendText_NameEmpcapUnbound 验证员工能力未绑定时告警一次并整批失败。
func TestSendText_NameEmpcapUnbound(t *testing.T) {
	alerter, events := newAlerter()
	svc, calls := buildService(t, twoCompanyConfig("x"),
		map[string]empcap.LarkUserIDIndex{"cli_a": {}, "cli_b": {}},
		func(context.Context, []string) (map[string][]*empcap.Employee, error) {
			return nil, empcap.ErrServiceUnbound
		}, nil, alerter)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{Name: "张三"}},
		Text:       "hi",
	}); err == nil {
		t.Fatal("员工能力未绑定时应返回错误")
	}
	if len(*calls) != 0 {
		t.Fatalf("能力未就绪不应发送, got %d", len(*calls))
	}
	if len(*events) != 1 || (*events)[0].Kind != alert.KindEmpcapUnbound {
		t.Fatalf("应告警 KindEmpcapUnbound 一次, got %v", alertKinds(*events))
	}
}

// TestSendText_NameBatchLookupOnce 验证一批姓名只发起一次解析调用（不随人数增加）。
func TestSendText_NameBatchLookupOnce(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{}}, "cli_b": {}}
	byName := make(map[string][]*empcap.Employee, 100)
	recipients := make([]larkpushcap.Recipient, 0, 100)
	for i := 0; i < 100; i++ {
		name := "员工" + string(rune('A'+i%26)) + "-" + strconv.Itoa(i)
		byName[name] = []*empcap.Employee{activeEmp(int64(i+1), name, "E"+strconv.Itoa(i))}
		recipients = append(recipients, larkpushcap.Recipient{Name: name})
	}
	var lookups int
	svc, _ := buildService(t, twoCompanyConfig("x"), idx,
		func(_ context.Context, names []string) (map[string][]*empcap.Employee, error) {
			lookups++
			out := make(map[string][]*empcap.Employee, len(names))
			for _, n := range names {
				if rows, ok := byName[n]; ok {
					out[n] = rows
				}
			}
			return out, nil
		}, nil, nil)

	if _, err := svc.SendText(context.Background(), larkpushcap.SendTextRequest{
		Group:      testGroup,
		Recipients: recipients,
		Text:       "hi",
	}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if lookups != 1 {
		t.Fatalf("100 个姓名应只查询一次, got %d", lookups)
	}
}
