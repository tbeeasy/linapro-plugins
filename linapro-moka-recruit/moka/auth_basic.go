package moka

import (
	"context"
	"encoding/base64"
	"net/http"
)

// BasicCredential 保存单个租户的 Moka OpenAPI Basic Auth 凭证。
// API Key 用作 Basic 鉴权的用户名，密码为空。
type BasicCredential struct {
	APIKey string // Basic 鉴权用户名，密码为空
}

// BasicAuth 以 Moka 的 Basic Auth 模式实现 Author：在每次请求中设置
// Authorization: Basic base64(apiKey:) 头，且不做查询签名。这是 Moka 招聘
// OpenAPI 的主力鉴权方式（所有已文档化的招聘端点均接受 `-u 'apiKey:'`）。
type BasicAuth struct {
	cred BasicCredential
}

// NewBasicAuth 基于给定凭证构建一个 BasicAuth。
func NewBasicAuth(cred BasicCredential) *BasicAuth {
	return &BasicAuth{cred: cred}
}

// ApplyAuth 设置 Authorization: Basic … 请求头。不添加任何查询参数，
// 招聘 API 不要求请求签名。
func (a *BasicAuth) ApplyAuth(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", BasicAuthHeader(a.cred.APIKey))
	return nil
}

// BasicAuthHeader 返回 Moka Basic 鉴权的 Authorization 请求头值：
// Basic base64(apiKey:) —— API Key 为用户名，密码为空。
func BasicAuthHeader(apiKey string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey+":"))
}
