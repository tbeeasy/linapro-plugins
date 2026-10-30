package moka

import (
	"context"
	"net/http"
)

// Author 对发出的 Moka API 请求应用鉴权。
// 存在两种实现，对应招聘 OpenAPI 支持的两种模式：BasicAuth
// （Authorization: Basic base64(apiKey:)）和 OAuth2Auth
// （通过 client_credentials 授权获取的 Bearer token）。
type Author interface {
	ApplyAuth(ctx context.Context, req *http.Request) error
}
