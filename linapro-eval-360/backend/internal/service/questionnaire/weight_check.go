// weight_check.go 抽出发布前角色权重校验的纯比较逻辑，
// 使其在不触库的前提下可被单测覆盖（见 tasks 11.1）。
//
// 发布前校验：每个角色权重 > 0 且各角色权重之和 = 100%。
// 这是硬红线（无快照 + 直读 live 下，引用完整性与历史得分不变性完全依赖策略①白名单与权重校验）。
package questionnaire

import (
	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// weightTotal100 是各角色权重必须达到的目标之和（百分比）。
var weightTotal100 = decimal.NewFromInt(100)

// checkRoleWeights 校验角色权重：无角色拒、每个角色 > 0 且和 = 100%。
// 不读库、纯计算，供 validateRoleWeights 与单测共用。
func checkRoleWeights(roles []*entitymodel.Role) error {
	if len(roles) == 0 {
		return bizerr.NewCode(CodeNoRoles)
	}
	sum := decimal.Zero
	for _, r := range roles {
		w := decimal.Zero
		if r != nil && r.Weight != nil {
			w = *r.Weight
		}
		if !w.IsPositive() {
			return bizerr.NewCode(CodeWeightMissing)
		}
		sum = sum.Add(w)
	}
	if !sum.Equal(weightTotal100) {
		return bizerr.NewCode(CodeWeightSumInvalid)
	}
	return nil
}
