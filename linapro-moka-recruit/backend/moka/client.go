// Package moka 提供 Moka 招聘系统 OpenAPI 客户端与鉴权层，
// 覆盖招聘系统 API（/api-platform/v1/、v2/、v3/）。
// 调用方使用 BasicAuth 或 OAuth2Auth 构建 Client，再通过 PostJSON
// 访问任意招聘端点。
package moka

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// DefaultBaseURL 是 Moka OpenAPI 主机地址。
const DefaultBaseURL = "https://api.mokahr.com"

// Client 是 Moka 招聘 OpenAPI 的 HTTP 客户端。它将鉴权委托给注入的 Author，
// 使同一套传输层同时适用于 HCM 与 OAuth2 两种模式。可安全并发使用。
type Client struct {
	baseURL string
	auth    Author
	http    *http.Client
}

// NewClient 构建一个 Client。baseURL 为空时回退到 DefaultBaseURL。
func NewClient(baseURL string, auth Author) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PostJSON 发送带 JSON 请求体的 POST 请求。path 为完整 API 路径
// （如 "/api-platform/v1/getReportData"）。发送请求前由 Author 应用鉴权。
// HTTP 2xx 时返回原始响应字节；由调用方解析响应信封。
func (c *Client) PostJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: build request")
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.auth.ApplyAuth(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: request failed")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: read response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gerror.Newf("moka-recruit: HTTP %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	return data, nil
}

// PutQuery 发送 PUT 请求，参数编码进 URL 查询串，请求体为空。发送请求前
// 由 Author 应用鉴权。HTTP 2xx 时返回 nil；非 2xx 时返回携带状态码的错误。
func (c *Client) PutQuery(ctx context.Context, path string, params url.Values) error {
	rawURL := c.baseURL + path
	if len(params) > 0 {
		rawURL += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, nil)
	if err != nil {
		return gerror.Wrap(err, "moka-recruit: build PUT request")
	}

	if err := c.auth.ApplyAuth(ctx, req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return gerror.Wrap(err, "moka-recruit: PUT request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return gerror.Newf("moka-recruit: HTTP %d: %s", resp.StatusCode, truncate(string(body), 512))
	}
	return nil
}

// GetJSON 发送无请求体的 GET 请求。发送请求前由 Author 应用鉴权。
// HTTP 2xx 时返回原始响应字节。
func (c *Client) GetJSON(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: build GET request")
	}

	if err := c.auth.ApplyAuth(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: GET request failed")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: read GET response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gerror.Newf("moka-recruit: HTTP %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	return data, nil
}

// truncate 截断字符串，用于安全地记录日志。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
