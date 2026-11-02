// Package signature 实现对外推送接口的 HMAC-SHA256 签名鉴权中间件。
//
// 为什么不复用宿主鉴权：宿主没有任何机器身份认证机制（ClientType 枚举刻意排除服务/机器
// 身份），且 Permission() 硬依赖 bizUser 上下文。把机器认证塞进宿主是框架级改造，
// 与本次范围不匹配，故在插件内实现。
//
// 待签串（换行分隔，任一字段变化都会导致签名失效）：
//
//	METHOD + "\n" + PATH + "\n" + CANONICAL_QUERY + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + hex(SHA256(""))
//
// 请求体一律不参与签名（FB-8 文件接口、FB-9 文本接口，用户要求）：末行固定为空串的 SHA-256，
// 中间件也不读取请求体，请求体上限由各处理器把关。保留末行是为了与既有客户端的待签串结构一致。
// 代价是请求体（消息内容、文件与收件人）不受签名保护，必须走 HTTPS 防止传输途中被篡改；
// 方法、路径、查询串、时间戳与 nonce 仍受签名保护，凭据校验与防重放照常生效。
//
// 凭据来自本插件数据表（后台「飞书推送凭据」页维护，FB-5），每次请求按 app_key 经
// apikey.Service 直接查库、不缓存：停用 / 删除在下一次请求即生效。
//
// 安全要点（全部 fail-closed）：
//   - 凭据表或去重缓存不可用 → HTTP 503，拒绝请求；
//   - 缺签名头 → 拒绝（HTTP 200 + 错误码，FB-12），且**不进入签名比对与 nonce 登记**；
//   - 时间戳超出 ±300 秒窗口 → 拒绝（限制重放窗口）；
//   - 签名用 HMAC-SHA256 计算并由常量时间比较，长度不等直接判否；
//   - nonce 经宿主共享缓存去重，先验签后登记：未通过认证的请求不产生任何状态变更，
//     攻击者无法用错签名的请求消耗他人正在传输中的 nonce；
//   - 缓存不可用时失败关闭（HTTP 503），不降级为「跳过去重放行」；
//   - 拒绝时只设置错误与状态码，响应体由外层宿主统一响应中间件包装（FB-12）；
//   - secret 与签名原文绝不进日志。
package signature

import (
	"context"
	"crypto/subtle"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"
	"lina-core/pkg/plugin/capability/cachecap"

	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
	"lina-plugin-linapro-lark-push/backend/internal/service/apikey"
)

// 签名协议常量。客户端必须与这些取值保持一致。
const (
	// HeaderAppKey 携带公开的应用标识。
	HeaderAppKey = "X-App-Key"
	// HeaderTimestamp 携带 Unix 秒级时间戳。
	HeaderTimestamp = "X-Timestamp"
	// HeaderNonce 携带一次性随机串（16~64 字符），用于防重放。
	HeaderNonce = "X-Nonce"
	// HeaderSignature 携带小写十六进制 HMAC-SHA256 签名。
	HeaderSignature = "X-Signature"

	// TimestampWindow 是时间戳允许的前后偏差；窗口外的请求一律拒绝。
	TimestampWindow = 300 * time.Second
	// MinNonceBytes 与 MaxNonceBytes 限定 nonce 长度，避免空串或超长键撑爆缓存键。
	MinNonceBytes = 16
	MaxNonceBytes = 64
	// MaxAppKeyBytes 限定 app_key 长度，保证「app_key + nonce」组成的缓存键不超限。
	MaxAppKeyBytes = 64
	// nonceNamespace 是 nonce 去重条目在插件缓存中的命名空间。
	nonceNamespace = "http-signature"
)

// nonceTTL 是 nonce 去重条目的存活时间：取窗口两倍，覆盖「窗口两端仍可能被接受」的整段时间。
const nonceTTL = 2 * TimestampWindow

// ctxKey 是本包注入上下文的私有键类型。
type ctxKey struct{}

// Identity 是鉴权通过后放进请求上下文的调用方标识，供投递选路与日志使用。
type Identity struct {
	// KeyID 是命中的凭据记录标识（如 api-key-12，取数据表 ID），**不是**密钥内容。
	KeyID string
	// AppKey 是公开的应用标识，可安全进日志。
	AppKey string
	// Group 是命中凭据绑定的推送分组，投递只用该分组的推送应用。
	Group string
	// ChatID 是命中凭据配置的群聊 chat_id（后台维护，调用方不能指定）；为空表示不推群聊。
	ChatID string
	// ChatPushAppID 是发送该群聊的推送应用；为空表示用分组唯一的推送应用。
	ChatPushAppID string
}

// FromContext 取出鉴权标识；未经过本中间件时返回 ok=false。
func FromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(ctxKey{}).(Identity)
	return v, ok
}

// Middleware 构造签名鉴权中间件。apiKeys 或 cache 为 nil 时一律 503（fail-closed）——
// 查不到凭据就无法验签，没有去重存储就无法判定重放，宁可拒绝也不能放行。
//
// 中间件不读取请求体：请求体不参与签名，原样留给处理器解析并由处理器负责大小上限。
//
// alerter 只用于「服务端基础设施不可用」（凭据存储、nonce 去重存储）；签名类错误
// （缺头、时间戳超窗、签名不匹配、nonce 重放、app_key 未知）属于调用方侧问题，
// 会被扫描器反复触发，一律不告警。alerter 为 nil 时静默跳过。
func Middleware(apiKeys apikey.Service, cache cachecap.Service, alerter alert.Service) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		if r == nil {
			return
		}
		ctx := r.Context()

		identity, err := authenticate(ctx, r, apiKeys, cache, alerter)
		if err != nil {
			// 只设置状态码与错误、不写响应体，也不调用 Next：响应体由外层宿主统一响应中间件
			// 包装为 code / message / errorCode（FB-12）。
			r.SetError(larkpushcode.WithHTTPStatus(r, err))
			return
		}

		r.SetCtx(context.WithValue(ctx, ctxKey{}, identity))
		r.Middleware.Next()
	}
}

