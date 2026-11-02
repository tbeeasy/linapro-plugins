// role_delete_test.go 覆盖角色删除 `DeleteRole` 的级联与拦截语义（FB-12）。
//
// 直连真实 PostgreSQL（经 testkit 自举建表）验证三条业务红线，与
// openspec/changes/linapro-eval-360/specs/linapro-eval-360/spec.md「关系编辑器」需求一致：
//  1. `cascade=false` 且角色下仍有评价关系 → 拒绝（EVAL360_ROLE_NOT_EMPTY），不做任何改动；
//  2. `cascade=true` → 在单事务内连带删除该角色在**全部被评估人**名下的评价关系（角色为问卷级，
//     不是只清当前那一格），随后角色本身消失；
//  3. 该角色下任一评价关系已有作答 → 一律拒绝（EVAL360_ROLE_HAS_SUBMISSION），
//     且拒绝后角色、关系与作答均原样保留（无部分删除）。
//
// 作答拦截只在其中一条关系上造作答：证明判定是「按角色整体」而非只看当前那一格。
package relation

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"
)

// roleDeleteFixture 是删除角色用例共用的数据快照。
type roleDeleteFixture struct {
	projectID       int64
	questionnaireID int64
	roleID          int64
	relationIDs     []int64
	submissionID    int64
}

// seedRoleDeleteFixture 建一份最小可用数据：1 个问卷 + 2 个被评估人 + 1 个角色，
// 两个被评估人各挂一条该角色的评价关系。跨两个被评估人正是为了证明级联删除是问卷级生效。
// 数据在 t.Cleanup 中按依赖倒序清理，保证用例自包含且可重复运行。
func seedRoleDeleteFixture(t *testing.T, ctx context.Context) *roleDeleteFixture {
	t.Helper()

	projectID, err := dao.Project.Ctx(ctx).Data(do.Project{
		Name:        "FB12 删除角色 " + t.Name(),
		Description: "集成测试",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}
	questionnaireID, err := dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId: projectID,
		Title:     "FB12 问卷 " + t.Name(),
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建问卷失败：%v", err)
	}
	weight := decimal.NewFromInt(50)
	roleID, err := dao.Role.Ctx(ctx).Data(do.Role{
		QuestionnaireId: questionnaireID,
		Name:            "同事",
		Weight:          &weight,
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建角色失败：%v", err)
	}

	fixture := &roleDeleteFixture{
		projectID:       projectID,
		questionnaireID: questionnaireID,
		roleID:          roleID,
		relationIDs:     make([]int64, 0, 2),
	}
	for i, empID := range []int64{9201, 9202} {
		evaluateeID, iErr := dao.Evaluatee.Ctx(ctx).Data(do.Evaluatee{
			ProjectId:       projectID,
			QuestionnaireId: questionnaireID,
			EvaluateeEmpId:  empID,
		}).InsertAndGetId()
		if iErr != nil {
			t.Fatalf("建第 %d 个被评估人失败：%v", i+1, iErr)
		}
		relationID, rErr := dao.Relation.Ctx(ctx).Data(do.Relation{
			ProjectId:       projectID,
			QuestionnaireId: questionnaireID,
			EvaluateeId:     evaluateeID,
			EvaluatorEmpId:  int64(9300 + i),
			RoleId:          roleID,
			Source:          SourceManual,
		}).InsertAndGetId()
		if rErr != nil {
			t.Fatalf("建第 %d 条评价关系失败：%v", i+1, rErr)
		}
		fixture.relationIDs = append(fixture.relationIDs, relationID)
	}

	t.Cleanup(func() {
		// 角色与关系可能已被级联删除，故清理一律用「按条件删除 + 忽略影响行数」，
		// 保证用例顺序无关且可重复运行。
		if _, dErr := dao.Submission.Ctx(ctx).
			Where(dao.Submission.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理作答失败：%v", dErr)
		}
		if _, dErr := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理评价关系失败：%v", dErr)
		}
		if _, dErr := dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理被评估人失败：%v", dErr)
		}
		if _, dErr := dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理角色失败：%v", dErr)
		}
		if _, dErr := dao.Questionnaire.Ctx(ctx).
			Where(dao.Questionnaire.Columns().Id, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理问卷失败：%v", dErr)
		}
		if _, dErr := dao.Project.Ctx(ctx).
			Where(dao.Project.Columns().Id, projectID).Delete(); dErr != nil {
			t.Errorf("清理项目失败：%v", dErr)
		}
	})
	return fixture
}

