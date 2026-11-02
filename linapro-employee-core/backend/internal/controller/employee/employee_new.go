// employee_new.go 定义后台「通讯录管理」控制器与构造函数。
//
// 该控制器依赖三类东西：本插件自己的员工服务（读写花名册）、运行期配置
// （飞书应用清单，用于绑定前的归属校验）、以及宿主的租户 / 用户能力
// （补租户名与后台账号候选）。全部由 plugin.go 在路由注册期注入。
package employee

import (
	"context"

	empemployeeapi "lina-plugin-linapro-employee-core/backend/api/employee"

	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-core/pkg/plugin/capability/tenantcap"
	"lina-core/pkg/plugin/capability/usercap"
	"lina-plugin-linapro-employee-core/backend/internal/service/employee"
	"linapro-lark-sdk/larkcontact"
)

// ControllerV1 是后台员工列表 / 详情 / 编辑控制器。
type ControllerV1 struct {
	// employees 是进程共享的员工服务，与 empcap 门面、同步任务共用同一实例。
	employees *employee.Service
	// hostConfig 用于读取飞书应用清单（只读 app_id，不取密钥）。
	hostConfig hostconfigcap.Service
	// tenants 用于批量补租户名；宿主未绑定时为 nil，此时租户名留空。
	tenants tenantcap.Service
	// users 用于后台账号候选择与可见性校验；宿主未绑定时为 nil。
	users usercap.Service
	// contactClient 按 app_id 构造飞书通讯录客户端，绑定前校验 open_id 是否存在。
	// 抽成函数类型便于测试打桩。
	contactClient ContactClientFactory
}

// ContactClientFactory 按飞书应用凭据构造通讯录查询器。
type ContactClientFactory func(appID, appSecret string) ContactClient

// ContactClient 是绑定前校验所需的飞书通讯录能力。
type ContactClient interface {
	// GetUser 按 open_id 查询成员；查不到时返回 larkcontact.ErrUserNotFound。
	GetUser(ctx context.Context, openID string) (larkcontact.User, error)
}

// 编译期断言：控制器实现生成的接口契约，接口增删方法时在此处暴露。
var _ empemployeeapi.IEmployeeV1 = (*ControllerV1)(nil)

// NewV1 构造后台员工控制器。
func NewV1(
	employees *employee.Service,
	hostConfig hostconfigcap.Service,
	tenants tenantcap.Service,
	users usercap.Service,
	contactClient ContactClientFactory,
) *ControllerV1 {
	return &ControllerV1{
		employees:     employees,
		hostConfig:    hostConfig,
		tenants:       tenants,
		users:         users,
		contactClient: contactClient,
	}
}
