// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// QuestionVisibility is the golang structure for table question_visibility.
type QuestionVisibility struct {
	Id             int64      `json:"id"             orm:"id"               description:""`
	QuestionId     int64      `json:"questionId"     orm:"question_id"      description:"所属题目 id"`
	VisibleRoleIds string     `json:"visibleRoleIds" orm:"visible_role_ids" description:"可见角色 id 逗号分隔，空=所有角色可见"`
	CreatedAt      *time.Time `json:"createdAt"      orm:"created_at"       description:""`
	UpdatedAt      *time.Time `json:"updatedAt"      orm:"updated_at"       description:""`
}
