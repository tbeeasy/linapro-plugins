// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Evaluatee is the golang structure for table evaluatee.
type Evaluatee struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	ProjectId       int64      `json:"projectId"       orm:"project_id"       description:"所属项目 id（冗余便于查询）"`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	EvaluateeEmpId  int64      `json:"evaluateeEmpId"  orm:"evaluatee_emp_id" description:"被评估员工 EmpID（linapro-employee-core）"`
	CreatedAt       *time.Time `json:"createdAt"       orm:"created_at"       description:""`
	UpdatedAt       *time.Time `json:"updatedAt"       orm:"updated_at"       description:""`
	JobTitle        string     `json:"jobTitle"        orm:"job_title"        description:"被评估人职务（导入/新增弹窗录入，可空）"`
	Department      string     `json:"department"      orm:"department"       description:"被评估人部门（导入/新增弹窗录入，可空）"`
}
