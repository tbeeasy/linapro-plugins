// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Submission is the golang structure for table submission.
type Submission struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	RelationId      int64      `json:"relationId"      orm:"relation_id"      description:"评价关系 id"`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	EvaluateeId     int64      `json:"evaluateeId"     orm:"evaluatee_id"     description:"被评估人 id"`
	EvaluatorEmpId  int64      `json:"evaluatorEmpId"  orm:"evaluator_emp_id" description:"评价者 EmpID"`
	DurationSeconds int        `json:"durationSeconds" orm:"duration_seconds" description:"作答时长（前端计时上传，后端原样存）"`
	SubmittedAt     *time.Time `json:"submittedAt"     orm:"submitted_at"     description:"提交时间"`
}
