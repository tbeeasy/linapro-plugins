// notify_test.go 覆盖 360 测评飞书推送编排的核心不变式（tasks 8.4 / 8.5）。
//
// 推送逻辑的硬红线（见 design.md D4 / spec「推送寻址」「一键发送编排」）：
//   - 投递只经 linapro-lark-push：本插件只给工号与卡片构造函数，不读任何飞书应用凭据；
//   - 一评价者一张卡：同一评价者 N 条评价关系只作为一个收件人（不按关系深链）；
//   - 卡片按推送应用构造：模板取该推送应用的 card_templates 项，链接 larkAppId 取
//     该推送应用所属企业的身份应用（员工端换票用身份应用）；
//   - 按问卷判定送达（FB-30 / FB-31）：全部问卷评价者并集只干跑一次（不调飞书），有评价者不可达的
//     问卷记入 Unreachable；只向干跑通过问卷的评价者并集正式投递（每人一张卡，只属于不可达问卷的
//     评价者不发卡），单条失败不中断，任一企业投递成功即视为触达，仍有评价者未送达的问卷记入
//     Failed（按工号粒度回报）；一名评价者未送达会使其所在的全部问卷失败；
//   - lark-push 无法完成本次请求（未就绪 / 配置非法）→ 返回错误，调用方不发布任何问卷。
//
// 测试不触库：直接调 senderImpl.pushEvaluators（已抽出为纯编排方法），
// 配合 fake Pusher / fake hostConfig 覆盖全部推送分支。
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-eval-360/backend/internal/notify/notifycode"
	"lina-plugin-linapro-eval-360/backend/internal/service/project"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
)

// fakeEmpLookup 是 EmpLookup 的测试替身：EmpID → 员工查询。
type fakeEmpLookup struct {
	byID map[int64]*empcap.Employee
}

func (f fakeEmpLookup) MapByEmpIDs(_ context.Context, empIDs []int64) (map[int64]*empcap.Employee, error) {
	out := make(map[int64]*empcap.Employee, len(empIDs))
	for _, id := range empIDs {
		if emp, ok := f.byID[id]; ok {
			out[id] = emp
		}
	}
	return out, nil
}

// pushedCard 是一次被 fakePusher 实际「发出」的卡片。
type pushedCard struct {
	pushAppID  string
	employeeNo string
	cardJSON   string
}

// fakePusher 是 Pusher 的测试替身，按 lark-push 的语义模拟「推送应用 × 收件人」扇出：
// 收件人在该推送应用对应企业有身份（reach）才调用 Build；干跑只记 addressable、不发出，
// 正式投递才计入 sent，failFor 命中即模拟飞书拒绝。sendErr 对干跑与正式投递都生效，
// realSendErr 只在正式投递时返回（模拟干跑通过后 lark-push 请求级失败）。
type fakePusher struct {
	apps        []larkpushcap.PushApp
	listErr     error
	sendErr     error
	realSendErr error
	reach       map[string]map[string]bool // 推送应用 app_id → 工号 → 有身份
	failFor     map[string]map[string]bool // 推送应用 app_id → 工号 → 发送失败
	reqs        []larkpushcap.SendCardRequest
	sent        []pushedCard
	// listGroups 记录 ListPushApps 收到的分组。
	listGroups []string
}

func (f *fakePusher) ListPushApps(_ context.Context, group string) ([]larkpushcap.PushApp, error) {
	f.listGroups = append(f.listGroups, group)
	return f.apps, f.listErr
}

func (f *fakePusher) SendCard(_ context.Context, req larkpushcap.SendCardRequest) (larkpushcap.SendResult, error) {
	f.reqs = append(f.reqs, req)
	if f.sendErr != nil {
		return larkpushcap.SendResult{}, f.sendErr
	}
	if !req.DryRun && f.realSendErr != nil {
		return larkpushcap.SendResult{}, f.realSendErr
	}
	var res larkpushcap.SendResult
	for _, app := range f.apps {
		for _, r := range req.Recipients {
			res.Deliveries = append(res.Deliveries, f.deliver(req, app, r))
		}
	}
	return res, nil
}

