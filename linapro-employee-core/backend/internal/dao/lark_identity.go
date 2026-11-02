// =================================================================================
// 此文件由 GoFrame CLI 工具自动生成。可按需修改。
// =================================================================================

package dao

import (
	"lina-plugin-linapro-employee-core/backend/internal/dao/internal"
)

// larkIdentityDao 是表 plugin_linapro_employee_core_lark_identity 的数据访问对象。
// 你可以在其上定义自定义方法以按需扩展功能。
type larkIdentityDao struct {
	*internal.LarkIdentityDao
}

var (
	// LarkIdentity 是用于表 plugin_linapro_employee_core_lark_identity 操作的全局可访问对象。
	LarkIdentity = larkIdentityDao{internal.NewLarkIdentityDao()}
)

// 在下方添加你的自定义方法和功能。
