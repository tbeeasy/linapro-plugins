// export_impl.go 实现按答案 / 按得分两张明细导出。
//
// 两表共用：固定前导列（序号|提交时间|所用时间|关系角色|被评价对象）+ 题目动态列。
// 只导已提交的作答（一行 = 一条已填评价关系）。矩阵题一题一列：行即被评估人，
// 一条作答只对应一个被评估人，故该列就是其所选评价项（答案 row_id 恒为 0）。
package export

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/excelutil"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
	"lina-plugin-linapro-eval-360/backend/internal/service/scoring"
)

// htmlTagPattern 匹配 HTML 标签，用于把富文本标题还原为纯文本写入 Excel。
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// stripHTML 去除富文本 HTML 标签并还原实体，供导出单元格使用纯文本。
// 题目标题改用富文本（HTML）后，Excel 单元格只需纯文本内容。
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	// 块级标签转空格，避免相邻文本粘连（如 <p>A</p><p>B</p> → "A B"）。
	text := htmlTagPattern.ReplaceAllString(s, " ")
	text = html.UnescapeString(text)
	// 折叠连续空白并去首尾。
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

// 前导列（1-based）。
const (
	colSeq       = 1 // 序号
	colSubmitted = 2 // 提交时间
	colDuration  = 3 // 所用时间
	colRole      = 4 // 关系角色
	colEvaluatee = 5 // 被评价对象
	// 第 6 列起为题目列（一题一列）。
	questionColStart = 6
)

