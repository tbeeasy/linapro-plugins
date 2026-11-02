// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"
)

// QuestionOption is the golang structure of table plugin_linapro_eval_360_question_option for DAO operations like Where/Data.
type QuestionOption struct {
	g.Meta     `orm:"table:plugin_linapro_eval_360_question_option, do:true"`
	Id         any              //
	QuestionId any              // 所属单选题 id
	Label      any              // 选项文案
	Score      *decimal.Decimal // 选项分值，NUMERIC
	AllowText  any              // 是否允许附文本评语：1=是
	Sort       any              // 选项排序
	CreatedAt  *time.Time       //
	UpdatedAt  *time.Time       //
}
