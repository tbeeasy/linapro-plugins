// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Relation is the golang structure of table plugin_linapro_eval_360_relation for DAO operations like Where/Data.
type Relation struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_relation, do:true"`
	Id              any        //
	ProjectId       any        // 所属项目 id
	QuestionnaireId any        // 所属问卷 id（必须在唯一键内）
	EvaluateeId     any        // 被评估人 evaluatee.id
	EvaluatorEmpId  any        // 评价者 EmpID
	RoleId          any        // 角色 role.id（属性，不进唯一键）
	Source          any        // 来源：import=全量导入 manual=关系编辑器手动新增
	Completed       any        // 完成状态：0=未填 1=已填；派生已作废不落库
	CompletedAt     *time.Time // 首次提交时间
	CreatedAt       *time.Time //
	UpdatedAt       *time.Time //
}
