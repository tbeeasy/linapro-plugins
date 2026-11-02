// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Dimension is the golang structure of table plugin_linapro_eval_360_dimension for DAO operations like Where/Data.
type Dimension struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_dimension, do:true"`
	Id              any        //
	QuestionnaireId any        // 所属问卷 id
	Name            any        // 维度名称
	Sort            any        // 维度排序
	CreatedAt       *time.Time //
	UpdatedAt       *time.Time //
}
