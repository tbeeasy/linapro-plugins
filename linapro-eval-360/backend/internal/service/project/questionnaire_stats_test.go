// questionnaire_stats_test.go 覆盖项目详情问卷卡片的人数与关系数聚合（FB-3 评价人数）。
//
// 场景：问卷 A 有 2 名被评估人、3 条评价关系，其中同一评价者评价了两人 → 评价人数为 2（去重），
// 被评估人数 2、应填 3、已填 1；无关系的问卷 B 各项均为 0。分页接口与项目详情两条入口口径一致。
//
// 需要真实 PostgreSQL（testkit.Setup）；数据直接经 DAO 写入，不依赖员工服务。
package project

import (
	"context"
	"fmt"
	"testing"
	"time"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
	"lina-plugin-linapro-eval-360/backend/internal/service/relation"
	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"
)

// statsFixture 是聚合测试的种子数据。
type statsFixture struct {
	projectID int64
	withRels  int64 // 有评价关系的问卷
	empty     int64 // 无评价关系的问卷
}

// seedStatsFixture 建项目、两份问卷，并给其中一份写入被评估人与评价关系；清理由 t.Cleanup 完成。
func seedStatsFixture(t *testing.T, ctx context.Context) statsFixture {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	projectID, err := dao.Project.Ctx(ctx).Data(do.Project{
		Name:        "FB3 评价人数 " + suffix,
		Description: "集成测试",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}
	f := statsFixture{projectID: projectID}
	t.Cleanup(func() { cleanupStatsFixture(t, ctx, f) })

	if f.withRels, err = dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId: projectID, Title: "有关系问卷", Sort: 0,
	}).InsertAndGetId(); err != nil {
		t.Fatalf("建问卷 A 失败：%v", err)
	}
	if f.empty, err = dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId: projectID, Title: "空问卷", Sort: 1,
	}).InsertAndGetId(); err != nil {
		t.Fatalf("建问卷 B 失败：%v", err)
	}
	roleID, err := dao.Role.Ctx(ctx).Data(do.Role{
		QuestionnaireId: f.withRels, Name: "同事",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建角色失败：%v", err)
	}

	// 被评估人 EmpID 101、102；评价者 201 评两人、202 只评 101。
	evaluateeIDs := make(map[int64]int64, 2)
	for _, empID := range []int64{101, 102} {
		id, iErr := dao.Evaluatee.Ctx(ctx).Data(do.Evaluatee{
			ProjectId: projectID, QuestionnaireId: f.withRels, EvaluateeEmpId: empID,
		}).InsertAndGetId()
		if iErr != nil {
			t.Fatalf("建被评估人 %d 失败：%v", empID, iErr)
		}
		evaluateeIDs[empID] = id
	}
	rels := []struct {
		evaluatee, evaluator int64
		completed            int
	}{
		{evaluatee: 101, evaluator: 201, completed: 1},
		{evaluatee: 102, evaluator: 201},
		{evaluatee: 101, evaluator: 202},
	}
	for _, r := range rels {
		if _, iErr := dao.Relation.Ctx(ctx).Data(do.Relation{
			ProjectId:       projectID,
			QuestionnaireId: f.withRels,
			EvaluateeId:     evaluateeIDs[r.evaluatee],
			EvaluatorEmpId:  r.evaluator,
			RoleId:          roleID,
			Source:          relation.SourceManual,
			Completed:       r.completed,
		}).Insert(); iErr != nil {
			t.Fatalf("建评价关系 %d→%d 失败：%v", r.evaluator, r.evaluatee, iErr)
		}
	}
	return f
}

// cleanupStatsFixture 删除种子数据；子表先删、项目最后删。
func cleanupStatsFixture(t *testing.T, ctx context.Context, f statsFixture) {
	t.Helper()
	if _, err := dao.Relation.Ctx(ctx).Where(dao.Relation.Columns().ProjectId, f.projectID).Delete(); err != nil {
		t.Errorf("清理评价关系失败：%v", err)
	}
	if _, err := dao.Evaluatee.Ctx(ctx).Where(dao.Evaluatee.Columns().ProjectId, f.projectID).Delete(); err != nil {
		t.Errorf("清理被评估人失败：%v", err)
	}
	if _, err := dao.Role.Ctx(ctx).WhereIn(dao.Role.Columns().QuestionnaireId, []int64{f.withRels, f.empty}).Delete(); err != nil {
		t.Errorf("清理角色失败：%v", err)
	}
	if _, err := dao.Questionnaire.Ctx(ctx).Where(dao.Questionnaire.Columns().ProjectId, f.projectID).Delete(); err != nil {
		t.Errorf("清理问卷失败：%v", err)
	}
	if _, err := dao.Project.Ctx(ctx).Where(dao.Project.Columns().Id, f.projectID).Delete(); err != nil {
		t.Errorf("清理项目失败：%v", err)
	}
}

// cardCounts 是卡片上与聚合相关的四个数。
type cardCounts struct {
	evaluatees, evaluators, completed, expected int
}

// countsByID 把卡片列表投影为「问卷 id → 四个数」。
func countsByID(cards []*QuestionnaireCard) map[int64]cardCounts {
	out := make(map[int64]cardCounts, len(cards))
	for _, c := range cards {
		out[c.Id] = cardCounts{
			evaluatees: c.EvaluateeCount, evaluators: c.EvaluatorCount,
			completed: c.Completed, expected: c.Expected,
		}
	}
	return out
}

// assertStats 断言两份问卷的聚合结果。
func assertStats(t *testing.T, label string, f statsFixture, got map[int64]cardCounts) {
	t.Helper()
	if want := (cardCounts{evaluatees: 2, evaluators: 2, completed: 1, expected: 3}); got[f.withRels] != want {
		t.Fatalf("%s：有关系问卷期望 %+v，实际 %+v", label, want, got[f.withRels])
	}
	if want := (cardCounts{}); got[f.empty] != want {
		t.Fatalf("%s：空问卷期望各项为 0，实际 %+v", label, got[f.empty])
	}
}

// TestQuestionnaireCardsCountDistinctEvaluators 分页与项目详情的评价人数按评价者去重。
func TestQuestionnaireCardsCountDistinctEvaluators(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedStatsFixture(t, ctx)
	svc := New(qsvc.New(), nil)

	cards, total, err := svc.PageQuestionnaires(ctx, f.projectID, "", 1, 10)
	if err != nil {
		t.Fatalf("分页取问卷失败：%v", err)
	}
	if total != 2 {
		t.Fatalf("期望 2 份问卷，实际 %d", total)
	}
	assertStats(t, "分页", f, countsByID(cards))

	detail, err := svc.GetDetail(ctx, f.projectID)
	if err != nil {
		t.Fatalf("取项目详情失败：%v", err)
	}
	assertStats(t, "项目详情", f, countsByID(detail.Questionnaires))
}