// deliver 模拟单条「推送应用 × 收件人」投递。
func (f *fakePusher) deliver(req larkpushcap.SendCardRequest, app larkpushcap.PushApp, r larkpushcap.Recipient) larkpushcap.Delivery {
	d := larkpushcap.Delivery{EmployeeNo: r.EmployeeNo, PushAppID: app.AppID}
	if !f.reach[app.AppID][r.EmployeeNo] {
		d.Status = larkpushcap.DeliveryStatusNoIdentity
		return d
	}
	cardJSON, err := req.Build(app, r)
	if err != nil {
		d.Status, d.Error = larkpushcap.DeliveryStatusFailed, err.Error()
		return d
	}
	if req.DryRun {
		d.Status = larkpushcap.DeliveryStatusAddressable
		return d
	}
	f.sent = append(f.sent, pushedCard{pushAppID: app.AppID, employeeNo: r.EmployeeNo, cardJSON: cardJSON})
	d.Status = larkpushcap.DeliveryStatusSent
	if f.failFor[app.AppID][r.EmployeeNo] {
		d.Status = larkpushcap.DeliveryStatusFailed
	}
	return d
}

// fakeHostConfig 满足 notify 需要的两个 hostconfigcap 方法子集（Get / String）。
// 它嵌入 hostconfigcap.Service 以保留其余方法的 nil 兜底，只覆盖 Get 与 String。
type fakeHostConfig struct {
	hostconfigcap.Service                      // 嵌入以满足接口其余方法（nil 兜底）
	values                map[string]*gvar.Var // key → 预设值
}

func (f fakeHostConfig) Get(_ context.Context, key string, _ any) (*gvar.Var, error) {
	if v, ok := f.values[key]; ok {
		return v, nil
	}
	return nil, nil
}

func (f fakeHostConfig) String(ctx context.Context, key, def string) (string, error) {
	v, err := f.Get(ctx, key, def)
	if err != nil || v == nil {
		return def, err
	}
	return v.String(), nil
}

// twoPushApps 是两企业各一个推送应用（identity_app_id 指向各企业的身份应用）。
var twoPushApps = []larkpushcap.PushApp{
	{AppID: "cli_push_a", Name: "系统通知(霍尔果斯)", IdentityAppID: "cli_a"},
	{AppID: "cli_push_b", Name: "系统通知(星嘉)", IdentityAppID: "cli_b"},
}

// newSenderForTest 构造注入 fake Pusher 的 senderImpl。
func newSenderForTest(emp EmpLookup, hc fakeHostConfig, pusher *fakePusher) *senderImpl {
	return New(emp, hc, pusher).(*senderImpl)
}

// ev 是构造 evaluator 的简写。
func ev(empID int64, no, name string) evaluator {
	return evaluator{empID: empID, employeeNo: no, name: name}
}

// qe 是构造一份问卷及其评价者的简写。
func qe(questionnaireID int64, evaluators ...evaluator) questionnaireEvaluators {
	return questionnaireEvaluators{questionnaireID: questionnaireID, evaluators: evaluators}
}

// sentNos 返回 fakePusher 正式发出卡片的工号集合。
func sentNos(pusher *fakePusher) map[string]int {
	out := map[string]int{}
	for _, sc := range pusher.sent {
		out[sc.employeeNo]++
	}
	return out
}

// assertOutcome 断言推送结果的 Unreachable / Failed 与期望完全一致（nil 与空映射视为相同）。
func assertOutcome(t *testing.T, got project.PushOutcome, wantUnreachable, wantFailed map[int64][]string) {
	t.Helper()
	if !sameLabels(got.Unreachable, wantUnreachable) {
		t.Fatalf("Unreachable want %v, got %v", wantUnreachable, got.Unreachable)
	}
	if !sameLabels(got.Failed, wantFailed) {
		t.Fatalf("Failed want %v, got %v", wantFailed, got.Failed)
	}
}

