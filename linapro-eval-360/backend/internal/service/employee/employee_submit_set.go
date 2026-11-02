// employee_submit_set.go 实现批量提交的「提交集合校验」与「逐人答案校验」两个纯函数（无 IO，便于单测）。

package employee

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// submitSetOutcome 是提交集合校验的结论。
type submitSetOutcome int

const (
	// submitSetProceed 请求内的被评估人全部未提交，可以继续校验答案并落库。
	submitSetProceed submitSetOutcome = iota
	// submitSetIdempotent 请求内的被评估人全部已提交，视为重复提交（重试/双击），按幂等返回。
	submitSetIdempotent
)

// checkSubmitSet 校验批量提交的被评估人集合。
//   - owned：当前评价者在该问卷下的全部关系 → 是否已提交；
//   - requested：本次请求的关系 id（页面上已展示的未提交被评估人）。
//
// 规则（产品口径：提交页面上已展示的人；页面打开后新增的人不受影响，不要求覆盖全部未提交的人）：
//   - requested 为空或含重复 → CodeEmptySubmission；
//   - 任一 relationId 不在 owned 内（非本人或不属于该问卷）→ CodeRelationNotFound；
//   - 全部已提交 → submitSetIdempotent；
//   - 部分已提交 → CodePartiallySubmitted；
//   - 全部未提交 → submitSetProceed。
func checkSubmitSet(owned map[int64]bool, requested []int64) (submitSetOutcome, error) {
	if len(requested) == 0 {
		return 0, bizerr.NewCode(CodeEmptySubmission)
	}
	seen := make(map[int64]struct{}, len(requested))
	submittedCount := 0
	for _, id := range requested {
		if _, dup := seen[id]; dup {
			return 0, bizerr.NewCode(CodeEmptySubmission)
		}
		seen[id] = struct{}{}
		submitted, ok := owned[id]
		if !ok {
			return 0, bizerr.NewCode(CodeRelationNotFound)
		}
		if submitted {
			submittedCount++
		}
	}
	switch submittedCount {
	case 0:
		return submitSetProceed, nil
	case len(requested):
		return submitSetIdempotent, nil
	default:
		return 0, bizerr.NewCode(CodePartiallySubmitted)
	}
}

// validateAnswers 校验单个被评估人（关系）的答案合法性与必填。
//   - visible：该关系可见的题目（question_id → 题目节点）；
//   - evaluatee：被评估人展示名，填入错误参数，便于批量提交时指明是谁的答案有问题；
//   - 答案必须指向可见题，且一题只能有一条；
//   - 单选选项 / 矩阵列 id 必须属于该题；文本答案不能为空白；
//   - 可见的必填题必须作答（矩阵 = 该被评估人选一列）。
func validateAnswers(visible map[int64]*questionnaire.QuestionNode, answers []AnswerInput, evaluatee string) error {
	invalid := func() error { return bizerr.NewCode(CodeInvalidAnswer, bizerr.P("evaluatee", evaluatee)) }
	answered := make(map[int64]bool, len(answers))
	for _, a := range answers {
		qq := visible[a.QuestionID]
		if qq == nil {
			// 答案指向不可见题 → 非法（可能是越权或题目已变）。
			return invalid()
		}
		if answered[a.QuestionID] {
			// 一题一条：同一题重复作答 → 非法。
			return invalid()
		}
		switch qq.Type {
		case questionnaire.QuestionTypeSingleChoice:
			if !hasOption(qq.Options, a.OptionID) {
				return invalid()
			}
		case questionnaire.QuestionTypeMatrix:
			// 矩阵已退化为 per-relation 单选：行由被评估人承载，单条关系里矩阵题只有一条答案，
			// option_id=所选列（落库 row_id 恒 0）。列 id 必须属于该题。
			col := matrixColumnByID(qq.MatrixColumns, a.OptionID)
			if col == nil {
				return invalid()
			}
			// 评语必填：所选评价项开启「允许评语 + 必填」时，评语不得为空白。
			// 只在提交时校验——作答过程中允许先选列后补评语、切换所选列。
			if col.AllowComment == 1 && col.CommentRequired == 1 && strings.TrimSpace(a.TextValue) == "" {
				return bizerr.NewCode(
					CodeMatrixCommentRequired,
					bizerr.P("evaluatee", evaluatee),
					bizerr.P("title", stripHTMLTags(qq.Title)),
				)
			}
		case questionnaire.QuestionTypeTextShort, questionnaire.QuestionTypeTextLong:
			if strings.TrimSpace(a.TextValue) == "" {
				return invalid()
			}
		default:
			return invalid()
		}
		answered[a.QuestionID] = true
	}

	// 必填校验：该关系可见的必填题必须全部作答（各题型一致：该题有一条合法答案）。
	for _, qq := range visible {
		if qq.Required == 1 && !answered[qq.Id] {
			return bizerr.NewCode(CodeMissingRequired, bizerr.P("evaluatee", evaluatee))
		}
	}
	return nil
}

// hasOption 判断选项 id 是否属于该单选题。
func hasOption(options []*questionnaire.OptionNode, id int64) bool {
	for _, o := range options {
		if o.Id == id {
			return true
		}
	}
	return false
}

// matrixColumnByID 取出该矩阵题中 id 匹配的评价项节点；不存在返回 nil。
func matrixColumnByID(columns []*questionnaire.MatrixColumnNode, id int64) *questionnaire.MatrixColumnNode {
	for _, c := range columns {
		if c.Id == id {
			return c
		}
	}
	return nil
}

// htmlTagPattern 匹配富文本标签，用于把题目标题降级为纯文本放入错误提示。
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// stripHTMLTags 去掉富文本标签与 HTML 实体，得到错误提示可读的纯文本标题。
func stripHTMLTags(s string) string {
	text := htmlTagPattern.ReplaceAllString(s, " ")
	return strings.TrimSpace(html.UnescapeString(text))
}

// evaluateeLabel 返回错误提示用的被评估人展示名；姓名缺失（empcap 未就绪）时退化为关系 id。
func evaluateeLabel(name string, relationID int64) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("#%d", relationID)
}
