package hcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// HCMCredential 保存单个租户的 Moka HCM OpenAPI 认证材料。
// 每个 Moka 接口都有各自的 apiCode（接口编码）；调用 NewClient 之前，
// 请用 SDK 定义的键常量（如 APICodeKeyReportData）填充 APICodes。
// 若缺少已知键，NewClient 会在启动时告警。
type HCMCredential struct {
	APIKey     string            // Basic 认证用户名；密码为空
	EntCode    string            // 查询参数 entCode（租户唯一 ID）
	PrivateKey *rsa.PrivateKey   // 用于 MD5withRSA 签名的 RSA 密钥
	APICodes   map[string]string // 接口名 → apiCode；键为 SDK 常量

	// OperatorEmail 是部分接口（如 batch/data 员工列表）要求的操作人邮箱，
	// 会作为签名查询参数 userName 参与签名。仅在对应接口需要时由调用方填充；
	// 为空则不追加该参数（如 getReportData 无需操作人）。
	OperatorEmail string
}

// Author 为发往 Moka HCM API 的请求应用认证。
// apiCode 按调用传入，因为每个 Moka 接口都有各自的编码。
// extra 是本次调用额外参与签名的查询参数（如 batch/data 的 userName）；
// 传 nil 表示无额外参数。
type Author interface {
	ApplyAuth(ctx context.Context, req *http.Request, apiCode string, extra map[string]string) error
}

// HCMAuth 为 Moka HCM OpenAPI 实现 Author：在每个请求上叠加 Basic 认证头
// 与 RSA 签名的查询参数（apiCode/entCode/nonce/timestamp/sign）。
// 这是 getReportData 等非隐私 HCM 端点使用的认证方式。
type HCMAuth struct {
	cred HCMCredential
}

// NewHCMAuth 根据给定的凭证构建 HCMAuth。
func NewHCMAuth(cred HCMCredential) *HCMAuth {
	return &HCMAuth{cred: cred}
}

// ApplyAuth 设置 Authorization: Basic …，并将签名的查询参数追加到请求 URL。
// 请求 URL 必须先设置好 path；已有的查询串会被保留，签名参数合并进去。
// apiCode 是 Moka 为每个 API 端点分配的接口编码。
// extra 是本次调用额外参与签名的查询参数（如 batch/data 的 userName）；
// 其键值会并入待签名参数，空值键会被跳过。
func (a *HCMAuth) ApplyAuth(_ context.Context, req *http.Request, apiCode string, extra map[string]string) error {
	n, err := nonce()
	if err != nil {
		return err
	}
	params := map[string]string{
		"entCode":   a.cred.EntCode,
		"apiCode":   apiCode,
		"nonce":     n,
		"timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10),
	}
	// 并入调用方指定的额外签名参数（跳过空值，避免签入无意义的空串）。
	for k, v := range extra {
		if k == "" || v == "" {
			continue
		}
		params[k] = v
	}
	query, err := BuildQuery(params, a.cred.PrivateKey)
	if err != nil {
		return err
	}
	// 将签名参数合并到已有的查询串（如有）中。
	existing := req.URL.RawQuery
	if existing == "" {
		req.URL.RawQuery = query
	} else {
		req.URL.RawQuery = existing + "&" + query
	}
	req.Header.Set("Authorization", BasicAuthHeader(a.cred.APIKey))
	return nil
}

// nonce 生成长度为 8 的随机字母数字串（Moka 上限：
// ≤8 个字符，数字与不区分大小写的字母）。
func nonce() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", gerror.Wrap(err, "hcm: generate nonce")
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

// BuildQuery 组装可追加到 URL 的签名查询串。它先对参数签名，
// 再追加 sign 键，最后通过 url.Values.Encode() 一次性编码。
func BuildQuery(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	sign, err := Sign(params, priv)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("sign", sign)
	return values.Encode(), nil
}