// sameLabels 比较两个「问卷 id → 标签」映射（nil 与空映射视为相同）。
func sameLabels(a, b map[int64][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if strings.Join(av, ",") != strings.Join(b[k], ",") {
			return false
		}
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// testCard 是测试用卡片模板配置（history 路由链接）。模板按推送应用配置：各推送应用一套
// （模板 ID 不同，模拟跨飞书企业），未列出的推送应用视为未配置模板。
var testCard = cardTemplate{
	todoURL: "https://emp.example.com/eval-360/emp/todo",
	appTemplates: map[string]cardTemplateEntry{
		"cli_push_a": {templateID: "AAqyjwfhabcef", versionName: "1.0.0"},
		"cli_push_b": {templateID: "BBqyjwfhabceg"},
	},
}

// sentCard 是模板卡片 content 的解码结构，供断言模板 ID 与变量。
type sentCard struct {
	Type string `json:"type"`
	Data struct {
		TemplateID          string            `json:"template_id"`
		TemplateVersionName string            `json:"template_version_name"`
		TemplateVariable    map[string]string `json:"template_variable"`
	} `json:"data"`
}

// decodeCard 解码一次发送的卡片 content。
func decodeCard(t *testing.T, cardJSON string) sentCard {
	t.Helper()
	var c sentCard
	if err := json.Unmarshal([]byte(cardJSON), &c); err != nil {
		t.Fatalf("卡片 content 非法: %v（%s）", err, cardJSON)
	}
	if c.Type != "template" {
		t.Fatalf("卡片 type want template, got %q", c.Type)
	}
	return c
}

// bizerrParams 断言 err 为指定业务错误码，并返回其 count / list 参数。
func bizerrParams(t *testing.T, err error, code *bizerr.Code) (int, string) {
	t.Helper()
	if !bizerr.Is(err, code) {
		t.Fatalf("want %s, got %v", code.RuntimeCode(), err)
	}
	e, _ := bizerr.As(err)
	return gvar.New(e.Params()["count"]).Int(), gvar.New(e.Params()["list"]).String()
}

// bizerrItem 取业务错误的 item 参数。
func bizerrItem(t *testing.T, err error) string {
	t.Helper()
	e, ok := bizerr.As(err)
	if !ok {
		t.Fatalf("不是业务错误: %v", err)
	}
	return gvar.New(e.Params()["item"]).String()
}

// --- 用例 ------------------------------------------------------------------

// TestPushEvaluators_DryRunThenSend: 评价者以工号作为收件人交给 lark-push，一人一个收件人；
// 先干跑（DryRun=true、不发出）再正式投递，全员送达时结果为空（问卷通过）。每张卡用该推送应用的
// 模板、链接 larkAppId 取该企业身份应用（不是推送应用 ID）、name 为评价者姓名。
func TestPushEvaluators_DryRunThenSend(t *testing.T) {
	pusher := &fakePusher{
		apps: twoPushApps,
		reach: map[string]map[string]bool{
			"cli_push_a": {"E001": true, "E002": true},
			"cli_push_b": {"E001": true},
		},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 42,
		[]questionnaireEvaluators{qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四"))}, testCard)
	if err != nil {
		t.Fatalf("全员送达不应返回错误, got %v", err)
	}
	assertOutcome(t, outcome, nil, nil)
	if len(pusher.reqs) != 2 || !pusher.reqs[0].DryRun || pusher.reqs[1].DryRun {
		t.Fatalf("应先干跑一次、再正式投递一次, got %d 次 %+v", len(pusher.reqs), pusher.reqs)
	}
	req := pusher.reqs[1]
	if len(req.Recipients) != 2 || req.Recipients[0].EmployeeNo != "E001" || req.Recipients[1].EmployeeNo != "E002" {
		t.Fatalf("收件人应为评价者工号, got %+v", req.Recipients)
	}
	if req.Group != "linapro-eval-360" {
		t.Fatalf("应以本插件分组投递（独立测评机器人）, got %q", req.Group)
	}
	if req.BizSource != bizSource || req.BizRef != "42" {
		t.Fatalf("日志对账字段 want %s/42, got %s/%s", bizSource, req.BizSource, req.BizRef)
	}
	if len(pusher.sent) != 3 {
		t.Fatalf("E001 两企业各一张（双发可接受）+ E002 一张, got %+v", pusher.sent)
	}
	identityByPushApp := map[string]string{"cli_push_a": "cli_a", "cli_push_b": "cli_b"}
	nameByNo := map[string]string{"E001": "张三", "E002": "李四"}
	for _, sc := range pusher.sent {
		got := decodeCard(t, sc.cardJSON)
		wantTpl := testCard.appTemplates[sc.pushAppID]
		if got.Data.TemplateID != wantTpl.templateID || got.Data.TemplateVersionName != wantTpl.versionName {
			t.Fatalf("%s 模板 ID/版本不符: %+v", sc.pushAppID, got.Data)
		}
		identity := identityByPushApp[sc.pushAppID]
		wantURL := wrapLarkAppLink("https://emp.example.com/eval-360/emp/todo?larkAppId=" + identity)
		if got.Data.TemplateVariable[cardVarEvaluationURL] != wantURL {
			t.Fatalf("%s 链接应携带身份应用 %s: want %s, got %s",
				sc.pushAppID, identity, wantURL, got.Data.TemplateVariable[cardVarEvaluationURL])
		}
		if got.Data.TemplateVariable[cardVarName] != nameByNo[sc.employeeNo] {
			t.Fatalf("%s/%s name want %s, got %s", sc.pushAppID, sc.employeeNo,
				nameByNo[sc.employeeNo], got.Data.TemplateVariable[cardVarName])
		}
	}
}

// TestPushEvaluators_UnreachableBlocksQuestionnaire: 单份问卷干跑发现有评价者在任何企业都没有身份时
// 记入 Unreachable 并点名其工号，且不进行正式投递（可达者也一张卡都不发）。
func TestPushEvaluators_UnreachableBlocksQuestionnaire(t *testing.T) {
	pusher := &fakePusher{
		apps:  twoPushApps,
		reach: map[string]map[string]bool{"cli_push_a": {"E001": true}},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1,
		[]questionnaireEvaluators{qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四"))}, testCard)
	if err != nil {
		t.Fatalf("不可达属于问卷级失败、不应返回请求级错误, got %v", err)
	}
	assertOutcome(t, outcome, map[int64][]string{10: {"E002"}}, nil)
	if len(pusher.reqs) != 1 || len(pusher.sent) != 0 {
		t.Fatalf("唯一问卷不可达时只干跑、不发出任何卡片, reqs=%d sent=%+v", len(pusher.reqs), pusher.sent)
	}
}

// TestPushEvaluators_SharedEvaluatorOneCard: 两份问卷共享评价者 E002 时干跑与正式投递都只把他作为
// 一个收件人（每人一张卡），全员送达则两份问卷都通过。
func TestPushEvaluators_SharedEvaluatorOneCard(t *testing.T) {
	pusher := &fakePusher{
		apps:  twoPushApps[:1],
		reach: map[string]map[string]bool{"cli_push_a": {"E001": true, "E002": true, "E003": true}},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1, []questionnaireEvaluators{
		qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四")),
		qe(11, ev(2, "E002", "李四"), ev(3, "E003", "王五")),
	}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, nil, nil)
	if len(pusher.reqs) != 2 {
		t.Fatalf("多份问卷也只干跑一次、正式投递一次, got %d", len(pusher.reqs))
	}
	for _, req := range pusher.reqs {
		if len(req.Recipients) != 3 {
			t.Fatalf("收件人应为三人的去重并集, got %+v", req.Recipients)
		}
	}
	if got := sentNos(pusher); got["E001"] != 1 || got["E002"] != 1 || got["E003"] != 1 {
		t.Fatalf("每人一张卡（共享评价者不重复发）, got %v", got)
	}
}

// TestPushEvaluators_UnreachableOnlyAffectsItsQuestionnaire: 问卷 11 有不可达评价者 → 只有它记入
// Unreachable；只属于问卷 11 的可达评价者 E004 不发卡，问卷 10 的评价者照常发卡并通过；同时属于
// 失败问卷 11 与通过问卷 10 的共享评价者 E002 照常发卡（待办里只会出现已发布的问卷 10）。
func TestPushEvaluators_UnreachableOnlyAffectsItsQuestionnaire(t *testing.T) {
	pusher := &fakePusher{
		apps:  twoPushApps[:1],
		reach: map[string]map[string]bool{"cli_push_a": {"E001": true, "E002": true, "E004": true}},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1, []questionnaireEvaluators{
		qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四")),
		qe(11, ev(2, "E002", "李四"), ev(4, "E004", "赵六"), ev(5, "E005", "孙七")),
	}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, map[int64][]string{11: {"E005"}}, nil)
	if !pusher.reqs[0].DryRun || len(pusher.reqs[0].Recipients) != 4 {
		t.Fatalf("应对四名评价者的并集只干跑一次, got %+v", pusher.reqs[0])
	}
	got := sentNos(pusher)
	if got["E001"] != 1 || got["E002"] != 1 {
		t.Fatalf("通过问卷的评价者（含与失败问卷共享的 E002）应各收到一张卡, got %v", got)
	}
	if got["E004"] != 0 || got["E005"] != 0 {
		t.Fatalf("只属于不可达问卷的评价者不应发卡, got %v", got)
	}
}

// TestPushEvaluators_SharedEvaluatorFailureFailsAllItsQuestionnaires: 两份问卷都干跑通过，共享评价者
// E002 正式投递失败 → 两份问卷都记入 Failed（均点名 E002），不共享的问卷 12 照常通过。
func TestPushEvaluators_SharedEvaluatorFailureFailsAllItsQuestionnaires(t *testing.T) {
	pusher := &fakePusher{
		apps:    twoPushApps[:1],
		reach:   map[string]map[string]bool{"cli_push_a": {"E001": true, "E002": true, "E003": true}},
		failFor: map[string]map[string]bool{"cli_push_a": {"E002": true}},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1, []questionnaireEvaluators{
		qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四")),
		qe(11, ev(2, "E002", "李四")),
		qe(12, ev(3, "E003", "王五")),
	}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, nil, map[int64][]string{10: {"E002"}, 11: {"E002"}})
}

// TestPushEvaluators_NoEvaluatorsPassesDirectly: 没有待评评价者的问卷直接通过；全部问卷都没有
// 评价者时不调用 lark-push。
func TestPushEvaluators_NoEvaluatorsPassesDirectly(t *testing.T) {
	pusher := &fakePusher{apps: twoPushApps}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1,
		[]questionnaireEvaluators{qe(10), qe(11)}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, nil, nil)
	if len(pusher.reqs) != 0 {
		t.Fatalf("无评价者时不应调用 SendCard, got %d", len(pusher.reqs))
	}
}

// TestPushEvaluators_ReachedIfAnyAppSucceeds: 正式投递中某企业失败不影响其余；评价者只要有一个
// 企业投递成功即视为已触达，全部企业都失败才计入未送达，问卷记入 Failed。
func TestPushEvaluators_ReachedIfAnyAppSucceeds(t *testing.T) {
	pusher := &fakePusher{
		apps: twoPushApps,
		reach: map[string]map[string]bool{
			"cli_push_a": {"E001": true, "E002": true},
			"cli_push_b": {"E001": true},
		},
		failFor: map[string]map[string]bool{
			"cli_push_a": {"E001": true, "E002": true},
		},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1,
		[]questionnaireEvaluators{qe(10, ev(1, "E001", "张三"), ev(2, "E002", "李四"))}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(pusher.sent) != 3 {
		t.Fatalf("失败不中断：三次投递都应发起, got %d", len(pusher.sent))
	}
	assertOutcome(t, outcome, nil, map[int64][]string{10: {"E002"}})
}

// TestPushEvaluators_AppWithoutTemplateUnreachable: 推送应用未配卡片模板时干跑阶段卡片构造失败，
// 其评价者若也没被其它企业覆盖 → 判为不可达，不发出任何卡片（不静默当作已推送）。
func TestPushEvaluators_AppWithoutTemplateUnreachable(t *testing.T) {
	pusher := &fakePusher{
		apps:  []larkpushcap.PushApp{{AppID: "cli_push_x", IdentityAppID: "cli_x"}},
		reach: map[string]map[string]bool{"cli_push_x": {"E001": true}},
	}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)

	outcome, err := s.pushEvaluators(context.Background(), 1,
		[]questionnaireEvaluators{qe(10, ev(1, "E001", "张三"))}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, map[int64][]string{10: {"E001"}}, nil)
	if len(pusher.sent) != 0 {
		t.Fatalf("未配置模板的推送应用不应发出卡片, got %+v", pusher.sent)
	}
}

// TestPushEvaluators_PushErrorReturned: lark-push 无法完成本次请求时返回错误（调用方据此不发布任何
// 问卷）：未就绪 / 未注入映射为 CodeLarkPushUnbound，其余请求级错误（如配置非法）原样透传；
// 干跑通过后正式投递请求级失败同样返回错误、不给出部分结果。
func TestPushEvaluators_PushErrorReturned(t *testing.T) {
	groups := []questionnaireEvaluators{qe(10, ev(1, "E001", "张三"))}
	unbound := map[string]Pusher{
		"lark-push 未就绪": &fakePusher{apps: twoPushApps, sendErr: larkpushcap.ErrSenderUnbound},
		"lark-push 未注入": nil,
	}
	for name, pusher := range unbound {
		s := New(fakeEmpLookup{}, fakeHostConfig{}, pusher).(*senderImpl)
		if _, err := s.pushEvaluators(context.Background(), 1, groups, testCard); !bizerr.Is(err, notifycode.CodeLarkPushUnbound) {
			t.Fatalf("%s: want CodeLarkPushUnbound, got %v", name, err)
		}
	}
	cfgErr := errors.New("lark-push 配置非法")
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, &fakePusher{apps: twoPushApps, sendErr: cfgErr})
	if _, err := s.pushEvaluators(context.Background(), 1, groups, testCard); !errors.Is(err, cfgErr) {
		t.Fatalf("配置非法应透传, got %v", err)
	}

	realErr := errors.New("lark-push 正式投递请求失败")
	pusher := &fakePusher{
		apps:        twoPushApps,
		reach:       map[string]map[string]bool{"cli_push_a": {"E001": true}},
		realSendErr: realErr,
	}
	s = newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)
	if _, err := s.pushEvaluators(context.Background(), 1, groups, testCard); !errors.Is(err, realErr) {
		t.Fatalf("干跑通过后正式投递请求级失败应返回错误, got %v", err)
	}
}

// TestPushEvaluators_LabelFallbackToEmpID: 工号为空的评价者不作为收件人投递，判为不可达时
// 回退 EmpID 字符串；没有可投递收件人时不调用 lark-push。
func TestPushEvaluators_LabelFallbackToEmpID(t *testing.T) {
	pusher := &fakePusher{apps: twoPushApps}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)
	outcome, err := s.pushEvaluators(context.Background(), 1,
		[]questionnaireEvaluators{qe(10, evaluator{empID: 7})}, testCard)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	assertOutcome(t, outcome, map[int64][]string{10: {"7"}}, nil)
	if len(pusher.reqs) != 0 {
		t.Fatalf("无可投递收件人时不应调用 SendCard, got %d", len(pusher.reqs))
	}
}

// TestUnionEmpIDs: 评价者并集按问卷顺序、问卷内顺序去重（一次批量解析，无 N+1）。
func TestUnionEmpIDs(t *testing.T) {
	got := unionEmpIDs([]int64{11, 10, 12}, map[int64][]int64{10: {1, 2}, 11: {2, 3}})
	want := []int64{2, 3, 1}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// TestSendAll_EmptyQuestionnairesSkips: 无待发布问卷时不推送（已发布跳过、幂等）。
func TestSendAll_EmptyQuestionnairesSkips(t *testing.T) {
	pusher := &fakePusher{}
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{}, pusher)
	outcome, err := s.SendAll(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("empty questionnaires should not error, got %v", err)
	}
	assertOutcome(t, outcome, nil, nil)
	if len(pusher.reqs) != 0 {
		t.Fatalf("empty questionnaires should not push, reqs=%d", len(pusher.reqs))
	}
}

// TestSendAll_EmpcapUnboundFailsClosed: emp 未注入时 fail-closed 返回请求级错误。
func TestSendAll_EmpcapUnboundFailsClosed(t *testing.T) {
	s := New(nil, fakeHostConfig{}, &fakePusher{}).(*senderImpl)
	_, err := s.SendAll(context.Background(), 1, []int64{10})
	if !bizerr.Is(err, notifycode.CodeEmpcapUnbound) {
		t.Fatalf("empcap unbound should fail-closed with CodeEmpcapUnbound, got %v", err)
	}
}

// TestWrapLarkAppLink 覆盖 AppLink 包装：协议地址与独立窗口模式固定，hash 路由链接中的
// #、?、& 整体编码进 url 参数（不被当作 AppLink 自身的分隔符），解码后与原链接完全一致。
func TestWrapLarkAppLink(t *testing.T) {
	target := "https://h.example.com/admin/#/eval-360/emp/todo?larkAppId=cli_a&y=2"
	got := wrapLarkAppLink(target)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("AppLink 不可解析: %v", err)
	}
	if base := u.Scheme + "://" + u.Host + u.Path; base != larkWebURLAppLink {
		t.Fatalf("协议地址 want %s, got %s", larkWebURLAppLink, base)
	}
	if u.Fragment != "" {
		t.Fatalf("目标链接的 # 未编码，被解析为 AppLink 片段: %s", u.Fragment)
	}
	query := u.Query()
	if query.Get("mode") != appLinkModeWindow {
		t.Fatalf("mode want %s, got %s", appLinkModeWindow, query.Get("mode"))
	}
	if query.Get("url") != target {
		t.Fatalf("url 往返不一致: want %s, got %s", target, query.Get("url"))
	}
	if len(query) != 2 {
		t.Fatalf("目标链接的参数泄漏到 AppLink 参数: %v", query)
	}
}

// TestWithLarkAppID 覆盖链接参数拼接：history 路由写 query、hash 路由写进 fragment 内的
// 路由 query（vue-router hash 模式只读 fragment 内参数）、已有参数被保留、同名参数被覆盖。
func TestWithLarkAppID(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"history", "https://h.example.com/admin/eval-360/emp/todo", "https://h.example.com/admin/eval-360/emp/todo?larkAppId=cli_a"},
		{"history 保留已有参数", "https://h.example.com/todo?x=1", "https://h.example.com/todo?larkAppId=cli_a&x=1"},
		{"hash", "https://h.example.com/admin/#/eval-360/emp/todo", "https://h.example.com/admin/#/eval-360/emp/todo?larkAppId=cli_a"},
		{"hash 覆盖同名参数", "https://h.example.com/admin/#/eval-360/emp/todo?larkAppId=old&y=2", "https://h.example.com/admin/#/eval-360/emp/todo?larkAppId=cli_a&y=2"},
	}
	for _, c := range cases {
		got, err := withLarkAppID(c.raw, "cli_a")
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: want %s, got %s", c.name, c.want, got)
		}
	}
	if _, err := withLarkAppID("/eval-360/emp/todo", "cli_a"); err == nil {
		t.Fatal("相对路径链接应被拒绝（卡片链接须为完整 URL）")
	}
}

