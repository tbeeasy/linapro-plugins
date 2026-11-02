// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"

	"github.com/shopspring/decimal"
)

// MatrixColumn is the golang structure for table matrix_column.
type MatrixColumn struct {
	Id                 int64            `json:"id"                 orm:"id"                   description:""`
	QuestionId         int64            `json:"questionId"         orm:"question_id"          description:"所属矩阵题 id"`
	Label              string           `json:"label"              orm:"label"                description:"列文案"`
	Score              *decimal.Decimal `json:"score"              orm:"score"                description:"列分值，NUMERIC"`
	Sort               int              `json:"sort"               orm:"sort"                 description:"列排序"`
	CreatedAt          *time.Time       `json:"createdAt"          orm:"created_at"           description:""`
	UpdatedAt          *time.Time       `json:"updatedAt"          orm:"updated_at"           description:""`
	AllowComment       int              `json:"allowComment"       orm:"allow_comment"        description:"是否允许附评语：0=否 1=是"`
	CommentRequired    int              `json:"commentRequired"    orm:"comment_required"     description:"评语是否必填：0=否 1=是（仅允许评语时生效，提交时校验）"`
	CommentPlaceholder string           `json:"commentPlaceholder" orm:"comment_placeholder"  description:"评语输入框占位提示（提示语），可空"`
	CommentPresets     string           `json:"commentPresets"     orm:"comment_presets"      description:"预设填空候选，JSON 数组文本；空=无预设（自由输入）"`
	CommentAllowCustom int              `json:"commentAllowCustom" orm:"comment_allow_custom" description:"是否允许自定义输入：0=只能选预设 1=可自定义（仅在有预设时生效）"`
}
