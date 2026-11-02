// copy_impl_test.go 覆盖「复制问卷」`CopyQuestionnaire` 对矩阵评价项评语配置与题目描述的深拷贝（FB-26）。
//
// 直连真实 PostgreSQL（经 testkit 自举建表）：源问卷建一道矩阵题，两个评价项分别为
// 「允许评语 + 必填 + 提示语 + 预设 + 允许自定义」与「不允许评语」，复制后逐列比对
// 评语五字段与题目描述，证明复制不会丢失评语相关配置。
package questionnaire

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"
)

// cleanupQuestionnaire 按依赖倒序清理一份问卷的题目结构、维度与问卷本体。
func cleanupQuestionnaire(t *testing.T, ctx context.Context, questionnaireID int64) {
	t.Helper()

	var qids []int64
	vals, err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, questionnaireID).
		Array(dao.Question.Columns().Id)
	if err != nil {
		t.Errorf("查询待清理题目失败：%v", err)
		return
	}
	for _, v := range vals {
		qids = append(qids, v.Int64())
	}
	if len(qids) > 0 {
		if _, dErr := dao.MatrixColumn.Ctx(ctx).
			WhereIn(dao.MatrixColumn.Columns().QuestionId, qids).Delete(); dErr != nil {
			t.Errorf("清理矩阵列失败：%v", dErr)
		}
	}
	if _, dErr := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
		t.Errorf("清理题目失败：%v", dErr)
	}
	if _, dErr := dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
		t.Errorf("清理维度失败：%v", dErr)
	}
	if _, dErr := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, questionnaireID).Delete(); dErr != nil {
		t.Errorf("清理问卷失败：%v", dErr)
	}
}

// loadMatrixColumns 按 sort 读取某问卷下唯一一道矩阵题的全部列及该题本体。
func loadMatrixColumns(t *testing.T, ctx context.Context, questionnaireID int64) (*entitymodel.Question, []*entitymodel.MatrixColumn) {
	t.Helper()

	var q *entitymodel.Question
	if err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, questionnaireID).Scan(&q); err != nil || q == nil {
		t.Fatalf("读取矩阵题失败：%v", err)
	}
	var cols []*entitymodel.MatrixColumn
	if err := dao.MatrixColumn.Ctx(ctx).
		Where(dao.MatrixColumn.Columns().QuestionId, q.Id).
		OrderAsc(dao.MatrixColumn.Columns().Sort).Scan(&cols); err != nil {
		t.Fatalf("读取矩阵列失败：%v", err)
	}
	return q, cols
}

// 复制问卷：矩阵评价项的评语五字段与题目描述须原样带到新问卷。
func TestCopyQuestionnaireKeepsMatrixColumnComment(t *testing.T) {
	ctx := testkit.Setup(t)

	projectID, err := dao.Project.Ctx(ctx).Data(do.Project{
		Name: "FB26 复制问卷 " + t.Name(), Description: "集成测试",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}
	t.Cleanup(func() {
		if _, dErr := dao.Project.Ctx(ctx).Where(dao.Project.Columns().Id, projectID).Delete(); dErr != nil {
			t.Errorf("清理项目失败：%v", dErr)
		}
	})
	srcID, err := dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId: projectID, Title: "FB26 源问卷",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建源问卷失败：%v", err)
	}
	t.Cleanup(func() { cleanupQuestionnaire(t, ctx, srcID) })

	dimID, err := dao.Dimension.Ctx(ctx).Data(do.Dimension{
		QuestionnaireId: srcID, Name: "领导力", Sort: 0,
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建维度失败：%v", err)
	}
	qID, err := dao.Question.Ctx(ctx).Data(do.Question{
		QuestionnaireId: srcID, DimensionId: dimID, Type: "matrix",
		Title: "矩阵题", Description: "<p>按实际表现评价</p>", Required: 1, Sort: 0,
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建矩阵题失败：%v", err)
	}
	five, four := decimal.NewFromInt(5), decimal.NewFromInt(4)
	if _, err = dao.MatrixColumn.Ctx(ctx).Data(do.MatrixColumn{
		QuestionId: qID, Label: "优秀", Score: &five, Sort: 0,
		AllowComment: 1, CommentRequired: 1, CommentPlaceholder: "请举例说明",
		CommentPresets: `["沟通清晰","推动力强"]`, CommentAllowCustom: 1,
	}).Insert(); err != nil {
		t.Fatalf("建评价项「优秀」失败：%v", err)
	}
	if _, err = dao.MatrixColumn.Ctx(ctx).Data(do.MatrixColumn{
		QuestionId: qID, Label: "良好", Score: &four, Sort: 1,
	}).Insert(); err != nil {
		t.Fatalf("建评价项「良好」失败：%v", err)
	}

	newID, err := New().CopyQuestionnaire(ctx, srcID, "FB26 复制问卷")
	if err != nil {
		t.Fatalf("复制问卷失败：%v", err)
	}
	t.Cleanup(func() { cleanupQuestionnaire(t, ctx, newID) })

	newQ, cols := loadMatrixColumns(t, ctx, newID)
	if newQ.Description != "<p>按实际表现评价</p>" {
		t.Errorf("题目描述未复制：got %q", newQ.Description)
	}
	if len(cols) != 2 {
		t.Fatalf("矩阵列数：got %d want 2", len(cols))
	}
	got := cols[0]
	if got.Label != "优秀" || got.AllowComment != 1 || got.CommentRequired != 1 ||
		got.CommentPlaceholder != "请举例说明" || got.CommentPresets != `["沟通清晰","推动力强"]` ||
		got.CommentAllowCustom != 1 {
		t.Errorf("「优秀」评语配置未完整复制：%+v", *got)
	}
	plain := cols[1]
	if plain.Label != "良好" || plain.AllowComment != 0 || plain.CommentRequired != 0 ||
		plain.CommentPlaceholder != "" || plain.CommentPresets != "" || plain.CommentAllowCustom != 0 {
		t.Errorf("「良好」应保持不允许评语：%+v", *plain)
	}
}
