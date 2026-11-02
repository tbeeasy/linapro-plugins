// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// QuestionVisibility is the golang structure of table plugin_linapro_eval_360_question_visibility for DAO operations like Where/Data.
type QuestionVisibility struct {
	g.Meta         `orm:"table:plugin_linapro_eval_360_question_visibility, do:true"`
	Id             any        //
	QuestionId     any        // 所属题目 id
	VisibleRoleIds any        // 可见角色 id 逗号分隔，空=所有角色可见
	CreatedAt      *time.Time //
	UpdatedAt      *time.Time //
}