// TestPrecheck 覆盖发布前的推送配置校验：待办链接缺失/非完整 URL、lark-push 无推送应用、
// 或某个推送应用在 card_templates 中缺模板时，返回 CodeCardConfigMissing；配置齐备时放行。
//
// 重点覆盖「卡片模板按推送应用分别配置」：两个飞书企业各有推送应用，模板须授权给实际发送
// 的应用、跨应用不可复用，故没有全局兜底——任一推送应用缺模板都必须在发布前拦截（发布后
// 再补配置不会重推，该企业员工将永远收不到卡片）。
func TestPrecheck(t *testing.T) {
	full := map[string]any{
		todoH5URLConfigKey: "https://h.example.com/admin/#/eval-360/emp/todo",
		cardTemplatesConfigKey: `[{"app_id":"cli_push_a","template_id":"tA","template_version":"1.0.0"},` +
			`{"app_id":"cli_push_b","template_id":"tB"}]`,
	}
	cases := []struct {
		name     string
		drop     string
		override map[string]any
		apps     []larkpushcap.PushApp
		wantItem string // 期望 CodeCardConfigMissing 的 item；空串表示放行
	}{
		{name: "配置完整", apps: twoPushApps},
		{name: "缺待办链接", drop: todoH5URLConfigKey, apps: twoPushApps, wantItem: todoH5URLConfigKey},
		{
			name:     "某推送应用缺模板",
			override: map[string]any{cardTemplatesConfigKey: `[{"app_id":"cli_push_a","template_id":"tA"}]`},
			apps:     twoPushApps,
			wantItem: cardTemplatesConfigKey + "[app_id=cli_push_b]",
		},
		{
			name:     "模板按身份应用配置不算数",
			override: map[string]any{cardTemplatesConfigKey: `[{"app_id":"cli_a","template_id":"tA"},{"app_id":"cli_b","template_id":"tB"}]`},
			apps:     twoPushApps,
			wantItem: cardTemplatesConfigKey + "[app_id=cli_push_a]",
		},
		{
			name:     "条目缺 template_id 视为未配置",
			override: map[string]any{cardTemplatesConfigKey: `[{"app_id":"cli_push_a","template_id":"tA"},{"app_id":"cli_push_b","template_id":""}]`},
			apps:     twoPushApps,
			wantItem: cardTemplatesConfigKey + "[app_id=cli_push_b]",
		},
		{name: "完全未配置模板", drop: cardTemplatesConfigKey, apps: twoPushApps, wantItem: cardTemplatesConfigKey + "[app_id=cli_push_a]"},
		{name: "lark-push 本插件分组无推送应用", apps: nil, wantItem: "plugin.linapro-lark-push.push_groups.linapro-eval-360"},
		{name: "链接非完整 URL", override: map[string]any{todoH5URLConfigKey: "/eval-360/emp/todo"}, apps: twoPushApps, wantItem: todoH5URLConfigKey},
	}
	for _, c := range cases {
		values := map[string]*gvar.Var{}
		for k, v := range full {
			if k != c.drop {
				values[k] = gvar.New(v)
			}
		}
		for k, v := range c.override {
			values[k] = gvar.New(v)
		}
		pusher := &fakePusher{apps: c.apps}
		s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{values: values}, pusher)
		err := s.Precheck(context.Background())
		if len(pusher.listGroups) > 0 && pusher.listGroups[0] != "linapro-eval-360" {
			t.Fatalf("%s: 预检应按本插件分组列推送应用, got %v", c.name, pusher.listGroups)
		}
		if c.wantItem == "" {
			if err != nil {
				t.Fatalf("%s: want nil, got %v", c.name, err)
			}
			continue
		}
		if !bizerr.Is(err, notifycode.CodeCardConfigMissing) {
			t.Fatalf("%s: want CodeCardConfigMissing, got %v", c.name, err)
		}
		if got := bizerrItem(t, err); got != c.wantItem {
			t.Fatalf("%s: item want %s, got %s", c.name, c.wantItem, got)
		}
	}
}

