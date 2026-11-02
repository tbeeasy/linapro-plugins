// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// Project is the golang structure for table project.
type Project struct {
	Id          int64      `json:"id"          orm:"id"          description:""`
	Name        string     `json:"name"        orm:"name"        description:"项目名称"`
	Description string     `json:"description" orm:"description" description:"项目描述"`
	StartAt     *time.Time `json:"startAt"     orm:"start_at"    description:"评估开始时间"`
	EndAt       *time.Time `json:"endAt"       orm:"end_at"      description:"项目统一截止时间；超过后拒绝提交"`
	ClosedAt    *time.Time `json:"closedAt"    orm:"closed_at"   description:"提前结束时间戳，空=未提前结束；清空即重新开启；不改 end_at"`
	CreatedBy   int        `json:"createdBy"   orm:"created_by"  description:"创建管理员 sys_user.id"`
	CreatedAt   *time.Time `json:"createdAt"   orm:"created_at"  description:""`
	UpdatedAt   *time.Time `json:"updatedAt"   orm:"updated_at"  description:""`
}
