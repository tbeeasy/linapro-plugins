// scoring_test.go 覆盖本期取分的硬红线：未命中（IsNA）与 0 分严格区分（tasks 6.2）。
package scoring

import (
	"testing"

	"github.com/shopspring/decimal"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// 单选题：选项分值为 0 时取分应为 0（而非 IsNA）。
func TestOptionScoreByAnswer_ZeroScoreIsNotNA(t *testing.T) {
	options := []*entitymodel.QuestionOption{
		{Id: 1, Score: ptrDecimal(decimal.Zero)},
		{Id: 2, Score: ptrDecimal(decimal.NewFromInt(5)), AllowText: 1},
	}
	answer := &entitymodel.Answer{OptionId: 1}
	cell := OptionScoreByAnswer(answer, options)
	if cell.IsNA {
		t.Fatalf("0 分选项不应记为 IsNA，got IsNA=true")
	}
	if !cell.Score.Equal(decimal.Zero) {
		t.Fatalf("0 分选项取分应为 0，got %s", cell.Score.String())
	}
}

// 单选题：选项分值为正取该分值。
func TestOptionScoreByAnswer_PositiveScore(t *testing.T) {
	options := []*entitymodel.QuestionOption{
		{Id: 2, Score: ptrDecimal(decimal.NewFromInt(5))},
	}
	answer := &entitymodel.Answer{OptionId: 2}
	cell := OptionScoreByAnswer(answer, options)
	if cell.IsNA {
		t.Fatalf("普通选项不应记为 N/A")
	}
	if !cell.Score.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("取分应为 5，got %s", cell.Score.String())
	}
}

// 单选题：答案指向不存在的选项（未命中）取 N/A。
func TestOptionScoreByAnswer_UnmatchedIsNA(t *testing.T) {
	options := []*entitymodel.QuestionOption{
		{Id: 2, Score: ptrDecimal(decimal.NewFromInt(5))},
	}
	answer := &entitymodel.Answer{OptionId: 999} // 指向已删除/不存在的选项
	cell := OptionScoreByAnswer(answer, options)
	if !cell.IsNA {
		t.Fatalf("未命中选项应记为 IsNA，got score=%s", cell.Score.String())
	}
}

// 矩阵题：某行选某列取该列分值。
func TestMatrixScoreByAnswer_PositiveScore(t *testing.T) {
	columns := []*entitymodel.MatrixColumn{
		{Id: 10, Score: ptrDecimal(decimal.NewFromInt(4))},
		{Id: 11, Score: ptrDecimal(decimal.Zero)},
	}
	answer := &entitymodel.Answer{OptionId: 10}
	cell := MatrixScoreByAnswer(answer, columns)
	if cell.IsNA {
		t.Fatalf("普通列不应记为 N/A")
	}
	if !cell.Score.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("取分应为 4，got %s", cell.Score.String())
	}
}

// 矩阵题：答案指向不存在的列（未命中）取 N/A。
func TestMatrixScoreByAnswer_UnmatchedIsNA(t *testing.T) {
	columns := []*entitymodel.MatrixColumn{
		{Id: 10, Score: ptrDecimal(decimal.NewFromInt(4))},
	}
	answer := &entitymodel.Answer{OptionId: 999}
	cell := MatrixScoreByAnswer(answer, columns)
	if !cell.IsNA {
		t.Fatalf("未命中列应记为 N/A，got score=%s", cell.Score.String())
	}
}

// nil 答案（未作答行）取 N/A，不取 0。
func TestOptionScoreByAnswer_NilAnswerIsNA(t *testing.T) {
	cell := OptionScoreByAnswer(nil, []*entitymodel.QuestionOption{})
	if !cell.IsNA {
		t.Fatalf("nil 答案应记为 N/A，不取 0")
	}
}

// ptrDecimal 把 decimal 转为指针（构造测试数据用）。
func ptrDecimal(d decimal.Decimal) *decimal.Decimal {
	return &d
}
