// relation_conflict_test.go 覆盖 FB-5「同一被评估人下，同一评价者只能属于一个角色、且不得重复」的校验。
//
// 纯函数用例（importConflictReasons）无需数据库；新增/导入用例直连真实 PostgreSQL（经 testkit 自举建表），
// 复用 import_impl_test.go 的 seedProfileFixture / buildImportXLSX / relationCount。

package relation

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"
)

// entryOf 拼一条待校验条目（被评估人/评价者 EmpID 固定映射到姓名，便于断言原因文案）。
func entryOf(row int, ee, er int64, role string) pairEntry {
	names := map[int64]string{1: "张三", 2: "李四", 3: "王五"}
	return pairEntry{
		rowNum:        row,
		pair:          evaluatorPair{evaluateeEmpID: ee, evaluatorEmpID: er},
		evaluateeName: names[ee],
		evaluatorName: names[er],
		roleName:      role,
	}
}

// TestImportConflictReasons 覆盖导入文件内的判定：跨角色/同角色重复的每一行都判失败，不同被评估人互不影响。
func TestImportConflictReasons(t *testing.T) {
	t.Run("同一被评估人下评价者跨角色，涉及的两行都失败", func(t *testing.T) {
		got := importConflictReasons([]pairEntry{
			entryOf(3, 1, 2, "上级"),
			entryOf(5, 1, 3, "同事"),
			entryOf(8, 1, 2, "同事"),
		})
		if len(got) != 2 {
			t.Fatalf("期望第 3、8 行失败，实际 %v", got)
		}
		want := "被评估人「张三」的评价者「李四」同时出现在多个角色中：「上级」（第 3 行）、「同事」（第 8 行），同一评价者对同一被评估人只能属于一个角色"
		testkit.Equal(t, "第 3 行原因", got[3], want)
		testkit.Equal(t, "第 8 行原因", got[8], want)
	})

	t.Run("同一角色下评价者重复，涉及的两行都失败", func(t *testing.T) {
		got := importConflictReasons([]pairEntry{
			entryOf(2, 1, 2, "同事"),
			entryOf(4, 1, 2, "同事"),
		})
		want := "被评估人「张三」的角色「同事」下评价者「李四」重复（第 2、4 行）"
		testkit.Equal(t, "第 2 行原因", got[2], want)
		testkit.Equal(t, "第 4 行原因", got[4], want)
	})

	t.Run("同一评价者评不同被评估人、同一角色下各不相同均放行", func(t *testing.T) {
		got := importConflictReasons([]pairEntry{
			entryOf(2, 1, 2, "上级"),
			entryOf(3, 3, 2, "同事"),
			entryOf(4, 1, 3, "同事"),
		})
		if len(got) != 0 {
			t.Fatalf("期望无冲突，实际 %v", got)
		}
	})
}

// TestAddRelationsRejectsCrossRoleInBatch 同一批内同一被评估人的同一评价者出现在两个角色 → 整批拒绝、零写入。
func TestAddRelationsRejectsCrossRoleInBatch(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, peer, er := f.emps[0], f.emps[1], f.emps[2]

	_, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{
			{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"},
			{EvaluateeName: ee.Name, EvaluatorName: peer.Name, RoleName: "同事"},
			{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "同事"},
		},
	})
	requireBizCode(t, err, CodeRelationEvaluatorRoleConflict)
	params := bizParams(t, err)
	testkit.Equal(t, "被评估人参数", params["evaluatee"], ee.Name)
	testkit.Equal(t, "评价者参数", params["evaluator"], er.Name)
	testkit.Equal(t, "角色参数", params["roles"], "上级、同事")
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("跨角色时整批不应写入，实际 %d 条评价关系", n)
	}
}

// TestAddRelationsRejectsSameRoleDuplicateInBatch 同一批内同一角色下评价者重复 → 整批拒绝、零写入。
func TestAddRelationsRejectsSameRoleDuplicateInBatch(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, er := f.emps[0], f.emps[2]

	_, err := New().AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items: []AddRelationItem{
			{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "同事"},
			// 角色名归一化后与上一条相同，仍视为同一角色。
			{EvaluateeName: ee.Name, EvaluatorName: " " + er.Name + " ", RoleName: " 同事 "},
		},
	})
	requireBizCode(t, err, CodeRelationEvaluatorDuplicate)
	testkit.Equal(t, "角色参数", bizParams(t, err)["role"], "同事")
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("同角色重复时整批不应写入，实际 %d 条评价关系", n)
	}
}