// notifyInfra 在告警通道已配置时报告一次服务端基础设施故障。
func notifyInfra(ctx context.Context, alerter alert.Service, kind alert.Kind, detail string) {
	if alerter != nil {
		alerter.Notify(ctx, alert.Event{Kind: kind, Detail: detail})
	}
}

// authenticate 执行完整校验链：依赖 → 头部完整性 → app_key（查库）→ 时间戳 → 签名 → nonce 去重。
//
// 顺序有意固定：先验签、后登记 nonce，使未通过认证的请求不产生任何状态变更。
func authenticate(
	ctx context.Context,
	r *ghttp.Request,
	apiKeys apikey.Service,
	cache cachecap.Service,
	alerter alert.Service,
) (Identity, error) {
	if apiKeys == nil {
		logger.Errorf(ctx, "lark-push signature: credential service unavailable; rejecting request")
		notifyInfra(ctx, alerter, alert.KindCredentialStoreUnavailable,
			"签名凭据服务未装配，所有对外推送请求已被拒绝")
		return Identity{}, bizerr.NewCode(larkpushcode.CodeCredentialStoreUnavailable)
	}
	if cache == nil {
		logger.Errorf(ctx, "lark-push signature: cache capability unavailable, cannot verify nonce replay; rejecting request")
		notifyInfra(ctx, alerter, alert.KindNonceStoreUnavailable,
			"宿主缓存能力不可用，无法判定 nonce 重放，所有对外推送请求已被拒绝")
		return Identity{}, bizerr.NewCode(larkpushcode.CodeNonceStoreUnavailable)
	}

	appKey := strings.TrimSpace(r.Header.Get(HeaderAppKey))
	timestamp := strings.TrimSpace(r.Header.Get(HeaderTimestamp))
	nonce := strings.TrimSpace(r.Header.Get(HeaderNonce))
	presented := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderSignature)))
	if appKey == "" || timestamp == "" || nonce == "" || presented == "" {
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureMissing)
	}
	if len(nonce) < MinNonceBytes || len(nonce) > MaxNonceBytes {
		// 长度越界按「头部不合法」处理：过短的 nonce 会显著降低唯一性。
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureMissing)
	}

	if len(appKey) > MaxAppKeyBytes {
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureUnknownAppKey)
	}
	cred, err := apiKeys.FindEnabledByAppKey(ctx, appKey)
	if err != nil {
		logger.Errorf(ctx, "lark-push signature: credential lookup failed app_key=%s: %v", appKey, err)
		notifyInfra(ctx, alerter, alert.KindCredentialStoreUnavailable,
			"签名凭据查询失败（数据库不可用），所有对外推送请求已被拒绝")
		return Identity{}, bizerr.NewCode(larkpushcode.CodeCredentialStoreUnavailable)
	}
	if cred == nil {
		// 不存在、已停用、已删除统一按未知处理，不向外泄露 app_key 是否存在。
		logger.Warningf(ctx, "lark-push signature: unknown or disabled app_key path=%s app_key=%s", r.URL.Path, appKey)
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureUnknownAppKey)
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureTimestampInvalid)
	}
	drift := time.Since(time.Unix(ts, 0))
	if drift < -TimestampWindow || drift > TimestampWindow {
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureTimestampInvalid)
	}

	expected := SignBody(cred.Secret, BuildStringToSign(r.Method, r.URL.Path, r.URL.Query(), timestamp, nonce))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) != 1 {
		// 只记录 app_key 与路径，绝不记录 secret 或签名原文。
		logger.Warningf(ctx, "lark-push signature: signature mismatch path=%s app_key=%s", r.URL.Path, appKey)
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureMismatch)
	}

	// 签名已通过，此时才登记 nonce：失败路径不写缓存，避免被用来消耗他人 nonce。
	first, err := claimNonce(ctx, cache, appKey, nonce)
	if err != nil {
		logger.Errorf(ctx, "lark-push signature: nonce de-duplication failed app_key=%s: %v", appKey, err)
		notifyInfra(ctx, alerter, alert.KindNonceStoreUnavailable,
			"nonce 去重登记失败，缓存后端不可用，对外推送请求已被拒绝")
		return Identity{}, bizerr.NewCode(larkpushcode.CodeNonceStoreUnavailable)
	}
	if !first {
		logger.Warningf(ctx, "lark-push signature: nonce replayed path=%s app_key=%s", r.URL.Path, appKey)
		return Identity{}, bizerr.NewCode(larkpushcode.CodeSignatureNonceReplayed)
	}

	return Identity{
		KeyID:         "api-key-" + strconv.FormatInt(cred.Id, 10),
		AppKey:        appKey,
		Group:         cred.Group,
		ChatID:        cred.ChatID,
		ChatPushAppID: cred.ChatPushAppID,
	}, nil
}
