package hcm

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// CanonicalString 根据查询参数构建待签名字符串。它排除已有的 "sign" 键，
// 将其余键按升序排序，并使用原始的未编码值拼接为 k=v&k=v。
func CanonicalString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

// ParsePrivateKey 解码 PEM 编码的 RSA 私钥。同时支持 PKCS#1
// （"RSA PRIVATE KEY"）与 PKCS#8（"PRIVATE KEY"）两种编码。
func ParsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemData)))
	if block == nil {
		return nil, gerror.New("hcm: RSA 私钥中未找到 PEM 块")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: 解析 RSA 私钥失败（已尝试 PKCS#1 与 PKCS#8）")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, gerror.Newf("hcm: PKCS#8 私钥不是 RSA 类型（%T）", parsed)
	}
	return key, nil
}

// Sign 生成 "sign" 查询参数的值：对规范串做 MD5withRSA 签名并 base64 编码。
// 返回值为原始的 base64 字符串；调用方将其放入 url.Values，
// 确保 URL 编码只发生一次。
func Sign(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", gerror.New("hcm: RSA 私钥为空")
	}
	digest := md5.Sum([]byte(CanonicalString(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.MD5, digest[:])
	if err != nil {
		return "", gerror.Wrap(err, "hcm: RSA 签名失败")
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify 使用公钥校验 base64 签名是否与规范串匹配。
// 该函数用于单元测试与运维诊断。
func Verify(params map[string]string, signB64 string, pub *rsa.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(signB64)
	if err != nil {
		return gerror.Wrap(err, "hcm: 解码签名失败")
	}
	digest := md5.Sum([]byte(CanonicalString(params)))
	if err := rsa.VerifyPKCS1v15(pub, crypto.MD5, digest[:], sig); err != nil {
		return gerror.Wrap(err, "hcm: 签名校验失败")
	}
	return nil
}

// BasicAuthHeader 返回 Moka Basic 认证的 Authorization 头值：
// "Basic " + base64(apiKey + ":")。API key 作为用户名，密码为空，
// 等价于 `curl -u 'apiKey:'`。
func BasicAuthHeader(apiKey string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey+":"))
}
