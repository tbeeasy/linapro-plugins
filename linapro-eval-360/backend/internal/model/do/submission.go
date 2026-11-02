// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Submission is the golang structure of table plugin_linapro_eval_360_submission for DAO operations like Where/Data.
type Submission struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_submission, do:true"`
	Id              any        //
	RelationId      any        // 评价关系 id
	QuestionnaireId any        // 所属问卷 id
	EvaluateeId     any        // 被评估人 id
	EvaluatorEmpId  any        // 评价者 EmpID
	DurationSeconds any        // 作答时长（前端计时上传，后端原样存）
	SubmittedAt     *time.Time // 提交时间
}
