// lark_code.go 声明飞书登录集成的业务错误码。对调用方可见的失败
// （凭据缺失、authen 换票失败）都是 bizerr.Code，因此登录响应携带稳定的
// code 与 i18n messageKey；本地化文案位于 manifest/i18n/<locale>/error.json。

package lark

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeCredentialMissing：租户缺少完整的飞书应用凭据；登录
	// 按失败关闭处理，而不是回退到共享应用。
	CodeCredentialMissing = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_CREDENTIAL_MISSING",
		"lark app credential not configured for tenant",
		gcode.CodeNotImplemented,
	)
	// CodeLarkAppsConfigReadFailed：读取 lark_apps 配置时配置后端出错。
	CodeLarkAppsConfigReadFailed = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_APPS_CONFIG_READ_FAILED",
		"failed to read lark app configuration",
		gcode.CodeInternalError,
	)
	// CodeLarkAppsConfigInvalid：lark_apps 配置存在但结构非法（应为对象数组）。
	CodeLarkAppsConfigInvalid = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_APPS_CONFIG_INVALID",
		"lark app configuration is malformed",
		gcode.CodeInternalError,
	)
	// CodeAuthenFailed：飞书 authen access-token 换票返回失败。
	CodeAuthenFailed = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_AUTHEN_FAILED",
		"lark login exchange failed",
		gcode.CodeInvalidParameter,
	)
	// CodeContactFailed：飞书通讯录查询（部门）失败。这是软失败 ——
	// 登录继续但部门为空 —— 但原因会被包装用于日志。
	CodeContactFailed = bizerr.MustDefine(
		"PLUGIN_EMPLOYEE_CORE_LARK_CONTACT_FAILED",
		"lark contact lookup failed",
		gcode.CodeInvalidParameter,
	)
)
