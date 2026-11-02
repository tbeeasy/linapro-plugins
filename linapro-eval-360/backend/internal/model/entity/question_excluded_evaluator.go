// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// QuestionExcludedEvaluator is the golang structure for table question_excluded_evaluator.
type QuestionExcludedEvaluator struct {
	Id             int64      `json:"id"             orm:"id"               description:""`
	QuestionId     int64      `json:"questionId"     orm:"question_id"      description:"所属题目 id"`
	EvaluatorEmpId int64      `json:"evaluatorEmpId" orm:"evaluator_emp_id" description:"被排除的评价者 EmpID"`
	CreatedAt      *time.Time `json:"createdAt"      orm:"created_at"       description:""`
}
