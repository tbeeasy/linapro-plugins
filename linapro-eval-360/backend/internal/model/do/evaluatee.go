// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Evaluatee is the golang structure of table plugin_linapro_eval_360_evaluatee for DAO operations like Where/Data.
type Evaluatee struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_evaluatee, do:true"`
	Id              any        //
	ProjectId       any        // 所属项目 id（冗余便于查询）
	QuestionnaireId any        // 所属问卷 id
	EvaluateeEmpId  any        // 被评估员工 EmpID（linapro-employee-core）
	CreatedAt       *time.Time //
	UpdatedAt       *time.Time //
	JobTitle        any        // 被评估人职务（导入/新增弹窗录入，可空）
	Department      any        // 被评估人部门（导入/新增弹窗录入，可空）
}
