// whitelist_check_test.go 覆盖编辑策略①白名单强校验的纯比较逻辑（见 tasks 11.1）。
// 硬红线：存在作答时禁改分值、禁删选项/列/行；新增与改文案放行。
package questionnaire

import (
	"testing"

	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

func ptr(d decimal.Decimal) *decimal.Decimal { return &d }

// isWhitelistViolation 判断错误是否为策略①白名单拦截（错误码 CodeEditRequiresStrategy）。
func isWhitelistViolation(err error) bool {
	return bizerr.Is(err, CodeEditRequiresStrategy)
}

// --- 单选题选项白名单 ---

func TestCheckOptionWhitelist_ChangeScoreRejected(t *testing.T) {
	existing := []*entitymodel.QuestionOption{{Id: 1, Score: ptr(decimal.NewFromInt(5))}}
	input := []parsedOption{{Id: 1, Score: decimal.NewFromInt(6)}} // 改分值
	err := checkOptionWhitelist(existing, input)
	if !isWhitelistViolation(err) {
		t.Fatalf("改分值应被拒，got %v", err)
	}
}

func TestCheckOptionWhitelist_DeleteOptionRejected(t *testing.T) {
	existing := []*entitymodel.QuestionOption{
		{Id: 1, Score: ptr(decimal.NewFromInt(5))},
		{Id: 2, Score: ptr(decimal.NewFromInt(3))},
	}
	input := []parsedOption{{Id: 1, Score: decimal.NewFromInt(5)}} // 删了 id=2
	err := checkOptionWhitelist(existing, input)
	if !isWhitelistViolation(err) {
		t.Fatalf("删选项应被拒，got %v", err)
	}
}

func TestCheckOptionWhitelist_AddOptionAllowed(t *testing.T) {
	existing := []*entitymodel.QuestionOption{{Id: 1, Score: ptr(decimal.NewFromInt(5))}}
	input := []parsedOption{
		{Id: 1, Score: decimal.NewFromInt(5)}, // 原选项不变
		{Id: 0, Score: decimal.NewFromInt(2)}, // 新增选项
	}
	if err := checkOptionWhitelist(existing, input); err != nil {
		t.Fatalf("新增选项应放行，got %v", err)
	}
}

func TestCheckOptionWhitelist_ChangeLabelAllowed(t *testing.T) {
	existing := []*entitymodel.QuestionOption{{Id: 1, Score: ptr(decimal.NewFromInt(5)), Label: "旧文案"}}
	input := []parsedOption{{Id: 1, Score: decimal.NewFromInt(5), Label: "新文案"}} // 只改文案
	if err := checkOptionWhitelist(existing, input); err != nil {
		t.Fatalf("改文案应放行，got %v", err)
	}
}

func TestCheckOptionWhitelist_ZeroScoreKept(t *testing.T) {
	// 0 分值保持 0 是允许的（禁的是改成别的值）。
	existing := []*entitymodel.QuestionOption{{Id: 1, Score: ptr(decimal.Zero)}}
	input := []parsedOption{{Id: 1, Score: decimal.Zero}}
	if err := checkOptionWhitelist(existing, input); err != nil {
		t.Fatalf("0 分保持 0 应放行，got %v", err)
	}
}

// --- 矩阵题列白名单（矩阵已无手填行，行由被评估人清单动态渲染） ---

func TestCheckMatrixWhitelist_ChangeColumnScoreRejected(t *testing.T) {
	existingCols := []*entitymodel.MatrixColumn{{Id: 10, Score: ptr(decimal.NewFromInt(4))}}
	inputCols := []parsedColumn{{Id: 10, Score: decimal.NewFromInt(5)}} // 改列分值
	err := checkMatrixWhitelist(existingCols, inputCols)
	if !isWhitelistViolation(err) {
		t.Fatalf("矩阵列改分值应被拒，got %v", err)
	}
}

func TestCheckMatrixWhitelist_DeleteColumnRejected(t *testing.T) {
	existingCols := []*entitymodel.MatrixColumn{
		{Id: 10, Score: ptr(decimal.NewFromInt(4))},
		{Id: 11, Score: ptr(decimal.Zero)},
	}
	inputCols := []parsedColumn{{Id: 10, Score: decimal.NewFromInt(4)}} // 删了列 11
	err := checkMatrixWhitelist(existingCols, inputCols)
	if !isWhitelistViolation(err) {
		t.Fatalf("删列应被拒，got %v", err)
	}
}

func TestCheckMatrixWhitelist_AddColumnAllowed(t *testing.T) {
	existingCols := []*entitymodel.MatrixColumn{{Id: 10, Score: ptr(decimal.NewFromInt(4))}}
	inputCols := []parsedColumn{
		{Id: 10, Score: decimal.NewFromInt(4)},
		{Id: 0, Score: decimal.NewFromInt(3)}, // 新增列
	}
	if err := checkMatrixWhitelist(existingCols, inputCols); err != nil {
		t.Fatalf("新增列应放行，got %v", err)
	}
}

func TestCheckMatrixWhitelist_ChangeColumnLabelAllowed(t *testing.T) {
	existingCols := []*entitymodel.MatrixColumn{{Id: 10, Score: ptr(decimal.NewFromInt(4)), Label: "旧文案"}}
	inputCols := []parsedColumn{{Id: 10, Score: decimal.NewFromInt(4), Label: "新文案"}} // 只改列文案
	if err := checkMatrixWhitelist(existingCols, inputCols); err != nil {
		t.Fatalf("改列文案应放行，got %v", err)
	}
}
