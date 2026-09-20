// Package larkhttp 提供一个包裹飞书官方 SDK 的自定义 HTTP 客户端，用于统一处理
// 飞书网关的 429 限流响应。
//
// 官方 SDK v3.11.0 的内置重试只覆盖「拨号失败」与「token 失效」，遇到 HTTP 429
// （飞书限频，响应体 code=99991400）会直接把错误抛给调用方。本包实现 larkcore.HttpClient
// 接口，在收到 429 时读取响应头 x-ogw-ratelimit-reset（单位：秒）建议的等待时长，
// 退避后重试；该头缺失时回退到 Retry-After，再缺失则用带抖动的指数退避兜底。
//
// 因为飞书 SDK 的 token 获取请求与业务请求走同一个 HttpClient，注入本客户端后
// tenant_access_token 刷新被限流时同样会自动退避重试。
package larkhttp

import (
	"bytes"
	"io"
	"lina-core/pkg/logger"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

const (
	// rateLimitStatus 是飞书网关触发限流时返回的 HTTP 状态码。
	rateLimitStatus = http.StatusTooManyRequests // 429

	// headerRateLimitReset 是飞书在限流响应中给出的建议等待秒数（恢复 limit 的周期）。
	// 使用 net/http 规范化的大写形式（CanonicalMIMEHeaderKey）以匹配 Header.Get 的查找键；
	// 飞书实际下发的 x-ogw-ratelimit-reset 大小写不敏感，两者等价。
	headerRateLimitReset = "X-Ogw-Ratelimit-Reset"

	// defaultMaxRetries 是命中限流后的最大重试次数（不含首次请求）。
	defaultMaxRetries = 4

	// defaultMaxWait 是单次退避的等待上限，防止异常响应头导致长时间阻塞。
	defaultMaxWait = 60 * time.Second

	// defaultBaseBackoff 是响应头缺失时指数退避的基准时长。
	defaultBaseBackoff = time.Second
)

// Client 实现 larkcore.HttpClient，在内层 http.Client 之上叠加 429 限流退避重试。
type Client struct {
	inner       larkcore.HttpClient
	maxRetries  int
	maxWait     time.Duration
	baseBackoff time.Duration
	// sleep 抽象出等待逻辑，便于单测注入假时钟；nil 时使用 ctxSleep。
	sleep func(*http.Request, time.Duration) error
}

// Option 用于覆盖 Client 的默认参数。
type Option func(*Client)

// WithMaxRetries 设置命中限流后的最大重试次数。
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithMaxWait 设置单次退避的等待上限。
func WithMaxWait(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.maxWait = d
		}
	}
}

// WithInner 覆盖内层 HttpClient，主要供单测注入 mock。生产环境留空则使用
// 带超时的默认 http.Client。
func WithInner(inner larkcore.HttpClient) Option {
	return func(c *Client) {
		if inner != nil {
			c.inner = inner
		}
	}
}

// NewClient 构造一个带 429 退避重试的 larkcore.HttpClient。未指定 WithInner 时，
// 内层使用 http.DefaultClient（与 SDK 默认行为一致，超时由调用方通过 ctx 控制）。
func NewClient(opts ...Option) *Client {
	c := &Client{
		inner:       http.DefaultClient,
		maxRetries:  defaultMaxRetries,
		maxWait:     defaultMaxWait,
		baseBackoff: defaultBaseBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sleep == nil {
		c.sleep = ctxSleep
	}
	return c
}

// Do 发送请求；收到 HTTP 429 时按建议时长退避并重试，直到成功、用尽重试次数、
// 遇到非限流响应或 ctx 取消。用尽重试后把最后一次 429 响应原样返回，交由 SDK
// 与业务照常报错，不吞异常。
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// 首次发送前把 body 缓冲进内存，以便每次重试重建可读流。飞书业务请求为 KB 级
	// JSON，上传请求 body 也是调用方预先读入内存的有界字节（简历上传有 20MiB 上限），
	// 故缓冲不会引入额外的内存峰值。
	body, err := bufferBody(req)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resetBody(req, body)

		resp, err = c.inner.Do(req)
		if err != nil {
			// 网络等错误交给上层（SDK 有自己的拨号重试），本层只负责限流。
			return nil, err
		}
		if resp.StatusCode != rateLimitStatus {
			return resp, nil
		}
		if attempt >= c.maxRetries {
			// 重试次数用尽，把 429 原样返回让上层报错。
			return resp, nil
		}

		wait := c.waitDuration(resp, attempt)
		// 丢弃并关闭本次 429 响应体，避免连接无法复用。
		drainAndClose(resp)

		logger.Warningf(req.Context(),
			"larkhttp: 飞书限流(429) url=%s 第%d次重试 等待=%s",
			req.URL.Path, attempt+1, wait)

		if serr := c.sleep(req, wait); serr != nil {
			return nil, serr
		}
	}
}

// waitDuration 依据限流响应决定本次退避时长：优先 x-ogw-ratelimit-reset（秒），
// 其次标准 Retry-After（秒），都缺失则用带抖动的指数退避兜底。结果被裁剪到 maxWait。
func (c *Client) waitDuration(resp *http.Response, attempt int) time.Duration {
	if d, ok := parseSeconds(resp.Header.Get(headerRateLimitReset)); ok {
		return clampWait(d, c.maxWait)
	}
	if d, ok := parseSeconds(resp.Header.Get("Retry-After")); ok {
		return clampWait(d, c.maxWait)
	}
	// 指数退避：base * 2^attempt，叠加 [0,base) 抖动，避免多协程同时重试形成新冲击。
	backoff := c.baseBackoff << attempt
	jitter := time.Duration(rand.Int63n(int64(c.baseBackoff)))
	return clampWait(backoff+jitter, c.maxWait)
}

// parseSeconds 把「秒」字符串解析为 Duration；空串或非法值返回 ok=false。
func parseSeconds(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

// clampWait 将等待时长裁剪到 [0, maxWait]。
func clampWait(d, maxWait time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > maxWait {
		return maxWait
	}
	return d
}

// bufferBody 读出并缓冲 req.Body。无 body 时返回 nil。
func bufferBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	return body, nil
}

// resetBody 用缓冲字节为 req 重建一个可读的 body 与配套的 GetBody，供每次发送使用。
func resetBody(req *http.Request, body []byte) {
	if body == nil {
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

// drainAndClose 读尽并关闭响应体，使底层 TCP 连接可被复用。
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// ctxSleep 等待 d，或在 ctx 取消时提前返回其错误。
func ctxSleep(req *http.Request, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	ctx := req.Context()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
