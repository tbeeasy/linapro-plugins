// weight_check_test.go 覆盖发布前角色权重校验的纯比较逻辑（见 tasks 11.1）。
// 发布前校验：无角色拒、每个角色权重 > 0 且和 = 100%。
package questionnaire

import (
	"testing"

	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

func isCode(err error, code *bizerr.Code) bool {
	return bizerr.Is(err, code)
}

func TestCheckRoleWeights_NoRolesRejected(t *testing.T) {
	if err := checkRoleWeights(nil); !isCode(err, CodeNoRoles) {
		t.Fatalf("无角色应拒，got %v", err)
	}
	if err := checkRoleWeights([]*entitymodel.Role{}); !isCode(err, CodeNoRoles) {
		t.Fatalf("空角色切片应拒，got %v", err)
	}
}

func TestCheckRoleWeights_ZeroWeightRejected(t *testing.T) {
	// 有角色权重为 0 → 拒（CodeWeightMissing）。
	roles := []*entitymodel.Role{
		{Id: 1, Weight: ptr(decimal.NewFromInt(50))},
		{Id: 2, Weight: ptr(decimal.Zero)}, // 权重 0
	}
	if err := checkRoleWeights(roles); !isCode(err, CodeWeightMissing) {
		t.Fatalf("存在 0 权重角色应拒，got %v", err)
	}
}

func TestCheckRoleWeights_NilWeightRejected(t *testing.T) {
	// 权重为 nil 视为 0 → 拒。
	roles := []*entitymodel.Role{{Id: 1, Weight: nil}}
	if err := checkRoleWeights(roles); !isCode(err, CodeWeightMissing) {
		t.Fatalf("nil 权重视为 0 应拒，got %v", err)
	}
}

func TestCheckRoleWeights_SumNot100Rejected(t *testing.T) {
	roles := []*entitymodel.Role{
		{Id: 1, Weight: ptr(decimal.NewFromInt(50))},
		{Id: 2, Weight: ptr(decimal.NewFromInt(30))}, // 和 80 ≠ 100
	}
	if err := checkRoleWeights(roles); !isCode(err, CodeWeightSumInvalid) {
		t.Fatalf("和 ≠ 100 应拒，got %v", err)
	}
}

func TestCheckRoleWeights_Sum100Allowed(t *testing.T) {
	roles := []*entitymodel.Role{
		{Id: 1, Weight: ptr(decimal.NewFromInt(50))},
		{Id: 2, Weight: ptr(decimal.NewFromInt(50))},
	}
	if err := checkRoleWeights(roles); err != nil {
		t.Fatalf("和 = 100 应放行，got %v", err)
	}
}

func TestCheckRoleWeights_DecimalWeightsAllowed(t *testing.T) {
	// 小数权重（33.33 + 33.33 + 33.34 = 100）应放行。
	roles := []*entitymodel.Role{
		{Id: 1, Weight: ptr(decimal.NewFromFloat(33.33))},
		{Id: 2, Weight: ptr(decimal.NewFromFloat(33.33))},
		{Id: 3, Weight: ptr(decimal.NewFromFloat(33.34))},
	}
	if err := checkRoleWeights(roles); err != nil {
		t.Fatalf("小数权重和=100 应放行，got %v", err)
	}
}

func TestCheckRoleWeights_SingleRole100(t *testing.T) {
	// 单一角色权重 100 = 100%，放行（一个角色承担全部权重）。
	roles := []*entitymodel.Role{{Id: 1, Weight: ptr(decimal.NewFromInt(100))}}
	if err := checkRoleWeights(roles); err != nil {
		t.Fatalf("单角色 100 应放行，got %v", err)
	}
}
