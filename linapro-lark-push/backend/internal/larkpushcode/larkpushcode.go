// Package larkpushcode 定义 linapro-lark-push 的业务错误码。
//
// 错误码按 bizerr 的约定给出，MessageKey 由错误码自动派生为扁平嵌套键
// （如 LINAPRO_LARK_PUSH_SENDER_UNBOUND → error.linapro.lark.push.sender.unbound），
// 对应的中英文案写在 manifest/i18n/{zh-CN,en-US}/error.json。
package larkpushcode

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeSenderUnbound 拥有方未绑定发送实现（插件未启用或装配失败）。
	CodeSenderUnbound = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SENDER_UNBOUND", "飞书推送能力未就绪", gcode.CodeNotImplemented,
	)
	// CodeNoPushApp 请求分组没有可用的推送应用（分组未配置，或该组所有条目凭据不全）。
	// 参数 group 为分组名；系统不会回退到其它分组。
	CodeNoPushApp = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_NO_PUSH_APP", "推送分组 {group} 没有可用的飞书推送应用，请检查 push_groups.{group} 配置", gcode.CodeInvalidConfiguration,
	)
	// CodeConfigInvalid 推送配置非法（结构无法解析，或请求分组内 app_id 重复、字段为空等）。
	// 参数 item 为第一条问题描述。
	CodeConfigInvalid = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_CONFIG_INVALID", "飞书配置非法：{item}", gcode.CodeInvalidConfiguration,
	)
	// CodeInvalidParam 请求参数非法（收件人为空、文本为空或超长、寻址方式不合法）。
	// 参数 item 为具体原因。
	CodeInvalidParam = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_INVALID_PARAM", "请求参数非法：{item}", gcode.CodeInvalidParameter,
	)
	// CodeEmpcapUnbound 员工能力（empcap）未绑定，无法把工号解析为企业级 user_id。
	CodeEmpcapUnbound = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_EMPCAP_UNBOUND", "员工能力服务未就绪，无法解析收件人", gcode.CodeNotImplemented,
	)
	// CodeHostConfigMissing 宿主配置能力不可用。
	CodeHostConfigMissing = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_HOST_CONFIG_MISSING", "宿主配置能力不可用", gcode.CodeNotImplemented,
	)
	// CodeCredentialStoreUnavailable 签名凭据表无法查询（数据库不可用或凭据服务未装配）。
	// 服务端 fail-closed：拒绝请求，不降级为放行。
	CodeCredentialStoreUnavailable = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_CREDENTIAL_STORE_UNAVAILABLE", "签名凭据存储不可用，暂时拒绝请求", gcode.CodeNotImplemented,
	)
	// CodeSignatureMissing 缺少签名头，或 nonce 长度不在 16~64 字节范围内。
	CodeSignatureMissing = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SIGNATURE_MISSING", "缺少签名头（须提供 X-App-Key / X-Timestamp / X-Nonce / X-Signature，nonce 长度 16~64）", gcode.CodeInvalidParameter,
	)
	// CodeSignatureUnknownAppKey 请求头里的 app_key 不对应任何启用且未删除的凭据。
	CodeSignatureUnknownAppKey = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY", "app_key 未知或未启用", gcode.CodeNotAuthorized,
	)
	// CodeSignatureTimestampInvalid 时间戳无法解析，或与服务器时间相差超过 ±300 秒。
	CodeSignatureTimestampInvalid = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SIGNATURE_TIMESTAMP_INVALID", "时间戳无效或超出 ±300 秒窗口，请检查调用方时钟", gcode.CodeNotAuthorized,
	)
	// CodeSignatureMismatch 重算签名与请求携带的签名不一致（含请求体、路径、查询串被篡改）。
	CodeSignatureMismatch = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH", "签名不匹配", gcode.CodeNotAuthorized,
	)
	// CodeSignatureNonceReplayed 同一 app_key 的 nonce 在有效窗口内被重复使用。
	CodeSignatureNonceReplayed = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SIGNATURE_NONCE_REPLAYED", "nonce 已使用过，请为每次请求生成新的随机串", gcode.CodeNotAuthorized,
	)
	// CodeBodyTooLarge 请求体超过本接口允许的上限（由处理器判定，请求体不参与签名）；
	// 参数 limit 为上限（文本 256 KB、文件 31 MB）。
	CodeBodyTooLarge = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_BODY_TOO_LARGE", "请求体超过 {limit} 上限", gcode.CodeInvalidParameter,
	)
	// CodeSendPartialFailed 请求已处理，但有收件人投递失败（对外 HTTP 接口用，FB-12）；
	// 参数 count 为失败项数，失败明细随响应 data.failed 返回，调用方据此只重试失败的收件人。
	CodeSendPartialFailed = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED", "{count} 个收件人投递失败", gcode.CodeOperationFailed,
	)
	// CodeNonceStoreUnavailable 宿主缓存能力不可用，无法判定 nonce 是否重放。
	// 服务端 fail-closed：不降级为「跳过去重放行」。
	CodeNonceStoreUnavailable = bizerr.MustDefine(
		"LINAPRO_LARK_PUSH_NONCE_STORE_UNAVAILABLE", "nonce 去重存储不可用，暂时拒绝请求", gcode.CodeNotImplemented,
	)
)
