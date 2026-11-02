// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// MatrixRow is the golang structure for table matrix_row.
type MatrixRow struct {
	Id         int64      `json:"id"         orm:"id"          description:""`
	QuestionId int64      `json:"questionId" orm:"question_id" description:"所属矩阵题 id"`
	Title      string     `json:"title"      orm:"title"       description:"行标题"`
	Sort       int        `json:"sort"       orm:"sort"        description:"行排序"`
	CreatedAt  *time.Time `json:"createdAt"  orm:"created_at"  description:""`
	UpdatedAt  *time.Time `json:"updatedAt"  orm:"updated_at"  description:""`
}
