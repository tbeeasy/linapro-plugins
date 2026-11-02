// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Questionnaire is the golang structure for table questionnaire.
type Questionnaire struct {
	Id          int64      `json:"id"          orm:"id"          description:""`
	ProjectId   int64      `json:"projectId"   orm:"project_id"  description:"所属项目 id"`
	Title       string     `json:"title"       orm:"title"       description:""`
	Description string     `json:"description" orm:"description" description:""`
	Published   int        `json:"published"   orm:"published"   description:"发布状态：0=未发布（不进评价者待办） 1=已发布"`
	Sort        int        `json:"sort"        orm:"sort"        description:"问卷排序"`
	CreatedAt   *time.Time `json:"createdAt"   orm:"created_at"  description:""`
	UpdatedAt   *time.Time `json:"updatedAt"   orm:"updated_at"  description:""`
}
