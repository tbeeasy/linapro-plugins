package larkhttp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockInner 是一个可编排的内层 HttpClient：按 responses 顺序逐次返回预设结果，
// 并记录每次收到的请求 body，用于断言重试时 body 被正确重放。
type mockInner struct {
	responses []*http.Response
	calls     int
	bodies    []string
}

func (m *mockInner) Do(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	m.bodies = append(m.bodies, body)

	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	return m.responses[idx], nil
}

func resp(status int, header map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func newRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, "https://open.feishu.cn/open-apis/bitable/v1/x", strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	return req
}

// recordSleep 收集每次等待时长，替代真实睡眠，让测试无需真的阻塞。
type recordSleep struct{ waits []time.Duration }

func (r *recordSleep) fn(_ *http.Request, d time.Duration) error {
	r.waits = append(r.waits, d)
	return nil
}

// Test429ThenSuccess 验证：首次 429（带 x-ogw-ratelimit-reset:1）后退避 1s 重试，
// 第二次拿到 200，总共调 2 次，body 两次都被完整重放。
func Test429ThenSuccess(t *testing.T) {
	inner := &mockInner{responses: []*http.Response{
		resp(429, map[string]string{headerRateLimitReset: "1"}),
		resp(200, nil),
	}}
	sleeper := &recordSleep{}
	c := NewClient(WithInner(inner))
	c.sleep = sleeper.fn

	got, err := c.Do(newRequest(t, "payload"))
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if got.StatusCode != 200 {
		t.Fatalf("期望最终 200，得到 %d", got.StatusCode)
	}
	if inner.calls != 2 {
		t.Fatalf("期望调用内层 2 次，实际 %d", inner.calls)
	}
	if len(sleeper.waits) != 1 || sleeper.waits[0] != time.Second {
		t.Fatalf("期望退避一次 1s，实际 %v", sleeper.waits)
	}
	for i, b := range inner.bodies {
		if b != "payload" {
			t.Fatalf("第 %d 次 body 未正确重放: %q", i+1, b)
		}
	}
}

// TestExhaustRetries 验证：持续 429 时，重试次数用尽后把最后一次 429 原样返回，
// 内层被调用 maxRetries+1 次。
func TestExhaustRetries(t *testing.T) {
	inner := &mockInner{responses: []*http.Response{
		resp(429, map[string]string{headerRateLimitReset: "0"}),
	}}
	sleeper := &recordSleep{}
	c := NewClient(WithInner(inner), WithMaxRetries(3))
	c.sleep = sleeper.fn

	got, err := c.Do(newRequest(t, "x"))
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if got.StatusCode != 429 {
		t.Fatalf("期望最终返回 429，得到 %d", got.StatusCode)
	}
	if inner.calls != 4 { // 首次 + 3 次重试
		t.Fatalf("期望内层调用 4 次，实际 %d", inner.calls)
	}
}

// TestNonRateLimitPassthrough 验证：非 429 响应直接透传，不触发重试。
func TestNonRateLimitPassthrough(t *testing.T) {
	inner := &mockInner{responses: []*http.Response{resp(500, nil)}}
	c := NewClient(WithInner(inner))
	c.sleep = (&recordSleep{}).fn

	got, err := c.Do(newRequest(t, "x"))
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if got.StatusCode != 500 {
		t.Fatalf("期望透传 500，得到 %d", got.StatusCode)
	}
	if inner.calls != 1 {
		t.Fatalf("期望内层只调 1 次，实际 %d", inner.calls)
	}
}

// TestContextCancelDuringWait 验证：退避等待期间 ctx 取消，Do 返回 ctx 错误。
func TestContextCancelDuringWait(t *testing.T) {
	inner := &mockInner{responses: []*http.Response{
		resp(429, map[string]string{headerRateLimitReset: "1"}),
	}}
	c := NewClient(WithInner(inner))
	c.sleep = func(req *http.Request, _ time.Duration) error {
		return context.Canceled
	}

	_, err := c.Do(newRequest(t, "x"))
	if err != context.Canceled {
		t.Fatalf("期望返回 context.Canceled，得到 %v", err)
	}
}

// TestFallbackBackoff 验证：限流响应无 reset/Retry-After 头时用指数退避兜底，
// 且等待时长不超过 maxWait。
func TestFallbackBackoff(t *testing.T) {
	inner := &mockInner{responses: []*http.Response{
		resp(429, nil),
		resp(200, nil),
	}}
	sleeper := &recordSleep{}
	c := NewClient(WithInner(inner), WithMaxWait(10*time.Second))
	c.sleep = sleeper.fn

	if _, err := c.Do(newRequest(t, "x")); err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if len(sleeper.waits) != 1 {
		t.Fatalf("期望退避一次，实际 %d", len(sleeper.waits))
	}
	// base=1s, attempt=0 → backoff 落在 [1s, 2s)。
	if w := sleeper.waits[0]; w < time.Second || w >= 2*time.Second {
		t.Fatalf("兜底退避应落在 [1s,2s)，实际 %v", w)
	}
}
