// export_impl_test.go 覆盖导出单元格取值：矩阵一题一列（答案 row_id=0）、按得分表 0 分/未作答留空与数字单元格。

package export

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"lina-core/pkg/excelutil"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// decimalPtr 由字符串构造分值指针（对应 NUMERIC 列的实体字段类型）。
func decimalPtr(v string) *decimal.Decimal {
	d := decimal.RequireFromString(v)
	return &d
}

// matrixQuestion 构造矩阵题：列 1=优秀(5)、2=良好(4)、3=待改进(0)、4=不计分(0.00)。
func matrixQuestion() *questionBundle {
	return &questionBundle{
		Id:   10,
		Type: questionnaire.QuestionTypeMatrix,
		matrixColumns: []*entitymodel.MatrixColumn{
			{Id: 1, Label: "优秀", Score: decimalPtr("5")},
			{Id: 2, Label: "良好", Score: decimalPtr("4")},
			{Id: 3, Label: "待改进", Score: decimalPtr("0")},
			{Id: 4, Label: "不计分", Score: decimalPtr("0.00")},
		},
	}
}

// 按得分表（FB-24）：非 0 分写数字；0 分（无法评价，含 0.00）、未作答、选项已删除一律留空。
func TestScoreCellValue_MatrixBlankUnlessScored(t *testing.T) {
	q := matrixQuestion()
	cases := []struct {
		name    string
		answers []*entitymodel.Answer
		want    any
		wantOK  bool
	}{
		{"非 0 分为数字", []*entitymodel.Answer{{QuestionId: 10, OptionId: 1}}, int64(5), true},
		{"0 分留空", []*entitymodel.Answer{{QuestionId: 10, OptionId: 3}}, nil, false},
		{"0.00 留空", []*entitymodel.Answer{{QuestionId: 10, OptionId: 4}}, nil, false},
		{"未作答留空", nil, nil, false},
		{"选项已删除留空", []*entitymodel.Answer{{QuestionId: 10, OptionId: 99}}, nil, false},
	}
	for _, c := range cases {
		got, ok := scoreCellValue(q, c.answers)
		if ok != c.wantOK || got != c.want {
			t.Errorf("%s: got (%v,%v), want (%v,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

// 按得分表（FB-24）：单选 0 分（无法评价）同样留空，与矩阵口径一致；带小数分值写 float64。
func TestScoreCellValue_SingleChoice(t *testing.T) {
	q := &questionBundle{
		Id:   20,
		Type: questionnaire.QuestionTypeSingleChoice,
		options: []*entitymodel.QuestionOption{
			{Id: 1, Label: "无法评价", Score: decimalPtr("0")},
			{Id: 2, Label: "良好", Score: decimalPtr("4.00")},
			{Id: 3, Label: "较好", Score: decimalPtr("3.5")},
		},
	}
	if got, ok := scoreCellValue(q, []*entitymodel.Answer{{QuestionId: 20, OptionId: 1}}); ok {
		t.Fatalf("单选 0 分应留空，got %v", got)
	}
	if got, ok := scoreCellValue(q, []*entitymodel.Answer{{QuestionId: 20, OptionId: 2}}); !ok || got != int64(4) {
		t.Fatalf("单选 4.00 应为整数 4，got (%v,%v)", got, ok)
	}
	if got, ok := scoreCellValue(q, []*entitymodel.Answer{{QuestionId: 20, OptionId: 3}}); !ok || got != 3.5 {
		t.Fatalf("单选 3.5 应为浮点 3.5，got (%v,%v)", got, ok)
	}
}

// 按得分表写入后读回（FB-24）：分值格为数字类型（Excel 可直接求和），留空格无任何内容。
func TestScoreCellValue_WrittenAsNumericCell(t *testing.T) {
	f := excelize.NewFile()
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("关闭 Excel 失败: %v", err)
		}
	})
	q := matrixQuestion()
	for row, answers := range [][]*entitymodel.Answer{
		{{QuestionId: 10, OptionId: 1}}, // 5 分
		{{QuestionId: 10, OptionId: 3}}, // 0 分
		nil,                             // 未作答
	} {
		val, ok := scoreCellValue(q, answers)
		if !ok {
			continue
		}
		if err := excelutil.SetCellValue(f, "Sheet1", 1, row+1, val); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	typ, err := f.GetCellType("Sheet1", "A1")
	if err != nil {
		t.Fatalf("读取单元格类型失败: %v", err)
	}
	if typ != excelize.CellTypeNumber && typ != excelize.CellTypeUnset {
		t.Fatalf("分值格应为数字类型，got %v", typ)
	}
	if v, _ := f.GetCellValue("Sheet1", "A1"); v != "5" {
		t.Fatalf("分值格应为 5，got %q", v)
	}
	for _, cell := range []string{"A2", "A3"} {
		if v, _ := f.GetCellValue("Sheet1", cell); v != "" {
			t.Fatalf("%s 应为空单元格，got %q", cell, v)
		}
	}
}

// 按答案表：矩阵一题一列，取该被评估人所选评价项文案。
func TestAnswerCellText_MatrixSingleColumn(t *testing.T) {
	q := matrixQuestion()
	if got := answerCellText(q, []*entitymodel.Answer{{QuestionId: 10, OptionId: 3}}); got != "待改进" {
		t.Fatalf("矩阵按答案应为所选列文案，got %q", got)
	}
	if got := answerCellText(q, nil); got != "" {
		t.Fatalf("未作答应为空，got %q", got)
	}
}

// 按答案表（FB-26）：矩阵评价项评语与单选同口径并入该格，以【】包裹；无评语时不加括号。
func TestAnswerCellText_MatrixWithComment(t *testing.T) {
	q := matrixQuestion()
	withComment := []*entitymodel.Answer{{QuestionId: 10, OptionId: 1, TextValue: "沟通能力强"}}
	if got := answerCellText(q, withComment); got != "优秀【沟通能力强】" {
		t.Fatalf("矩阵评语应并入单元格并以【】包裹，got %q", got)
	}
	// 无评语：只输出评价项文案，不出现空括号。
	noComment := []*entitymodel.Answer{{QuestionId: 10, OptionId: 1}}
	if got := answerCellText(q, noComment); got != "优秀" {
		t.Fatalf("无评语时不应加括号，got %q", got)
	}
}

// 前导列：不含「评价者」（FB-23），且题目列起点紧随前导列之后、无空列。
func TestLeadHeaders_NoEvaluator(t *testing.T) {
	want := []string{"序号", "提交时间", "所用时间", "关系角色", "被评价对象"}
	got := leadHeaders()
	if len(got) != len(want) {
		t.Fatalf("前导列应为 %v，got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("前导列应为 %v，got %v", want, got)
		}
	}
	if questionColStart != len(got)+1 {
		t.Fatalf("题目列应从紧随前导列的第 %d 列开始，got %d", len(got)+1, questionColStart)
	}
}

// 导出列：矩阵一题一列；按得分表剔除文本题。
func TestExportColumns_OneColumnPerQuestion(t *testing.T) {
	b := &exportBundle{dimensions: []*dimensionBundle{{questions: []*questionBundle{
		{Id: 1, Type: questionnaire.QuestionTypeSingleChoice},
		{Id: 2, Type: questionnaire.QuestionTypeMatrix},
		{Id: 3, Type: questionnaire.QuestionTypeTextShort},
		{Id: 4, Type: questionnaire.QuestionTypeTextLong},
	}}}}
	ids := func(cols []*questionBundle) []int64 {
		out := make([]int64, 0, len(cols))
		for _, c := range cols {
			out = append(out, c.Id)
		}
		return out
	}
	if got := ids(exportColumns(b, false)); len(got) != 4 || got[1] != 2 {
		t.Fatalf("按答案表应一题一列共 4 列，got %v", got)
	}
	if got := ids(exportColumns(b, true)); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("按得分表应只含单选与矩阵，got %v", got)
	}
}