// ExportByAnswer 按答案导出：全部题原文；矩阵一题一列；选项级评语并入单元格【】包裹；开放题独立列；只导已提交。
func (s *serviceImpl) ExportByAnswer(ctx context.Context, questionnaireID int64) (data []byte, err error) {
	bundle, err := s.loadExportBundle(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer excelutil.CloseFile(ctx, f, &err)
	sheet := "Sheet1"

	// 表头：前导列 + 题目列（全部题型，一题一列）。
	headers := leadHeaders()
	cols := exportColumns(bundle, false)
	for _, q := range cols {
		headers = append(headers, stripHTML(q.Title))
	}
	for i, h := range headers {
		if e := excelutil.SetCellValue(f, sheet, i+1, 1, h); e != nil {
			return nil, e
		}
	}

	// 数据行：每条已提交评价关系一行。
	for i, sub := range bundle.submissions {
		row := i + 2
		rel := bundle.relations[sub.RelationId]
		if rel == nil {
			continue
		}
		// 前导列。
		if e := excelutil.SetCellValue(f, sheet, colSeq, row, i+1); e != nil {
			return nil, e
		}
		if sub.SubmittedAt != nil {
			if e := excelutil.SetCellValue(f, sheet, colSubmitted, row, sub.SubmittedAt.Format("2006-01-02 15:04:05")); e != nil {
				return nil, e
			}
		}
		if e := excelutil.SetCellValue(f, sheet, colDuration, row, fmt.Sprintf("%d秒", sub.DurationSeconds)); e != nil {
			return nil, e
		}
		if e := excelutil.SetCellValue(f, sheet, colRole, row, bundle.roleNames[rel.RoleId]); e != nil {
			return nil, e
		}
		if e := excelutil.SetCellValue(f, sheet, colEvaluatee, row, bundle.evalNames[rel.EvaluateeId]); e != nil {
			return nil, e
		}
		// 题目列。
		answersByQ := bundle.answers[sub.Id] // map[question_id][]*Answer
		for offset, q := range cols {
			cellCol := questionColStart + offset
			val := answerCellText(q, answersByQ[q.Id])
			if e := excelutil.SetCellValue(f, sheet, cellCol, row, val); e != nil {
				return nil, e
			}
		}
	}

	var buf bytes.Buffer
	if e := f.Write(&buf); e != nil {
		return nil, bizerr.WrapCode(e, CodeExportExcelFailed)
	}
	return buf.Bytes(), nil
}

// ExportByScore 按得分导出：仅含计分题（单选、矩阵）、数字单元格；一题一列；0 分（无法评价）与未作答均留空；不含文本题与评语；只导已提交。
func (s *serviceImpl) ExportByScore(ctx context.Context, questionnaireID int64) (data []byte, err error) {
	bundle, err := s.loadExportBundle(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer excelutil.CloseFile(ctx, f, &err)
	sheet := "Sheet1"

	// 表头：前导列 + 仅计分题（单选、矩阵，一题一列）。
	headers := leadHeaders()
	cols := exportColumns(bundle, true)
	for _, q := range cols {
		headers = append(headers, stripHTML(q.Title))
	}
	for i, h := range headers {
		if e := excelutil.SetCellValue(f, sheet, i+1, 1, h); e != nil {
			return nil, e
		}
	}

	for i, sub := range bundle.submissions {
		row := i + 2
		rel := bundle.relations[sub.RelationId]
		if rel == nil {
			continue
		}
		// 前导列（同按答案表）。
		if e := excelutil.SetCellValue(f, sheet, colSeq, row, i+1); e != nil {
			return nil, e
		}
		if sub.SubmittedAt != nil {
			if e := excelutil.SetCellValue(f, sheet, colSubmitted, row, sub.SubmittedAt.Format("2006-01-02 15:04:05")); e != nil {
				return nil, e
			}
		}
		if e := excelutil.SetCellValue(f, sheet, colDuration, row, fmt.Sprintf("%d秒", sub.DurationSeconds)); e != nil {
			return nil, e
		}
		if e := excelutil.SetCellValue(f, sheet, colRole, row, bundle.roleNames[rel.RoleId]); e != nil {
			return nil, e
		}
		if e := excelutil.SetCellValue(f, sheet, colEvaluatee, row, bundle.evalNames[rel.EvaluateeId]); e != nil {
			return nil, e
		}
		// 计分题列：有效分值写数字单元格；0 分（无法评价）、未作答一律不写，保持空单元格。
		answersByQ := bundle.answers[sub.Id]
		for offset, q := range cols {
			val, ok := scoreCellValue(q, answersByQ[q.Id])
			if !ok {
				continue
			}
			if e := excelutil.SetCellValue(f, sheet, questionColStart+offset, row, val); e != nil {
				return nil, e
			}
		}
	}

	var buf bytes.Buffer
	if e := f.Write(&buf); e != nil {
		return nil, bizerr.WrapCode(e, CodeExportExcelFailed)
	}
	return buf.Bytes(), nil
}

// leadHeaders 返回固定前导列（不含评价者，见 FB-23）。
func leadHeaders() []string {
	return []string{"序号", "提交时间", "所用时间", "关系角色", "被评价对象"}
}

// exportColumns 按维度、题目顺序返回导出题目列（一题一列）。
// scoredOnly=true 时只取计分题（单选、矩阵），供按得分表使用。
func exportColumns(b *exportBundle, scoredOnly bool) []*questionBundle {
	cols := make([]*questionBundle, 0)
	for _, d := range b.dimensions {
		for _, q := range d.questions {
			switch q.Type {
			case questionnaire.QuestionTypeSingleChoice, questionnaire.QuestionTypeMatrix:
				cols = append(cols, q)
			case questionnaire.QuestionTypeTextShort, questionnaire.QuestionTypeTextLong:
				if !scoredOnly {
					cols = append(cols, q)
				}
			}
		}
	}
	return cols
}

// questionAnswer 取该题本条作答的唯一答案（row_id 恒为 0），未作答返回 nil。
func questionAnswer(answers []*entitymodel.Answer) *entitymodel.Answer {
	for _, a := range answers {
		if a != nil && a.RowId == 0 {
			return a
		}
	}
	return nil
}

// answerCellText 按答案表单格的展示文本。
//   - 单选：选项文案 +（如有评语）【评语】。
//   - 矩阵：所选评价项（列）文案 +（如有评语）【评语】，与单选同一口径。
//   - 文本题：文本原文。
//   - 未作答：空。
func answerCellText(q *questionBundle, answers []*entitymodel.Answer) string {
	a := questionAnswer(answers)
	if q == nil || a == nil {
		return ""
	}
	switch q.Type {
	case questionnaire.QuestionTypeSingleChoice:
		// 答案 OptionId 指向 question_option.id。
		for _, o := range q.options {
			if o.Id == a.OptionId {
				text := o.Label
				if a.TextValue != "" {
					text = fmt.Sprintf("%s【%s】", text, a.TextValue)
				}
				return text
			}
		}
		return ""
	case questionnaire.QuestionTypeTextShort, questionnaire.QuestionTypeTextLong:
		return a.TextValue
	case questionnaire.QuestionTypeMatrix:
		// 答案 OptionId 指向 matrix_column.id；评价项评语同样并入该格、以【】包裹（与单选口径一致）。
		for _, col := range q.matrixColumns {
			if col.Id == a.OptionId {
				text := col.Label
				if a.TextValue != "" {
					text = fmt.Sprintf("%s【%s】", text, a.TextValue)
				}
				return text
			}
		}
		return ""
	}
	return ""
}

// scoreCellValue 返回按得分表单格要写入的分值，ok=false 表示该格留空（不写入，保持真正的空单元格）。
//   - 命中非 0 分：数字（整数分写 int64，带小数写 float64），Excel 可直接求和/求均值。
//   - 其余一律留空（FB-24）：0 分即「无法评价」档（问卷不设 0 分的正常选项），单选与矩阵口径一致；
//     未作答、答案指向已删除选项同样留空，导出中不出现 N/A 文本。
func scoreCellValue(q *questionBundle, answers []*entitymodel.Answer) (value any, ok bool) {
	a := questionAnswer(answers)
	if q == nil || a == nil {
		return nil, false
	}
	var cell scoring.CellScore
	switch q.Type {
	case questionnaire.QuestionTypeSingleChoice:
		cell = scoring.OptionScoreByAnswer(a, q.options)
	case questionnaire.QuestionTypeMatrix:
		cell = scoring.MatrixScoreByAnswer(a, q.matrixColumns)
	default:
		return nil, false
	}
	if cell.IsNA || cell.Score.IsZero() {
		return nil, false
	}
	if cell.Score.IsInteger() {
		return cell.Score.IntPart(), true
	}
	return cell.Score.InexactFloat64(), true
}
