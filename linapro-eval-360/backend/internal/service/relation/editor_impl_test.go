// editor_impl_test.go 覆盖关系编辑器「按姓名批量新增评价关系」的语义（FB-13 职务/部门、FB-32 按姓名录入）。
//
// 与 spec「关系编辑器」需求一致：
//  1. 新增评价关系时被评估人不存在 → 以入参职务/部门新建，明细列表回显这两列；
//  2. 被评估人已存在 → 非空职务/部门覆盖原值，留空保留原值（行内「新增」预填后原样提交即不变）；
//  3. 只录入姓名：任一姓名有两名及以上在职同名员工或找不到 → 整批拒绝、零写入，错误参数点名全部问题姓名。
//
// 复用 import_impl_test.go 的 seedProfileFixture / seedHomonym（真实 PostgreSQL + empcap 测试实现）。
package relation

import (
	"errors"
	"strings"
	"testing"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"
)

// TestAddRelationsWritesAndUpdatesEvaluateeProfile 覆盖新建写入、空值保留、非空覆盖与列表回显。
func TestAddRelationsWritesAndUpdatesEvaluateeProfile(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, peer, er := f.emps[0], f.emps[1], f.emps[2]
	svc := New()

	// 1. 被评估人首次出现：按入参（归一化后）新建；姓名首尾空白被去掉后仍能匹配。
	ids, err := svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{{
			EvaluateeName:       " " + ee.Name + " ",
			EvaluateeJobTitle:   "  销售总监 ",
			EvaluateeDepartment: "华东销售部",
			EvaluatorName:       er.Name,
			RoleName:            "上级",
		}},
	})
	if err != nil {
		t.Fatalf("首次新增失败：%v", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		t.Fatalf("期望返回 1 个新评价关系 id，实际 %v", ids)
	}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, ee.EmpID); got == nil ||
		*got != (evaluateeProfile{JobTitle: "销售总监", Department: "华东销售部"}) {
		t.Fatalf("新建被评估人职务/部门期望 销售总监/华东销售部，实际 %+v", got)
	}

	// 2. 已存在 + 职务留空、部门改值：职务保留，部门覆盖。
	if _, err = svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{{
			EvaluateeName:       ee.Name,
			EvaluateeDepartment: "华南销售部",
			EvaluatorName:       peer.Name,
			RoleName:            "平级",
		}},
	}); err != nil {
		t.Fatalf("追加新增失败：%v", err)
	}
	want := evaluateeProfile{JobTitle: "销售总监", Department: "华南销售部"}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, ee.EmpID); got == nil || *got != want {
		t.Fatalf("已存在被评估人职务/部门期望 %+v，实际 %+v", want, got)
	}

	// 3. 明细列表回显职务/部门。
	matrix, err := svc.ListRelations(ctx, f.questionnaireID)
	if err != nil {
		t.Fatalf("读取明细失败：%v", err)
	}
	if len(matrix.Evaluatees) != 1 {
		t.Fatalf("期望 1 名被评估人，实际 %d", len(matrix.Evaluatees))
	}
	row := matrix.Evaluatees[0]
	testkit.Equal(t, "列表职务", row.EvaluateeJobTitle, want.JobTitle)
	testkit.Equal(t, "列表部门", row.EvaluateeDepartment, want.Department)
}

// TestAddRelationsBatchSharesEvaluateeAndRole 一次提交多条：同一被评估人与同一角色只建一份，返回 id 与条目一一对应。
func TestAddRelationsBatchSharesEvaluateeAndRole(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, peer, er := f.emps[0], f.emps[1], f.emps[2]

	ids, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{
			{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"},
			{EvaluateeName: ee.Name, EvaluatorName: peer.Name, RoleName: "上级"},
		},
	})
	if err != nil {
		t.Fatalf("批量新增失败：%v", err)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("期望返回 2 个不同的评价关系 id，实际 %v", ids)
	}
	matrix, err := New().ListRelations(ctx, f.questionnaireID)
	if err != nil {
		t.Fatalf("读取明细失败：%v", err)
	}
	if len(matrix.Evaluatees) != 1 || len(matrix.Roles) != 1 {
		t.Fatalf("期望 1 名被评估人、1 个角色，实际 %d / %d", len(matrix.Evaluatees), len(matrix.Roles))
	}
	if groups := matrix.Evaluatees[0].Groups; len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("期望该角色下 2 名评价者，实际 %+v", groups)
	}
}

// TestAddRelationsRejectsDuplicateNameWithoutWriting 一批中某个评价者姓名有在职同名员工（另一租户）
// → 整批拒绝、零写入（含排在前面、本可解析的条目），错误参数点名该姓名。
func TestAddRelationsRejectsDuplicateNameWithoutWriting(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, peer, er := f.emps[0], f.emps[1], f.emps[2]
	seedHomonym(t, ctx, 2, er.Name, false)

	_, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{
			{EvaluateeName: ee.Name, EvaluatorName: peer.Name, RoleName: "平级"},
			{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"},
		},
	})
	requireBizCode(t, err, CodeEmployeeNameDuplicate)
	testkit.Equal(t, "重名姓名参数", namesParam(t, err), er.Name)
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("重名时整批不应写入，实际 %d 条评价关系", n)
	}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, ee.EmpID); got != nil {
		t.Fatalf("重名时不应新建被评估人，实际 %+v", got)
	}
}

// TestAddRelationsIgnoresDepartedHomonym 离职的同名员工不算重名，按姓名解析到唯一在职员工。
func TestAddRelationsIgnoresDepartedHomonym(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, er := f.emps[0], f.emps[2]
	seedHomonym(t, ctx, 1, ee.Name, true)

	if _, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items:           []AddRelationItem{{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"}},
	}); err != nil {
		t.Fatalf("离职同名不应拦截：%v", err)
	}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, ee.EmpID); got == nil {
		t.Fatal("被评估人应解析为在职员工并新建")
	}
}

// TestAddRelationsRejectsUnknownNames 姓名找不到在职员工 → 整批拒绝，错误参数按出现顺序点名全部未找到的姓名。
func TestAddRelationsRejectsUnknownNames(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee := f.emps[0]

	_, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{
			{EvaluateeName: ee.Name, EvaluatorName: "查无此人FB32甲", RoleName: "上级"},
			{EvaluateeName: ee.Name, EvaluatorName: "查无此人FB32乙", RoleName: "上级"},
		},
	})
	requireBizCode(t, err, CodeEmployeeNameNotFound)
	testkit.Equal(t, "未找到姓名参数", namesParam(t, err), "查无此人FB32甲、查无此人FB32乙")
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("找不到员工时不应写入，实际 %d 条评价关系", n)
	}
}

// namesParam 取业务错误的 names 参数。
func namesParam(t *testing.T, err error) string {
	t.Helper()

	var bizErr *bizerr.Error
	if !errors.As(err, &bizErr) {
		t.Fatalf("期望结构化业务错误，实际 %T：%v", err, err)
	}
	v, ok := bizErr.Params()["names"].(string)
	if !ok {
		t.Fatalf("错误参数缺少 names：%v", bizErr.Params())
	}
	return strings.TrimSpace(v)
}
