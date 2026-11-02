// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Question is the golang structure of table plugin_linapro_eval_360_question for DAO operations like Where/Data.
type Question struct {
	g.Meta          `orm:"table:plugin_linapro_eval_360_question, do:true"`
	Id              any        //
	QuestionnaireId any        // 所属问卷 id
	DimensionId     any        // 所属维度 id（每题必须挂维度）
	Type            any        // 题型：single_choice|text_short|text_long|matrix
	Title           any        // 题目标题（富文本 HTML）
	Description     any        // 题目描述（富文本 HTML，可空）
	Required        any        // 是否必填：0=否 1=是
	Sort            any        // 题目排序
	CreatedAt       *time.Time //
	UpdatedAt       *time.Time //
}
