// =================================================================================
// 代码由 GoFrame CLI 工具生成并维护。请勿编辑。
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Employee 是表 plugin_linapro_employee_core_employee 的 Go 结构体，用于 Where/Data 等 DAO 操作。
type Employee struct {
	g.Meta         `orm:"table:plugin_linapro_employee_core_employee, do:true"`
	Id             any        // 内部 EmpID：与身份提供方无关的代理主键，所有业务数据引用此值
	TenantId       any        // 所属租户；同一自然人在两个租户中是两条记录
	Name           any        // 员工姓名；Moka 为权威来源，登录时不覆盖
	MokaEmployeeNo any        // Moka HR 系统工号（如 GZ001198）；同租户内非空值唯一，是员工行存在的稳定锚点；未接入 Moka 时为空字符串
	Status         any        // 在职状态：1=在职，0=离职；EmpAuth 每次请求查询此字段以实时拦截离职员工
	SysUserId      any        // 可选关联：该员工同时是后台管理员时回填对应 sys_user.id；否则为 0
	CreatedAt      *time.Time // 创建时间
	UpdatedAt      *time.Time // 更新时间
	DeletedAt      *time.Time // 软删除时间；存活行为 NULL
}
