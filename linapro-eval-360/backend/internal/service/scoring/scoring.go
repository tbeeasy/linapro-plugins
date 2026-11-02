// Package scoring 实现 360 测评本期「逐格取分」逻辑（eval-scoring）。
//
// 本期计分仅指：把一条答案映射为其所选选项的 score（单选取选项分值、矩阵取该行所选列分值），
// 供「按得分导出」使用。IsNA 仅代表「未作答 / 未命中」，不参与取分；
// 导出侧对 IsNA 与 0 分（无法评价）一律留空（FB-24），本包只负责区分二者、不决定展示。
//
// 三层聚合（题目分/维度分/个人总分）、角色加权、缺角色计 0、精度舍入——全部放第二期，
// 本包不实现。未命中与 0 分严格区分（见 scoring_test.go）。
package scoring

import (
	"github.com/shopspring/decimal"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// CellScore 是单格的取分结果。
// 当答案缺失（未作答）或未命中任何选项/列时，IsNA=true、Score=零值；
// 否则 IsNA=false、Score 为所选选项/列的分值。
type CellScore struct {
	IsNA  bool
	Score decimal.Decimal
}

// OptionScoreByAnswer 按单选题答案取分。
// answer.OptionId 是所选 question_option.id；options 是该题全部选项。
// 命中选项 → 其 score；未命中（答案缺失或指向已删除选项）→ IsNA。
func OptionScoreByAnswer(answer *entitymodel.Answer, options []*entitymodel.QuestionOption) CellScore {
	if answer == nil {
		return CellScore{IsNA: true}
	}
	for _, o := range options {
		if o == nil || o.Id != answer.OptionId {
			continue
		}
		return CellScore{IsNA: false, Score: derefScore(o.Score)}
	}
	// 选项未命中（可能答案指向已删除选项，理论上策略①白名单兜住，但取分需防御）。
	return CellScore{IsNA: true}
}

// MatrixScoreByAnswer 按矩阵题某行答案取分。
// answer.OptionId 是该行所选 matrix_column.id；columns 是该题全部列。
// 命中列 → 其 score；未命中（答案缺失）→ IsNA。
func MatrixScoreByAnswer(answer *entitymodel.Answer, columns []*entitymodel.MatrixColumn) CellScore {
	if answer == nil {
		return CellScore{IsNA: true}
	}
	for _, c := range columns {
		if c == nil || c.Id != answer.OptionId {
			continue
		}
		return CellScore{IsNA: false, Score: derefScore(c.Score)}
	}
	return CellScore{IsNA: true}
}

// derefScore 把 *decimal.Decimal 安全解引用，nil 记 0。
func derefScore(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}
