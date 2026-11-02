// =================================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// =================================================================================

package entity

import (
	"time"
)

// Employee 是表 employee 的 Go 结构体。
type Employee struct {
	Id             int64      `json:"id"             orm:"id"               description:"内部 EmpID：与身份提供方无关的代理主键，所有业务数据引用此值"`
	TenantId       int        `json:"tenantId"       orm:"tenant_id"        description:"所属租户；同一自然人在两个租户中是两条记录"`
	Name           string     `json:"name"           orm:"name"             description:"员工姓名；Moka 为权威来源，登录时不覆盖"`
	MokaEmployeeNo string     `json:"mokaEmployeeNo" orm:"moka_employee_no" description:"Moka HR 系统工号（如 GZ001198）；同租户内非空值唯一，是员工行存在的稳定锚点；未接入 Moka 时为空字符串"`
	Status         int        `json:"status"         orm:"status"           description:"在职状态：1=在职，0=离职；EmpAuth 每次请求查询此字段以实时拦截离职员工"`
	SysUserId      int        `json:"sysUserId"      orm:"sys_user_id"      description:"可选关联：该员工同时是后台管理员时回填对应 sys_user.id；否则为 0"`
	CreatedAt      *time.Time `json:"createdAt"      orm:"created_at"       description:"创建时间"`
	UpdatedAt      *time.Time `json:"updatedAt"      orm:"updated_at"       description:"更新时间"`
	DeletedAt      *time.Time `json:"deletedAt"      orm:"deleted_at"       description:"软删除时间；存活行为 NULL"`
}
