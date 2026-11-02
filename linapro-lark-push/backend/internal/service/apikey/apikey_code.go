// 本文件集中定义签名凭据管理的业务错误码；中英文案在 manifest/i18n/{zh-CN,en-US}/error.json。

package apikey

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeApiKeyNotFound 凭据不存在或已删除。
	CodeApiKeyNotFound = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_NOT_FOUND", "Signature credential does not exist or has been deleted", gcode.CodeNotFound,
	)
	// CodeApiKeyInvalidParam 凭据参数非法；参数 item 为具体原因。
	CodeApiKeyInvalidParam = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_INVALID_PARAM", "Invalid signature credential parameter: {item}", gcode.CodeInvalidParameter,
	)
	// CodeApiKeyAppKeyExists app_key 与另一条未删除凭据重复；参数 appKey 为重复值。
	CodeApiKeyAppKeyExists = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_APP_KEY_EXISTS", "app_key {appKey} already exists", gcode.CodeInvalidParameter,
	)
	// CodeApiKeyGroupNotFound 绑定的分组不在 push_groups 配置中；参数 group 为分组名。
	CodeApiKeyGroupNotFound = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_GROUP_NOT_FOUND", "Push group {group} is not configured in push_groups", gcode.CodeInvalidParameter,
	)
	// CodeApiKeyQueryFailed 查询凭据失败（数据库错误）。
	CodeApiKeyQueryFailed = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_QUERY_FAILED", "Failed to query signature credentials", gcode.CodeDbOperationError,
	)
	// CodeApiKeySaveFailed 保存凭据失败（数据库错误）。
	CodeApiKeySaveFailed = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_SAVE_FAILED", "Failed to save signature credential", gcode.CodeDbOperationError,
	)
	// CodeApiKeyDeleteFailed 删除凭据失败（数据库错误）。
	CodeApiKeyDeleteFailed = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_DELETE_FAILED", "Failed to delete signature credential", gcode.CodeDbOperationError,
	)
	// CodeApiKeyGroupsUnavailable 读取 push_groups 分组失败（宿主配置能力缺失或配置结构非法）。
	CodeApiKeyGroupsUnavailable = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_API_KEY_GROUPS_UNAVAILABLE", "Failed to read push groups from push_groups config", gcode.CodeInvalidConfiguration,
	)
)
