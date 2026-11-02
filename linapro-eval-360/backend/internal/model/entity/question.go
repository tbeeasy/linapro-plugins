// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Question is the golang structure for table question.
type Question struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	DimensionId     int64      `json:"dimensionId"     orm:"dimension_id"     description:"所属维度 id（每题必须挂维度）"`
	Type            string     `json:"type"            orm:"type"             description:"题型：single_choice|text_short|text_long|matrix"`
	Title           string     `json:"title"           orm:"title"            description:"题目标题（富文本 HTML）"`
	Description     string     `json:"description"     orm:"description"      description:"题目描述（富文本 HTML，可空）"`
	Required        int        `json:"required"        orm:"required"         description:"是否必填：0=否 1=是"`
	Sort            int        `json:"sort"            orm:"sort"             description:"题目排序"`
	CreatedAt       *time.Time `json:"createdAt"       orm:"created_at"       description:""`
	UpdatedAt       *time.Time `json:"updatedAt"       orm:"updated_at"       description:""`
}
