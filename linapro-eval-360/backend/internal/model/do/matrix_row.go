// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// MatrixRow is the golang structure of table plugin_linapro_eval_360_matrix_row for DAO operations like Where/Data.
type MatrixRow struct {
	g.Meta     `orm:"table:plugin_linapro_eval_360_matrix_row, do:true"`
	Id         any        //
	QuestionId any        // 所属矩阵题 id
	Title      any        // 行标题
	Sort       any        // 行排序
	CreatedAt  *time.Time //
	UpdatedAt  *time.Time //
}
