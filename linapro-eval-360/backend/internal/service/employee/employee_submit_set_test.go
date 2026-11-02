// employee_submit_set_test.go 覆盖批量提交的提交集合校验与逐人答案校验两个纯函数。

package employee

import (
	"testing"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// TestCheckSubmitSet 覆盖提交集合校验的全部分支：通过 / 幂等 / 部分已提交 / 非本人 / 空与重复。
func TestCheckSubmitSet(t *testing.T) {
	// 本人在该问卷下的关系：21、22 未提交，23 已提交，24 已提交。
	owned := map[int64]bool{21: false, 22: false, 23: true, 24: true}

	cases := []struct {
		name      string
		requested []int64
		want      submitSetOutcome
		wantCode  *bizerr.Code
	}{
		{name: "全部未提交即通过", requested: []int64{21, 22}, want: submitSetProceed},
		{name: "不要求覆盖全部未提交的人", requested: []int64{21}, want: submitSetProceed},
		{name: "全部已提交视为幂等", requested: []int64{23, 24}, want: submitSetIdempotent},
		{name: "部分已提交整体拒绝", requested: []int64{21, 23}, wantCode: CodePartiallySubmitted},
		{name: "非本人或不属于该问卷", requested: []int64{21, 99}, wantCode: CodeRelationNotFound},
		{name: "空集合", requested: nil, wantCode: CodeEmptySubmission},
		{name: "重复的被评估人", requested: []int64{21, 21}, wantCode: CodeEmptySubmission},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkSubmitSet(owned, tc.requested)
			if tc.wantCode != nil {
				if !bizerr.Is(err, tc.wantCode) {
					t.Fatalf("期望错误码 %s，实际 err=%v", tc.wantCode.RuntimeCode(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际 err=%v", err)
			}
			if got != tc.want {
				t.Fatalf("期望结论 %d，实际 %d", tc.want, got)
			}
		})
	}
}

// answerFixture 构造一组可见题：必填单选、必填矩阵、选填单行文本。
func answerFixture() map[int64]*questionnaire.QuestionNode {
	return map[int64]*questionnaire.QuestionNode{
		1: {
			Id: 1, Type: questionnaire.QuestionTypeSingleChoice, Required: 1,
			Options: []*questionnaire.OptionNode{{Id: 11}, {Id: 12}},
		},
		2: {
			Id: 2, Type: questionnaire.QuestionTypeMatrix, Required: 1,
			MatrixColumns: []*questionnaire.MatrixColumnNode{{Id: 21}, {Id: 22}},
		},
		3: {Id: 3, Type: questionnaire.QuestionTypeTextShort, Required: 0},
	}
}

// TestValidateAnswers 覆盖矩阵单列作答、不可见题拒绝、必填缺失、重复作答等规则。
func TestValidateAnswers(t *testing.T) {
	cases := []struct {
		name     string
		answers  []AnswerInput
		wantCode *bizerr.Code
	}{
		{
			name:    "矩阵选一列 + 单选即通过（选填文本可不答）",
			answers: []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 22}},
		},
		{
			name: "选填文本作答也通过",
			answers: []AnswerInput{
				{QuestionID: 1, OptionID: 12}, {QuestionID: 2, OptionID: 21}, {QuestionID: 3, TextValue: "好"},
			},
		},
		{
			name:     "矩阵列不属于该题",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 11}},
			wantCode: CodeInvalidAnswer,
		},
		{
			name:     "矩阵同一题选了两列（一人一题只能一条）",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 21}, {QuestionID: 2, OptionID: 22}},
			wantCode: CodeInvalidAnswer,
		},
		{
			name:     "答案指向不可见题",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 21}, {QuestionID: 9, OptionID: 91}},
			wantCode: CodeInvalidAnswer,
		},
		{
			name:     "单选选项不属于该题",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 99}, {QuestionID: 2, OptionID: 21}},
			wantCode: CodeInvalidAnswer,
		},
		{
			name:     "文本只填空白",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 21}, {QuestionID: 3, TextValue: "  "}},
			wantCode: CodeInvalidAnswer,
		},
		{
			name:     "必填矩阵未作答",
			answers:  []AnswerInput{{QuestionID: 1, OptionID: 11}},
			wantCode: CodeMissingRequired,
		},
		{
			name:     "全部未作答",
			answers:  nil,
			wantCode: CodeMissingRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAnswers(answerFixture(), tc.answers, "张三")
			if tc.wantCode == nil {
				if err != nil {
					t.Fatalf("期望通过，实际 err=%v", err)
				}
				return
			}
			if !bizerr.Is(err, tc.wantCode) {
				t.Fatalf("期望错误码 %s，实际 err=%v", tc.wantCode.RuntimeCode(), err)
			}
			// 错误须带被评估人姓名参数，便于批量提交时指明是谁的答案有问题。
			be, ok := bizerr.As(err)
			if !ok || be.Params()["evaluatee"] != "张三" {
				t.Fatalf("期望错误参数 evaluatee=张三，实际 %v", err)
			}
		})
	}
}

