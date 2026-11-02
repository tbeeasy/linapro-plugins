// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Relation is the golang structure for table relation.
type Relation struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	ProjectId       int64      `json:"projectId"       orm:"project_id"       description:"所属项目 id"`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id（必须在唯一键内）"`
	EvaluateeId     int64      `json:"evaluateeId"     orm:"evaluatee_id"     description:"被评估人 evaluatee.id"`
	EvaluatorEmpId  int64      `json:"evaluatorEmpId"  orm:"evaluator_emp_id" description:"评价者 EmpID"`
	RoleId          int64      `json:"roleId"          orm:"role_id"          description:"角色 role.id（属性，不进唯一键）"`
	Source          string     `json:"source"          orm:"source"           description:"来源：import=全量导入 manual=关系编辑器手动新增"`
	Completed       int        `json:"completed"       orm:"completed"        description:"完成状态：0=未填 1=已填；派生已作废不落库"`
	CompletedAt     *time.Time `json:"completedAt"     orm:"completed_at"     description:"首次提交时间"`
	CreatedAt       *time.Time `json:"createdAt"       orm:"created_at"       description:""`
	UpdatedAt       *time.Time `json:"updatedAt"       orm:"updated_at"       description:""`
}
