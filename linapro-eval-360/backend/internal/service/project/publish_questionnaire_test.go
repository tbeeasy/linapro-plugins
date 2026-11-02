// publish_questionnaire_test.go 覆盖「单份问卷发布 + 推送」编排（FB-21、FB-30、FB-31），以及一键发送
// 校验通过后的按问卷发布编排 publishEach（FB-31）。
//
// FB-21：单份发布原先只翻转发布状态、从不推送，评价者收不到卡片。
// FB-30：推送失败时问卷仍被置为已发布。需求为「全部评价者送达才算发布成功」，故编排改为
// 校验 → 推送 → 全员送达后才写发布状态：
//  1. 全员送达 → 先推送、后写发布状态；
//  2. 推送失败（不可达 / 未送达）→ 返回对应业务错误，不写发布状态（原缺陷复现场景）；
//  3. 已发布 → 幂等，不重推、不重写；
//  4. 推送配置预检失败 → 不校验、不推送、不发布；
//  5. 发布校验失败 → 不推送、不发布；
//  6. 无推送能力（sender=nil）→ 仅发布。
//
// FB-31：一键发送按问卷判定，部分通过时只发布通过的问卷、失败问卷随结果回报；全部失败不发布、
// 不返回错误；请求级推送错误返回错误、不发布任何问卷。SendAll 开头的空问卷检查直接访问数据库，
// 故以其校验后的编排 publishEach 为测试入口。
//
// 问卷服务与 Sender 均为桩，不依赖数据库；两者共用事件日志以断言调用顺序。
package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/notify/notifycode"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// 事件日志中的调用标记。
const (
	eventValidate = "validate"
	eventPush     = "push"
	eventMark     = "mark"
)

// fakeQuestionnaire 只实现发布相关方法，其余方法经内嵌 nil 接口（调用即 panic，暴露误用）。
type fakeQuestionnaire struct {
	qsvc.Service
	events      *[]string
	projectID   int64
	published   bool  // 问卷当前是否已发布
	validateErr error // 发布校验失败时返回
	markedIDs   []int64
}

func (f *fakeQuestionnaire) ValidatePublish(_ context.Context, _ int64) (int64, bool, error) {
	*f.events = append(*f.events, eventValidate)
	if f.published {
		return f.projectID, false, nil
	}
	if f.validateErr != nil {
		return 0, false, f.validateErr
	}
	return f.projectID, true, nil
}

func (f *fakeQuestionnaire) MarkPublished(_ context.Context, ids []int64) error {
	*f.events = append(*f.events, eventMark)
	f.markedIDs = ids
	f.published = true
	return nil
}

// fakeSender 记录推送调用；outcome 模拟按问卷的推送失败，sendErr 模拟请求级推送错误。
type fakeSender struct {
	events      *[]string
	precheckErr error
	outcome     PushOutcome
	sendErr     error
	gotProject  int64
	gotIDs      []int64
}

func (f *fakeSender) Precheck(context.Context) error { return f.precheckErr }

func (f *fakeSender) SendAll(_ context.Context, projectID int64, ids []int64) (PushOutcome, error) {
	*f.events = append(*f.events, eventPush)
	f.gotProject, f.gotIDs = projectID, ids
	if f.sendErr != nil {
		return PushOutcome{}, f.sendErr
	}
	return f.outcome, nil
}

// bizerrCountList 断言 err 为指定业务错误码，并返回其 count / list 参数。
func bizerrCountList(t *testing.T, err error, code *bizerr.Code) (int, string) {
	t.Helper()
	if !bizerr.Is(err, code) {
		t.Fatalf("want %s, got %v", code.RuntimeCode(), err)
	}
	e, _ := bizerr.As(err)
	return gvar.New(e.Params()["count"]).Int(), gvar.New(e.Params()["list"]).String()
}

// newFakes 构造共用同一事件日志的问卷桩与推送桩。
func newFakes() (*fakeQuestionnaire, *fakeSender, *[]string) {
	events := &[]string{}
	return &fakeQuestionnaire{events: events, projectID: 7}, &fakeSender{events: events}, events
}

func TestPublishQuestionnaire_PushesThenPublishes(t *testing.T) {
	q, sender, events := newFakes()
	res, err := New(q, sender).PublishQuestionnaire(context.Background(), 42)
	if err != nil {
		t.Fatalf("全员送达应发布成功：%v", err)
	}
	if want := []string{eventValidate, eventPush, eventMark}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("应先校验、再推送、全员送达后才写发布状态，want %v got %v", want, *events)
	}
	if sender.gotProject != 7 || !reflect.DeepEqual(sender.gotIDs, []int64{42}) {
		t.Fatalf("应按本问卷推送（project=7, ids=[42]），实际 project=%d ids=%v", sender.gotProject, sender.gotIDs)
	}
	if !reflect.DeepEqual(q.markedIDs, []int64{42}) || !reflect.DeepEqual(res.PublishedIDs, []int64{42}) {
		t.Fatalf("应发布本问卷并回传其 id，实际 marked=%v res=%+v", q.markedIDs, res)
	}
}

