// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// QuestionExcludedEvaluator is the golang structure of table plugin_linapro_eval_360_question_excluded_evaluator for DAO operations like Where/Data.
type QuestionExcludedEvaluator struct {
	g.Meta         `orm:"table:plugin_linapro_eval_360_question_excluded_evaluator, do:true"`
	Id             any        //
	QuestionId     any        // 所属题目 id
	EvaluatorEmpId any        // 被排除的评价者 EmpID
	CreatedAt      *time.Time //
}
