// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"

	"github.com/shopspring/decimal"
)

// QuestionOption is the golang structure for table question_option.
type QuestionOption struct {
	Id         int64            `json:"id"         orm:"id"          description:""`
	QuestionId int64            `json:"questionId" orm:"question_id" description:"所属单选题 id"`
	Label      string           `json:"label"      orm:"label"       description:"选项文案"`
	Score      *decimal.Decimal `json:"score"      orm:"score"       description:"选项分值，NUMERIC"`
	AllowText  int              `json:"allowText"  orm:"allow_text"  description:"是否允许附文本评语：1=是"`
	Sort       int              `json:"sort"       orm:"sort"        description:"选项排序"`
	CreatedAt  *time.Time       `json:"createdAt"  orm:"created_at"  description:""`
	UpdatedAt  *time.Time       `json:"updatedAt"  orm:"updated_at"  description:""`
}