// TestValidateAnswersInvisibleRequiredIgnored 验证对该被评估人不可见的必填题不参与必填校验
// （visible 集合由调用方按关系的角色 + 被评估人过滤，题目不在集合内即不要求作答）。
func TestValidateAnswersInvisibleRequiredIgnored(t *testing.T) {
	visible := answerFixture()
	delete(visible, 2) // 必填矩阵对该被评估人不可见。
	if err := validateAnswers(visible, []AnswerInput{{QuestionID: 1, OptionID: 11}}, "李四"); err != nil {
		t.Fatalf("不可见的必填题不应要求作答，实际 err=%v", err)
	}
	// 反过来给不可见题作答则拒绝。
	err := validateAnswers(visible, []AnswerInput{{QuestionID: 1, OptionID: 11}, {QuestionID: 2, OptionID: 21}}, "李四")
	if !bizerr.Is(err, CodeInvalidAnswer) {
		t.Fatalf("给不可见题作答应拒绝，实际 err=%v", err)
	}
}

// TestEvaluateeLabel 验证姓名缺失时退化为关系 id。
func TestEvaluateeLabel(t *testing.T) {
	if got := evaluateeLabel("王五", 7); got != "王五" {
		t.Fatalf("期望 王五，实际 %s", got)
	}
	if got := evaluateeLabel("", 7); got != "#7" {
		t.Fatalf("期望 #7，实际 %s", got)
	}
}

// TestValidateAnswersMatrixCommentRequired 覆盖矩阵评价项评语的必填校验（FB-26）：
//   - 仅「允许评语 + 必填」同时成立时才要求评语；
//   - 只在提交时校验，选填列不填评语照常通过；
//   - 评语为空白等同于未填写；错误必须指明被评估人与题目（标题去标签）。
func TestValidateAnswersMatrixCommentRequired(t *testing.T) {
	// 矩阵题 2 的两列：21=必填评语，22=选填评语；题目标题为富文本，错误提示需降级为纯文本。
	fixture := func() map[int64]*questionnaire.QuestionNode {
		visible := answerFixture()
		visible[2].Title = "<p>领导力表现</p>"
		visible[2].MatrixColumns = []*questionnaire.MatrixColumnNode{
			{Id: 21, AllowComment: 1, CommentRequired: 1},
			{Id: 22, AllowComment: 1, CommentRequired: 0},
		}
		return visible
	}

	t.Run("选填评语的列不填也能提交", func(t *testing.T) {
		err := validateAnswers(fixture(), []AnswerInput{
			{QuestionID: 1, OptionID: 11},
			{QuestionID: 2, OptionID: 22},
		}, "张三")
		if err != nil {
			t.Fatalf("选填评语不应拦截，实际 err=%v", err)
		}
	})

	t.Run("必填评语的列已填写可提交", func(t *testing.T) {
		err := validateAnswers(fixture(), []AnswerInput{
			{QuestionID: 1, OptionID: 11},
			{QuestionID: 2, OptionID: 21, TextValue: "沟通清晰"},
		}, "张三")
		if err != nil {
			t.Fatalf("已填必填评语应通过，实际 err=%v", err)
		}
	})

	t.Run("必填评语的列未填写被拒绝", func(t *testing.T) {
		err := validateAnswers(fixture(), []AnswerInput{
			{QuestionID: 1, OptionID: 11},
			{QuestionID: 2, OptionID: 21},
		}, "张三")
		if !bizerr.Is(err, CodeMatrixCommentRequired) {
			t.Fatalf("期望错误码 %s，实际 err=%v", CodeMatrixCommentRequired.RuntimeCode(), err)
		}
		be, ok := bizerr.As(err)
		if !ok {
			t.Fatalf("期望 bizerr，实际 %v", err)
		}
		if be.Params()["evaluatee"] != "张三" {
			t.Fatalf("期望错误参数 evaluatee=张三，实际 %v", be.Params())
		}
		// 标题为富文本，提示里必须是去标签后的纯文本。
		if be.Params()["title"] != "领导力表现" {
			t.Fatalf("期望错误参数 title=领导力表现，实际 %v", be.Params())
		}
	})

	t.Run("必填评语只填空白等同未填", func(t *testing.T) {
		err := validateAnswers(fixture(), []AnswerInput{
			{QuestionID: 1, OptionID: 11},
			{QuestionID: 2, OptionID: 21, TextValue: "   "},
		}, "张三")
		if !bizerr.Is(err, CodeMatrixCommentRequired) {
			t.Fatalf("空白评语应被拒绝，实际 err=%v", err)
		}
	})

	t.Run("评价项未开启允许评语时评语不参与校验", func(t *testing.T) {
		visible := answerFixture()
		visible[2].MatrixColumns = []*questionnaire.MatrixColumnNode{
			{Id: 21, AllowComment: 0, CommentRequired: 1},
		}
		err := validateAnswers(visible, []AnswerInput{
			{QuestionID: 1, OptionID: 11},
			{QuestionID: 2, OptionID: 21},
		}, "张三")
		if err != nil {
			t.Fatalf("未开启允许评语时不应要求评语，实际 err=%v", err)
		}
	})
}