// addSubmissionOnFirstRelation 只在第一条关系上造作答，供作答拦截用例使用。
func addSubmissionOnFirstRelation(t *testing.T, ctx context.Context, f *roleDeleteFixture) {
	t.Helper()

	submissionID, err := dao.Submission.Ctx(ctx).Data(do.Submission{
		RelationId:      f.relationIDs[0],
		QuestionnaireId: f.questionnaireID,
		EvaluateeId:     f.relationIDs[0],
		EvaluatorEmpId:  9300,
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("造作答失败：%v", err)
	}
	f.submissionID = submissionID
}

// inspectRole 返回角色是否仍存在、其名下关系数、这些关系上的作答数。
func inspectRole(t *testing.T, ctx context.Context, f *roleDeleteFixture) (bool, int, int) {
	t.Helper()

	roleCount, err := dao.Role.Ctx(ctx).Where(dao.Role.Columns().Id, f.roleID).Count()
	if err != nil {
		t.Fatalf("统计角色失败：%v", err)
	}
	relCount, err := dao.Relation.Ctx(ctx).
		WhereIn(dao.Relation.Columns().Id, f.relationIDs).Count()
	if err != nil {
		t.Fatalf("统计评价关系失败：%v", err)
	}
	subCount, err := dao.Submission.Ctx(ctx).
		WhereIn(dao.Submission.Columns().RelationId, f.relationIDs).Count()
	if err != nil {
		t.Fatalf("统计作答失败：%v", err)
	}
	return roleCount > 0, relCount, subCount
}

// requireBizCode 断言错误正是期望的业务错误码，避免把「参数错误」等其它失败误判为拦截生效。
func requireBizCode(t *testing.T, err error, want *bizerr.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("期望被拒绝并返回错误码 %s，实际删除成功", want.RuntimeCode())
	}
	var bizErr *bizerr.Error
	if !errors.As(err, &bizErr) {
		t.Fatalf("期望结构化业务错误，实际 %T：%v", err, err)
	}
	if !bizErr.Matches(want) {
		t.Fatalf("期望错误码 %s，实际 %s", want.RuntimeCode(), bizErr.RuntimeCode())
	}
}

// 角色下仍有评价关系且未要求级联：必须拒绝，且角色与关系都不得被改动。
func TestDeleteRoleRejectsNonEmptyWithoutCascade(t *testing.T) {
	ctx := testkit.Setup(t)
	fixture := seedRoleDeleteFixture(t, ctx)

	err := New().DeleteRole(ctx, fixture.roleID, false)
	requireBizCode(t, err, CodeRoleNotEmpty)

	roleExists, relCount, _ := inspectRole(t, ctx, fixture)
	if !roleExists {
		t.Error("拒绝删除后角色必须仍然存在")
	}
	if relCount != 2 {
		t.Errorf("拒绝删除后评价关系必须原样保留，期望 2 条，实际 %d 条", relCount)
	}
}

// 空角色（无任何评价关系）无需级联也允许删除。
func TestDeleteRoleAllowsEmptyRoleWithoutCascade(t *testing.T) {
	ctx := testkit.Setup(t)
	fixture := seedRoleDeleteFixture(t, ctx)

	if _, err := dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().RoleId, fixture.roleID).Delete(); err != nil {
		t.Fatalf("清空角色下评价关系失败：%v", err)
	}
	if err := New().DeleteRole(ctx, fixture.roleID, false); err != nil {
		t.Fatalf("空角色应可直接删除，实际 %v", err)
	}

	roleExists, _, _ := inspectRole(t, ctx, fixture)
	if roleExists {
		t.Error("删除后角色应已不存在")
	}
}

// 级联删除：角色连同它在全部被评估人名下的评价关系一并消失。
func TestDeleteRoleCascadesAcrossAllEvaluatees(t *testing.T) {
	ctx := testkit.Setup(t)
	fixture := seedRoleDeleteFixture(t, ctx)

	if err := New().DeleteRole(ctx, fixture.roleID, true); err != nil {
		t.Fatalf("无作答时级联删除角色应成功，实际 %v", err)
	}

	roleExists, relCount, _ := inspectRole(t, ctx, fixture)
	if roleExists {
		t.Error("级联删除后角色应已删除")
	}
	if relCount != 0 {
		t.Errorf("级联删除后该角色下全部评价关系应已删除，实际剩余 %d 条", relCount)
	}
}

// 角色下已有作答：级联删除同样必须拒绝，且不做任何部分删除。
func TestDeleteRoleRejectsWhenAnyRelationHasSubmission(t *testing.T) {
	ctx := testkit.Setup(t)
	fixture := seedRoleDeleteFixture(t, ctx)
	addSubmissionOnFirstRelation(t, ctx, fixture)

	err := New().DeleteRole(ctx, fixture.roleID, true)
	requireBizCode(t, err, CodeRoleHasSubmission)

	roleExists, relCount, subCount := inspectRole(t, ctx, fixture)
	if !roleExists {
		t.Error("作答拦截后角色必须仍然存在")
	}
	if relCount != 2 {
		t.Errorf("作答拦截后评价关系必须原样保留，期望 2 条，实际 %d 条", relCount)
	}
	if subCount != 1 {
		t.Errorf("作答拦截不得影响作答记录，期望 1 条，实际 %d 条", subCount)
	}
}
