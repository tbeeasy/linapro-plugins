// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Questionnaire is the golang structure of table plugin_linapro_eval_360_questionnaire for DAO operations like Where/Data.
type Questionnaire struct {
	g.Meta      `orm:"table:plugin_linapro_eval_360_questionnaire, do:true"`
	Id          any        //
	ProjectId   any        // 所属项目 id
	Title       any        //
	Description any        //
	Published   any        // 发布状态：0=未发布（不进评价者待办） 1=已发布
	Sort        any        // 问卷排序
	CreatedAt   *time.Time //
	UpdatedAt   *time.Time //
}
