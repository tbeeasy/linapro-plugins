// empcap_code.go 定义 empcap 接缝各处返回的共享业务错误，
// 使消费方和控制器无需导入拥有方插件的内部包即可匹配它们。每个错误都是
// bizerr.Code，因此对调用方可见的响应携带稳定的 errorCode、派生的 i18n
// messageKey 以及英文回退文案；本地化文案位于 manifest/i18n/<locale>/error.json。

package empcap

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

// 业务错误定义。运行时 i18n messageKey 由 bizerr 从 errorCode 派生
// （例如 PLUGIN_EMPLOYEE_CORE_UNAUTHORIZED ->
// error.plugin.employee.core.unauthorized）。
var (
	// CodeEmployeeNotFound：没有匹配的员工且不允许创建
	// （白名单模式），或查找无结果。
	CodeEmployeeNotFound = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_EMPLOYEE_NOT_FOUND",
		"employee not found",
		gcode.CodeNotFound,
	)
	// CodeUnauthorized：EmpAuth 拒绝了缺失/无效的 token 或离职员工。
	CodeUnauthorized = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_UNAUTHORIZED",
		"employee authentication failed",
		gcode.CodeNotAuthorized,
	)
	// CodeServiceUnbound：员工服务尚未绑定（拥有方插件被禁用或未安装）。
	CodeServiceUnbound = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_SERVICE_UNBOUND",
		"employee service unbound",
		gcode.CodeNotImplemented,
	)
	// CodeTokenSecretUnavailable：员工 JWT 签名密钥未配置；
	// 签名与校验按失败关闭处理，而不是使用空密钥。
	CodeTokenSecretUnavailable = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_TOKEN_SECRET_UNAVAILABLE",
		"employee token secret unavailable",
		gcode.CodeNotImplemented,
	)
	// CodeLarkIdentityTaken：要绑定的飞书身份已映射到另一名员工。
	CodeLarkIdentityTaken = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_IDENTITY_TAKEN",
		"lark identity already bound to another employee",
		gcode.CodeInvalidParameter,
	)
	// CodeLarkIdentityNotFound：该员工名下没有要解绑的那条飞书身份。
	CodeLarkIdentityNotFound = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_IDENTITY_NOT_FOUND",
		"lark identity not found for this employee",
		gcode.CodeNotFound,
	)
	// CodeLarkUserNotFound：按 open_id 在该应用通讯录中查不到用户
	// （ID 无效、已删除或不在应用通讯录权限范围内）。
	CodeLarkUserNotFound = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_USER_NOT_FOUND",
		"lark user not found in app contact scope",
		gcode.CodeNotFound,
	)
	// CodeSysUserTaken：该后台账号已关联另一名员工。
	CodeSysUserTaken = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_SYS_USER_TAKEN",
		"system user already linked to another employee",
		gcode.CodeInvalidParameter,
	)
	// CodeInvalidParameter：后台员工编辑入参不合法（缺应用、缺 open_id 等）。
	CodeInvalidParameter = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_INVALID_PARAMETER",
		"invalid employee edit parameter",
		gcode.CodeInvalidParameter,
	)
)

// 导出的哨兵错误，通过 bizerr 创建，调用方可以继续用 errors.Is 匹配，
// 同时 HTTP/插件响应携带稳定的 code 与 i18n key。
var (
	// ErrEmployeeNotFound 包装 CodeEmployeeNotFound。
	ErrEmployeeNotFound = bizerr.NewCode(CodeEmployeeNotFound)
	// ErrUnauthorized 包装 CodeUnauthorized。
	ErrUnauthorized = bizerr.NewCode(CodeUnauthorized)
	// ErrServiceUnbound 包装 CodeServiceUnbound。
	ErrServiceUnbound = bizerr.NewCode(CodeServiceUnbound)
	// ErrTokenSecretUnavailable 包装 CodeTokenSecretUnavailable。
	ErrTokenSecretUnavailable = bizerr.NewCode(CodeTokenSecretUnavailable)
	// ErrLarkIdentityTaken 包装 CodeLarkIdentityTaken。
	ErrLarkIdentityTaken = bizerr.NewCode(CodeLarkIdentityTaken)
	// ErrLarkIdentityNotFound 包装 CodeLarkIdentityNotFound。
	ErrLarkIdentityNotFound = bizerr.NewCode(CodeLarkIdentityNotFound)
	// ErrLarkUserNotFound 包装 CodeLarkUserNotFound。
	ErrLarkUserNotFound = bizerr.NewCode(CodeLarkUserNotFound)
	// ErrSysUserTaken 包装 CodeSysUserTaken。
	ErrSysUserTaken = bizerr.NewCode(CodeSysUserTaken)
	// ErrInvalidParameter 包装 CodeInvalidParameter。
	ErrInvalidParameter = bizerr.NewCode(CodeInvalidParameter)
)
