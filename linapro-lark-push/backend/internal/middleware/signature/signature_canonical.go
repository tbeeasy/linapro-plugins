// 本文件负责待签串的规范化与签名计算，是客户端与服务端必须逐字节一致的部分。

package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// EmptyBodyDigest 是空串的 SHA-256 十六进制摘要，即待签串的固定末行。请求体不参与签名，
// 保留该行只为与既有客户端的待签串结构一致。
const EmptyBodyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// BuildStringToSign 拼接待签串，客户端实现必须与之完全一致：
//
//	METHOD + "\n" + PATH + "\n" + CANONICAL_QUERY + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + EmptyBodyDigest
//
// CANONICAL_QUERY 的规范化规则：按参数名升序、同名参数按值升序，逐项拼成
// `urlencode(name)=urlencode(value)`（空格编码为 `+`），各项用 `&` 连接；无参数时为空串。
// 先排序再拼接让同一组查询参数只有一种表示，避免「参数顺序不同 → 签名不同」的调用方困扰。
func BuildStringToSign(
	method string,
	path string,
	query url.Values,
	timestamp string,
	nonce string,
) string {
	parts := []string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		canonicalQuery(query),
		timestamp,
		nonce,
		EmptyBodyDigest,
	}
	return strings.Join(parts, "\n")
}

// canonicalQuery 把查询参数规范化为可稳定重现的字符串。
func canonicalQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(query))
	for name, values := range query {
		if len(values) == 0 {
			// 只有参数名没有值：按空值参与签名，与服务端解析结果保持一致。
			pairs = append(pairs, urlEncode(name)+"=")
			continue
		}
		for _, value := range values {
			pairs = append(pairs, urlEncode(name)+"="+urlEncode(value))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// urlEncode 使用与多数语言 urlencode() 一致的编码：空格为 `+`，其余按 QueryEscape。
func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// SignBody 用 secret 对待签串计算小写十六进制 HMAC-SHA256。
func SignBody(secret string, stringToSign string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}