// TestPublishQuestionnaire_PushFailedKeepsUnpublished 复现 FB-30：推送有评价者未送达时，
// 原实现已先写发布状态；修复后须返回错误且问卷保持未发布。FB-31 起推送失败按问卷回报，
// 单份发布把本问卷的失败转为原有业务错误：未送达 → CodePushFailed、不可达 →
// CodeEvaluatorsUnreachable（参数 count / list），请求级错误原样返回；均不写发布状态。
func TestPublishQuestionnaire_PushFailedKeepsUnpublished(t *testing.T) {
	cases := []struct {
		name     string
		outcome  PushOutcome
		sendErr  error
		wantCode *bizerr.Code
	}{
		{name: "未送达", outcome: PushOutcome{Failed: map[int64][]string{42: {"E001", "E002"}}}, wantCode: notifycode.CodePushFailed},
		{name: "不可达", outcome: PushOutcome{Unreachable: map[int64][]string{42: {"E001", "E002"}}}, wantCode: notifycode.CodeEvaluatorsUnreachable},
		{name: "请求级错误", sendErr: errors.New("lark-push 配置非法")},
	}
	for _, c := range cases {
		q, sender, events := newFakes()
		sender.outcome, sender.sendErr = c.outcome, c.sendErr
		res, err := New(q, sender).PublishQuestionnaire(context.Background(), 42)
		if res != nil {
			t.Fatalf("%s: 推送失败不应有结果，实际 %+v", c.name, res)
		}
		if c.wantCode == nil {
			if !errors.Is(err, c.sendErr) {
				t.Fatalf("%s: 应原样返回请求级错误，实际 %v", c.name, err)
			}
		} else if count, list := bizerrCountList(t, err, c.wantCode); count != 2 || list != "E001, E002" {
			t.Fatalf("%s: want count=2 list=%q, got count=%d list=%q", c.name, "E001, E002", count, list)
		}
		if want := []string{eventValidate, eventPush}; q.published || !reflect.DeepEqual(*events, want) {
			t.Fatalf("%s: 推送失败不得写发布状态，want events %v，实际 published=%v events=%v", c.name, want, q.published, *events)
		}
	}
}

func TestPublishQuestionnaire_AlreadyPublishedNoRepush(t *testing.T) {
	q, sender, events := newFakes()
	q.published = true
	res, err := New(q, sender).PublishQuestionnaire(context.Background(), 42)
	if err != nil {
		t.Fatalf("已发布应幂等成功：%v", err)
	}
	if want := []string{eventValidate}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("已发布问卷不得重推、不得重写，want %v got %v", want, *events)
	}
	if len(res.PublishedIDs) != 0 {
		t.Fatalf("已发布应返回空结果，实际 %+v", res)
	}
}

func TestPublishQuestionnaire_PrecheckFailBlocksAll(t *testing.T) {
	q, sender, events := newFakes()
	precheckErr := errors.New("card config missing")
	sender.precheckErr = precheckErr
	if _, err := New(q, sender).PublishQuestionnaire(context.Background(), 42); !errors.Is(err, precheckErr) {
		t.Fatalf("应返回预检错误，实际 %v", err)
	}
	if len(*events) != 0 || q.published {
		t.Fatalf("推送配置缺失时不得校验、推送或发布，实际 events=%v published=%v", *events, q.published)
	}
}

func TestPublishQuestionnaire_ValidateFailNoPush(t *testing.T) {
	q, sender, events := newFakes()
	validateErr := errors.New("weight invalid")
	q.validateErr = validateErr
	if _, err := New(q, sender).PublishQuestionnaire(context.Background(), 42); !errors.Is(err, validateErr) {
		t.Fatalf("应返回发布校验错误，实际 %v", err)
	}
	if want := []string{eventValidate}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("校验失败不得推送或发布，want %v got %v", want, *events)
	}
}

func TestPublishQuestionnaire_NilSenderOnlyPublishes(t *testing.T) {
	q, _, events := newFakes()
	res, err := New(q, nil).PublishQuestionnaire(context.Background(), 42)
	if err != nil {
		t.Fatalf("无推送能力时应仅发布成功：%v", err)
	}
	if want := []string{eventValidate, eventMark}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("无推送能力时应校验后直接发布，want %v got %v", want, *events)
	}
	if !q.published || !reflect.DeepEqual(res.PublishedIDs, []int64{42}) {
		t.Fatalf("应完成发布，实际 published=%v res=%+v", q.published, res)
	}
}

