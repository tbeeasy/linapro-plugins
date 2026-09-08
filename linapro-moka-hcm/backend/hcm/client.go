// Package hcm 提供 Moka HCM OpenAPI 客户端与认证层。
// 它为 getReportData 等 HCM 端点暴露 HCMAuth（Basic 认证 + MD5withRSA 查询签名）。
// 调用方通过 NewClient(baseURL, cred) 构建 Client，再调用类型化的业务方法
// （如 GetReportData）；apiCode 处理在内部完成。
//
// 文件布局：
//   - author.go  — Author 接口、HCMCredential、HCMAuth
//   - sign.go    — Sign、BuildQuery、CanonicalString、ParsePrivateKey、BasicAuthHeader
//   - client.go  — Client、NewClient、PostJSON
//   - report.go  — GetReportData、ReportData、ReportHeader
//
// 其他 HCM API 域以单独文件添加（如 employee.go、org.go），
// 各自在 *Client 上挂载方法。
package hcm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// DefaultBaseURL 是 Moka OpenAPI 主机地址。
const DefaultBaseURL = "https://api.mokahr.com"

// Client 是 Moka HCM OpenAPI HTTP 客户端。它持有完整凭证，
// 因此每个业务方法可向 PostJSON 提供各自的 apiCode。
// 可安全并发使用。
type Client struct {
	baseURL string
	cred    HCMCredential
	auth    Author
	http    *http.Client
}

// NewClient 构建 Client。baseURL 为空时使用 DefaultBaseURL。
// cred 保存在 Client 上，业务方法可直接从中读取各自的 apiCode；
// 调用方无需自行构造 Author。
func NewClient(baseURL string, cred HCMCredential) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		cred:    cred,
		auth:    NewHCMAuth(cred),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PostJSON 发送带 JSON 请求体的 POST 请求。path 是完整 API 路径
// （如 "/api-platform/hcm/oapi/v1/report/getReportData"）。apiCode 是本次调用
// 的接口编码；业务方法从其所属的凭证字段中提供。extra 是本次调用额外参与签名的
// 查询参数（如 batch/data 的 userName），无额外参数时传 nil。返回原始响应字节；
// 由调用方解码外层信封。
func (c *Client) PostJSON(ctx context.Context, path string, apiCode string, body []byte, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: build request")
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.auth.ApplyAuth(ctx, req, apiCode, extra); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: request failed")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: read response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gerror.Newf("hcm: HTTP %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	return data, nil
}

// truncate 截断字符串以便安全地打印日志。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
