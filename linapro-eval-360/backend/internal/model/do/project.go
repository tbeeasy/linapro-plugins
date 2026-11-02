// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Project is the golang structure of table plugin_linapro_eval_360_project for DAO operations like Where/Data.
type Project struct {
	g.Meta      `orm:"table:plugin_linapro_eval_360_project, do:true"`
	Id          any        //
	Name        any        // 项目名称
	Description any        // 项目描述
	StartAt     *time.Time // 评估开始时间
	EndAt       *time.Time // 项目统一截止时间；超过后拒绝提交
	ClosedAt    *time.Time // 提前结束时间戳，空=未提前结束；清空即重新开启；不改 end_at
	CreatedBy   any        // 创建管理员 sys_user.id
	CreatedAt   *time.Time //
	UpdatedAt   *time.Time //
}
