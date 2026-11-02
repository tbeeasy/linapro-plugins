// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Dimension is the golang structure for table dimension.
type Dimension struct {
	Id              int64      `json:"id"              orm:"id"               description:""`
	QuestionnaireId int64      `json:"questionnaireId" orm:"questionnaire_id" description:"所属问卷 id"`
	Name            string     `json:"name"            orm:"name"             description:"维度名称"`
	Sort            int        `json:"sort"            orm:"sort"             description:"维度排序"`
	CreatedAt       *time.Time `json:"createdAt"       orm:"created_at"       description:""`
	UpdatedAt       *time.Time `json:"updatedAt"       orm:"updated_at"       description:""`
}
