// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"
)

// MatrixColumn is the golang structure of table plugin_linapro_eval_360_matrix_column for DAO operations like Where/Data.
type MatrixColumn struct {
	g.Meta             `orm:"table:plugin_linapro_eval_360_matrix_column, do:true"`
	Id                 any              //
	QuestionId         any              // 所属矩阵题 id
	Label              any              // 列文案
	Score              *decimal.Decimal // 列分值，NUMERIC
	Sort               any              // 列排序
	CreatedAt          *time.Time       //
	UpdatedAt          *time.Time       //
	AllowComment       any              // 是否允许附评语：0=否 1=是
	CommentRequired    any              // 评语是否必填：0=否 1=是（仅允许评语时生效，提交时校验）
	CommentPlaceholder any              // 评语输入框占位提示（提示语），可空
	CommentPresets     any              // 预设填空候选，JSON 数组文本；空=无预设（自由输入）
	CommentAllowCustom any              // 是否允许自定义输入：0=只能选预设 1=可自定义（仅在有预设时生效）
}