// publishEachTitles 是 publishEach 用例的问卷名。
var publishEachTitles = map[int64]string{10: "问卷 A", 11: "问卷 B", 12: "问卷 C"}

// TestPublishEach_PartialPublishesPassedOnly: 一键发送部分通过时只把通过的问卷写为已发布，
// 失败问卷按待发布顺序带上问卷名、原因与失败工号回报，且不返回错误。
func TestPublishEach_PartialPublishesPassedOnly(t *testing.T) {
	q, sender, events := newFakes()
	sender.outcome = PushOutcome{
		Unreachable: map[int64][]string{12: {"E009"}},
		Failed:      map[int64][]string{10: {"E002", "E003"}},
	}
	res, err := New(q, sender).(*serviceImpl).publishEach(context.Background(), 7, []int64{10, 11, 12}, publishEachTitles)
	if err != nil {
		t.Fatalf("部分失败不应返回错误：%v", err)
	}
	if want := []string{eventPush, eventMark}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("应先推送、后写发布状态，want %v got %v", want, *events)
	}
	if !reflect.DeepEqual(sender.gotIDs, []int64{10, 11, 12}) {
		t.Fatalf("应对全部待发布问卷推送，实际 %v", sender.gotIDs)
	}
	if !reflect.DeepEqual(q.markedIDs, []int64{11}) || !reflect.DeepEqual(res.PublishedIDs, []int64{11}) {
		t.Fatalf("只应发布通过的问卷 11，实际 marked=%v published=%v", q.markedIDs, res.PublishedIDs)
	}
	want := []*SendFailure{
		{QuestionnaireID: 10, Title: "问卷 A", Reason: FailReasonPushFailed, EmployeeNos: []string{"E002", "E003"}},
		{QuestionnaireID: 12, Title: "问卷 C", Reason: FailReasonUnreachable, EmployeeNos: []string{"E009"}},
	}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Fatalf("失败清单不符，want %+v got %+v", want, res.Failed)
	}
}

// TestPublishEach_AllFailedNoPublishNoError: 全部问卷推送失败时不写发布状态、不返回错误，
// PublishedIDs 为空切片（非 nil，序列化为 []）。
func TestPublishEach_AllFailedNoPublishNoError(t *testing.T) {
	q, sender, _ := newFakes()
	sender.outcome = PushOutcome{Unreachable: map[int64][]string{10: {"E001"}, 11: {"E001"}}}
	res, err := New(q, sender).(*serviceImpl).publishEach(context.Background(), 7, []int64{10, 11}, publishEachTitles)
	if err != nil {
		t.Fatalf("全部失败也应正常返回：%v", err)
	}
	if q.published || len(q.markedIDs) != 0 {
		t.Fatalf("全部失败不得发布任何问卷，实际 marked=%v", q.markedIDs)
	}
	if res.PublishedIDs == nil || len(res.PublishedIDs) != 0 {
		t.Fatalf("PublishedIDs 应为空切片，实际 %#v", res.PublishedIDs)
	}
	if len(res.Failed) != 2 || res.Failed[0].QuestionnaireID != 10 || res.Failed[1].QuestionnaireID != 11 {
		t.Fatalf("两份问卷都应列入失败清单，实际 %+v", res.Failed)
	}
}

// TestPublishEach_RequestErrorNoPublish: 请求级推送错误原样返回，不写任何发布状态。
func TestPublishEach_RequestErrorNoPublish(t *testing.T) {
	q, sender, events := newFakes()
	pushErr := errors.New("lark-push 未就绪")
	sender.sendErr = pushErr
	res, err := New(q, sender).(*serviceImpl).publishEach(context.Background(), 7, []int64{10, 11}, publishEachTitles)
	if !errors.Is(err, pushErr) || res != nil {
		t.Fatalf("应返回请求级错误且无结果，实际 res=%+v err=%v", res, err)
	}
	if want := []string{eventPush}; !reflect.DeepEqual(*events, want) {
		t.Fatalf("请求级错误不得写发布状态，want %v got %v", want, *events)
	}
}

// TestPublishEach_AllPassed: 全部通过时一次性发布全部问卷，失败清单为空切片。
func TestPublishEach_AllPassed(t *testing.T) {
	q, sender, _ := newFakes()
	res, err := New(q, sender).(*serviceImpl).publishEach(context.Background(), 7, []int64{10, 11}, publishEachTitles)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !reflect.DeepEqual(q.markedIDs, []int64{10, 11}) || !reflect.DeepEqual(res.PublishedIDs, []int64{10, 11}) {
		t.Fatalf("应一次性发布全部问卷，实际 marked=%v res=%+v", q.markedIDs, res)
	}
	if res.Failed == nil || len(res.Failed) != 0 {
		t.Fatalf("Failed 应为空切片，实际 %#v", res.Failed)
	}
}
