// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Answer is the golang structure of table plugin_linapro_eval_360_answer for DAO operations like Where/Data.
type Answer struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_answer, do:true"`
	Id              any        //
	SubmissionId    any        // 所属作答 id
	QuestionnaireId any        // 所属问卷 id
	QuestionId      any        // 所属题目 id
	RowId           any        // 矩阵题行 id，非矩阵为 0
	OptionId        any        // 单选选项 id / 矩阵列 id / 文本为 0
	TextValue       any        // 文本题答案 或 选项级评语（allow_text）
	CreatedAt       *time.Time //
}