// TestPrecheck_LarkPushUnavailable 覆盖 lark-push 不可用时的发布前拦截：未就绪 / 未注入映射为
// CodeLarkPushUnbound；lark-push 自身配置非法等错误原样透传（不被吞成放行）。
func TestPrecheck_LarkPushUnavailable(t *testing.T) {
	values := map[string]*gvar.Var{
		todoH5URLConfigKey:     gvar.New("https://h.example.com/admin/#/eval-360/emp/todo"),
		cardTemplatesConfigKey: gvar.New(`[{"app_id":"cli_push_a","template_id":"tA"}]`),
	}
	for name, pusher := range map[string]Pusher{
		"未就绪": &fakePusher{listErr: larkpushcap.ErrSenderUnbound},
		"未注入": nil,
	} {
		s := New(fakeEmpLookup{}, fakeHostConfig{values: values}, pusher).(*senderImpl)
		if err := s.Precheck(context.Background()); !bizerr.Is(err, notifycode.CodeLarkPushUnbound) {
			t.Fatalf("%s: want CodeLarkPushUnbound, got %v", name, err)
		}
	}

	cfgErr := errors.New("飞书配置非法：push_groups.linapro-eval-360[1].app_id: app_id 不能为空")
	s := newSenderForTest(fakeEmpLookup{}, fakeHostConfig{values: values}, &fakePusher{listErr: cfgErr})
	if err := s.Precheck(context.Background()); !errors.Is(err, cfgErr) {
		t.Fatalf("lark-push 配置错误应原样透传, got %v", err)
	}
}