// TestAddRelationsRejectsConflictWithExisting 库中已有「张三-上级-李四」：再加「张三-同事-李四」报跨角色，
// 再加「张三-上级-李四」（编辑弹窗同一格重复录入）报重复；两种情况均零写入。
// 李四评价其他被评估人不受影响（例 2）。
func TestAddRelationsRejectsConflictWithExisting(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, otherEe, er := f.emps[0], f.emps[1], f.emps[2]
	svc := New()

	if _, err := svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items:           []AddRelationItem{{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"}},
	}); err != nil {
		t.Fatalf("预置评价关系失败：%v", err)
	}

	_, err := svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items:           []AddRelationItem{{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "同事"}},
	})
	requireBizCode(t, err, CodeRelationEvaluatorRoleConflict)
	testkit.Equal(t, "角色参数", bizParams(t, err)["roles"], "上级、同事")

	_, err = svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items:           []AddRelationItem{{EvaluateeName: ee.Name, EvaluatorName: er.Name, RoleName: "上级"}},
	})
	requireBizCode(t, err, CodeRelationEvaluatorDuplicate)
	if n := relationCount(t, ctx, f.questionnaireID); n != 1 {
		t.Fatalf("冲突时不应写入，期望仍为 1 条评价关系，实际 %d", n)
	}

	// 同一评价者评价另一名被评估人（不同角色）放行。
	if _, err = svc.AddRelations(ctx, AddRelationsInput{
		QuestionnaireID: f.questionnaireID,
		Items:           []AddRelationItem{{EvaluateeName: otherEe.Name, EvaluatorName: er.Name, RoleName: "同事"}},
	}); err != nil {
		t.Fatalf("评价其他被评估人不应被拦截：%v", err)
	}
}

// TestImportRelationsRejectsEvaluatorConflicts 导入文件内跨角色与同角色重复：涉及的每一行都判失败、整批不写入，
// 失败清单按行号排序。
func TestImportRelationsRejectsEvaluatorConflicts(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	ee, peer, er := f.emps[0], f.emps[1], f.emps[2]

	result, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(ee, er, "", "", "", "上级"),   // 第 2 行
			importRowOf(peer, er, "", "", "", "同事"), // 第 3 行：评价另一名被评估人，放行
			importRowOf(ee, peer, "", "", "", "同事"), // 第 4 行
			importRowOf(ee, er, "", "", "", "同事"),   // 第 5 行：与第 2 行跨角色
			importRowOf(ee, peer, "", "", "", "同事"), // 第 6 行：与第 4 行同角色重复
		}),
	})
	if err != nil {
		t.Fatalf("导入不应返回系统错误：%v", err)
	}
	gotRows := make([]int, 0, len(result.FailList))
	for _, item := range result.FailList {
		gotRows = append(gotRows, item.Row)
	}
	if want := []int{2, 4, 5, 6}; !slices.Equal(gotRows, want) {
		t.Fatalf("失败行期望 %v，实际 %v（%+v）", want, gotRows, result.FailList)
	}
	if !strings.Contains(result.FailList[0].Reason, "同时出现在多个角色中") {
		t.Fatalf("第 2 行应报跨角色，实际 %q", result.FailList[0].Reason)
	}
	if !strings.Contains(result.FailList[1].Reason, "重复（第 4、6 行）") {
		t.Fatalf("第 4 行应报同角色重复，实际 %q", result.FailList[1].Reason)
	}
	if result.Success != 0 || relationCount(t, ctx, f.questionnaireID) != 0 {
		t.Fatalf("存在冲突时整批不应写入，success=%d", result.Success)
	}
}

// bizParams 取业务错误的参数表（值按字符串返回，便于断言）。
func bizParams(t *testing.T, err error) map[string]string {
	t.Helper()

	var bizErr *bizerr.Error
	if !errors.As(err, &bizErr) {
		t.Fatalf("期望结构化业务错误，实际 %T：%v", err, err)
	}
	out := make(map[string]string, len(bizErr.Params()))
	for k, v := range bizErr.Params() {
		out[k] = fmt.Sprint(v)
	}
	return out
}
