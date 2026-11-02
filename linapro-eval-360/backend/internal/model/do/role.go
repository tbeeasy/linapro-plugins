// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"
)

// Role is the golang structure of table plugin_linapro_eval_360_role for DAO operations like Where/Data.
type Role struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_role, do:true"`
	Id              any              //
	QuestionnaireId any              // 所属问卷 id
	Name            any              // 角色名（归一化后精确匹配）
	Weight          *decimal.Decimal // 角色权重，NUMERIC；发布前每角色 >0 且和=100%
	CreatedAt       *time.Time       //
	UpdatedAt       *time.Time       //
}
