// whitelist_check.go 抽出编辑策略①「保留答案」白名单的纯比较逻辑，
// 使其在不触库的前提下可被单测覆盖（见 tasks 11.1）。
//
// 策略①白名单：存在作答时仅允许改文案 / 新增 / 调排序，
// 拒绝改分值、删选项 / 列 / 行。新增选项与改文案放行。
package questionnaire

import (
	"lina-core/pkg/bizerr"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// checkOptionWhitelist 校验单选题在存在作答时的白名单约束。
//   - 已有选项的分值不得变更（改文案、调排序放行）；
//   - 已有选项不得删除；
//   - 新增选项（Id<=0）放行。
//
// existing 为该题当前全部选项（live 结构），input 为本次编辑提交的选项集合。
func checkOptionWhitelist(existing []*entitymodel.QuestionOption, input []parsedOption) error {
	existingMap := make(map[int64]*entitymodel.QuestionOption, len(existing))
	for _, r := range existing {
		if r == nil {
			continue
		}
		existingMap[r.Id] = r
	}
	inputIDs := make(map[int64]struct{}, len(input))
	for _, p := range input {
		if p.Id <= 0 {
			continue // 新增选项：允许。
		}
		inputIDs[p.Id] = struct{}{}
		eo := existingMap[p.Id]
		if eo == nil {
			return bizerr.NewCode(CodeEditRequiresStrategy)
		}
		if !decimalEqual(eo.Score, p.Score) {
			return bizerr.NewCode(CodeEditRequiresStrategy) // 禁改分值
		}
	}
	for id := range existingMap {
		if _, ok := inputIDs[id]; !ok {
			return bizerr.NewCode(CodeEditRequiresStrategy) // 禁删选项
		}
	}
	return nil
}

// checkMatrixWhitelist 校验矩阵题在存在作答时的白名单约束。
//   - 列：禁改分值、禁删列（改文案、加列放行）。
//
// 矩阵已无手填行（行由被评估人清单动态渲染），故不再校验行。
// existingCols 为该题当前全部列，inputCols 为本次提交的列集合。
func checkMatrixWhitelist(
	existingCols []*entitymodel.MatrixColumn,
	inputCols []parsedColumn,
) error {
	colMap := make(map[int64]*entitymodel.MatrixColumn, len(existingCols))
	for _, c := range existingCols {
		if c == nil {
			continue
		}
		colMap[c.Id] = c
	}
	inputColIDs := make(map[int64]struct{}, len(inputCols))
	for _, p := range inputCols {
		if p.Id <= 0 {
			continue
		}
		inputColIDs[p.Id] = struct{}{}
		ec := colMap[p.Id]
		if ec == nil {
			return bizerr.NewCode(CodeEditRequiresStrategy)
		}
		if !decimalEqual(ec.Score, p.Score) {
			return bizerr.NewCode(CodeEditRequiresStrategy) // 禁改分值
		}
	}
	for id := range colMap {
		if _, ok := inputColIDs[id]; !ok {
			return bizerr.NewCode(CodeEditRequiresStrategy) // 禁删列
		}
	}
	return nil
}
