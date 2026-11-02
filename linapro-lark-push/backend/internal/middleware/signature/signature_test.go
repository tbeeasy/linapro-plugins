// 本文件覆盖对外接口 HMAC 签名鉴权的全部失败路径与正常路径。
//
// 每个用例都自行构造凭据服务替身、缓存替身与请求，互不依赖执行顺序。

package signature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	"lina-core/pkg/plugin/capability/cachecap"

	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/service/apikey"
	"lina-plugin-linapro-lark-push/backend/internal/testutil/envelope"
)

// testSecret 是测试用签名密钥（仅测试内使用，非真实凭据）。
const testSecret = "unit-test-secret"

// fakeKeys 是 apikey.Service 的替身：只实现中间件用到的 FindEnabledByAppKey，
// creds 只放「启用且未删除」的凭据（停用 / 删除在真实实现里同样查不到）。
type fakeKeys struct {
	apikey.Service
	creds map[string]*apikey.Credential
	// err 非空时模拟凭据表不可用。
	err error
	// lookups 记录查库次数。
	lookups int
}

// FindEnabledByAppKey 按 app_key 返回凭据；未命中返回 (nil, nil)。
func (f *fakeKeys) FindEnabledByAppKey(_ context.Context, appKey string) (*apikey.Credential, error) {
	f.lookups++
	if f.err != nil {
		return nil, f.err
	}
	return f.creds[appKey], nil
}

// fakeCache 是 cachecap.Service 的内存替身，只实现中间件用到的 Incr。
// 其余方法保持最小可用行为，避免测试依赖真实缓存后端。
type fakeCache struct {
	mu     sync.Mutex
	counts map[string]int64
	ttls   map[string]time.Duration
	// incrErr 非空时 Incr 返回该错误，用于模拟缓存后端不可用。
	incrErr error
}

// newFakeCache 构造一个空的内存缓存替身。
func newFakeCache() *fakeCache {
	return &fakeCache{counts: make(map[string]int64), ttls: make(map[string]time.Duration)}
}

// Incr 累加计数并返回最新快照，语义与宿主实现一致（不存在时从 0 起算）。
func (c *fakeCache) Incr(
	_ context.Context,
	namespace string,
	key string,
	delta int64,
	ttl time.Duration,
) (*cachecap.CacheItem, error) {
	if c.incrErr != nil {
		return nil, c.incrErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	full := namespace + "/" + key
	c.counts[full] += delta
	c.ttls[full] = ttl
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: c.counts[full]}, nil
}

// Get 返回已写入的计数条目。
func (c *fakeCache) Get(_ context.Context, namespace string, key string) (*cachecap.CacheItem, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.counts[namespace+"/"+key]
	if !ok {
		return nil, false, nil
	}
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: value}, true, nil
}

// GetMany 是批量读取的最简实现。
func (c *fakeCache) GetMany(_ context.Context, in cachecap.GetManyInput) (*cachecap.GetManyOutput, error) {
	out := &cachecap.GetManyOutput{Items: make(map[string]*cachecap.CacheItem)}
	for _, key := range in.Keys {
		item, ok, err := c.Get(context.Background(), in.Namespace, key)
		if err != nil {
			return nil, err
		}
		if ok {
			out.Items[key] = item
		} else {
			out.MissingKeys = append(out.MissingKeys, key)
		}
	}
	return out, nil
}

// Set 写入字符串条目。
func (c *fakeCache) Set(
	_ context.Context,
	namespace string,
	key string,
	value string,
	ttl time.Duration,
) (*cachecap.CacheItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttls[namespace+"/"+key] = ttl
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindString, Value: value}, nil
}

// SetMany 逐个写入字符串条目。
func (c *fakeCache) SetMany(_ context.Context, in cachecap.SetManyInput) (*cachecap.SetManyOutput, error) {
	out := &cachecap.SetManyOutput{Items: make(map[string]*cachecap.CacheItem)}
	for _, item := range in.Items {
		written, err := c.Set(context.Background(), in.Namespace, item.Key, item.Value, item.TTL)
		if err != nil {
			return nil, err
		}
		out.Items[item.Key] = written
	}
	return out, nil
}

// Delete 删除条目。
func (c *fakeCache) Delete(_ context.Context, namespace string, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.counts, namespace+"/"+key)
	return nil
}

// DeleteMany 批量删除条目。
func (c *fakeCache) DeleteMany(_ context.Context, in cachecap.DeleteManyInput) error {
	for _, key := range in.Keys {
		if err := c.Delete(context.Background(), in.Namespace, key); err != nil {
			return err
		}
	}
	return nil
}

// Expire 只记录 TTL，测试不依赖真实过期。
func (c *fakeCache) Expire(_ context.Context, namespace, key string, ttl time.Duration) (bool, *time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttls[namespace+"/"+key] = ttl
	expireAt := time.Now().Add(ttl)
	return true, &expireAt, nil
}

// entries 把若干 (app_key, secret, group) 三元组构造成凭据服务替身，ID 依次为 1、2、3……
func entries(triples ...string) *fakeKeys {
	keys := &fakeKeys{creds: make(map[string]*apikey.Credential)}
	for i := 0; i+2 < len(triples); i += 3 {
		keys.creds[triples[i]] = &apikey.Credential{
			Id:     int64(i/3 + 1),
			AppKey: triples[i],
			Secret: triples[i+1],
			Group:  triples[i+2],
		}
	}
	return keys
}

// testServer 是启动后的测试服务地址。
type testServer struct {
	URL string
}

// newTestServer 构造挂着「统一响应替身 → 签名中间件」的最小服务（与生产装配一致：签名拒绝时只设置
// 错误，由外层统一响应写响应体）。通过鉴权后回 200 "ok:<keyID>:<appKey>:<group>"，
// 并在 /echo 上回显收到的请求体，用于验证「中间件不读请求体，处理器能读到完整请求体」。
func newTestServer(t *testing.T, keys apikey.Service, cache cachecap.Service) *testServer {
	t.Helper()
	s := ghttp.GetServer("signature-test-" + t.Name())
	s.SetAddr("127.0.0.1:0")
	s.SetDumpRouterMap(false)
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(envelope.Middleware(), Middleware(keys, cache, nil))
		handler := func(r *ghttp.Request) {
			id, ok := FromContext(r.Context())
			if !ok {
				t.Errorf("鉴权通过后上下文中应带 Identity")
			}
			r.Response.Write("ok:" + id.KeyID + ":" + id.AppKey + ":" + id.Group)
		}
		group.GET("/probe", handler)
		group.POST("/probe", handler)
		group.POST("/echo", func(r *ghttp.Request) {
			r.Response.Write("body:" + string(r.GetBody()))
		})
	})
	if err := s.Start(); err != nil {
		t.Fatalf("启动测试服务: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	return &testServer{URL: "http://" + s.GetListenedAddress()}
}

// signedRequest 构造一次带完整签名头的请求；body 只作为请求体发送，不参与签名。
//
// now 用于覆盖时间戳场景；nonce 由调用方给出以便构造重放；secret 用于计算签名。
func signedRequest(
	t *testing.T,
	method string,
	target string,
	body string,
	appKey string,
	secret string,
	nonce string,
	now time.Time,
) *http.Request {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	toSign := BuildStringToSign(method, u.Path, u.Query(), timestamp, nonce)
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set(HeaderAppKey, appKey)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderNonce, nonce)
	req.Header.Set(HeaderSignature, SignBody(secret, toSign))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// send 执行请求并返回状态码与响应体。
func send(t *testing.T, req *http.Request) (int, string, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n]), resp.Status
}

// validNonce 返回一个符合长度要求的 nonce。
func validNonce(suffix string) string {
	base := "nonce-0123456789-abcdefghij-"
	return base + suffix
}

// TestMiddleware_CredentialStoreErrorRejects 验证凭据表不可查询时返回 503（fail-closed）。
func TestMiddleware_CredentialStoreErrorRejects(t *testing.T) {
	keys := entries("k1", testSecret, "ops-alert")
	keys.err = context.DeadlineExceeded
	srv := newTestServer(t, keys, newFakeCache())
	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("1"), time.Now()))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, larkpushcode.CodeCredentialStoreUnavailable.RuntimeCode()) {
		t.Fatalf("凭据表不可用应 503 且报凭据存储不可用, got %d %s", code, body)
	}
}

// TestMiddleware_NilCredentialServiceRejects 验证凭据服务未装配时同样失败关闭。
func TestMiddleware_NilCredentialServiceRejects(t *testing.T) {
	srv := newTestServer(t, nil, newFakeCache())
	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("2"), time.Now()))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, larkpushcode.CodeCredentialStoreUnavailable.RuntimeCode()) {
		t.Fatalf("凭据服务为 nil 应 503, got %d %s", code, body)
	}
}

// TestMiddleware_MissingHeadersReject 验证缺少任一签名头时以错误码拒绝（HTTP 200）且错误码为「缺少签名头」。
func TestMiddleware_MissingHeadersReject(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	for _, header := range []string{HeaderAppKey, HeaderTimestamp, HeaderNonce, HeaderSignature} {
		req := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("3"), time.Now())
		req.Header.Del(header)
		code, body, _ := send(t, req)
		if code != http.StatusOK {
			t.Fatalf("缺 %s 应被拒, got %d", header, code)
		}
		if !strings.Contains(body, larkpushcode.CodeSignatureMissing.RuntimeCode()) {
			t.Fatalf("缺 %s 的错误码应为 %s, got %s", header, larkpushcode.CodeSignatureMissing.RuntimeCode(), body)
		}
	}
}

// TestMiddleware_ShortNonceRejects 验证 nonce 长度不足 16 字节时按缺少签名头拒绝。
func TestMiddleware_ShortNonceRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, "short", time.Now()))
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMissing.RuntimeCode()) {
		t.Fatalf("过短 nonce 应被拒 且报缺少签名头, got %d %s", code, body)
	}
}

// TestMiddleware_UnknownAppKeyRejects 验证 app_key 查不到启用凭据（不存在 / 已停用 / 已删除）时以签名错误码拒绝（HTTP 200）。
func TestMiddleware_UnknownAppKeyRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k9", testSecret, validNonce("4"), time.Now()))
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureUnknownAppKey.RuntimeCode()) {
		t.Fatalf("未知 app_key 应被拒 且报 app_key 未知, got %d %s", code, body)
	}
}

// TestMiddleware_ExpiredTimestampRejects 验证时间戳超出 ±300 秒窗口时以签名错误码拒绝（HTTP 200）。
func TestMiddleware_ExpiredTimestampRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	for name, drift := range map[string]time.Duration{
		"过期 301 秒": -301 * time.Second,
		"超前 301 秒": 301 * time.Second,
	} {
		req := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("5"), time.Now().Add(drift))
		code, body, _ := send(t, req)
		if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureTimestampInvalid.RuntimeCode()) {
			t.Fatalf("%s 应被拒 且报时间戳无效, got %d %s", name, code, body)
		}
	}
}

// TestMiddleware_TimestampWithinWindowAccepted 验证窗口边界内的请求仍可通过（避免把边界判错）。
func TestMiddleware_TimestampWithinWindowAccepted(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	req := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("6"), time.Now().Add(-299*time.Second))
	code, body, _ := send(t, req)
	if code != http.StatusOK {
		t.Fatalf("窗口内时间戳应放行, got %d %s", code, body)
	}
}

// TestMiddleware_BodyNotSigned 验证请求体不参与签名：签名后替换请求体仍放行（协议约定，
// 请求体完整性由 HTTPS 保证），处理器读到的是实际发送的请求体。
func TestMiddleware_BodyNotSigned(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	signed := signedRequest(t, http.MethodPost, srv.URL+"/echo", `{"text":"original"}`, "k1", testSecret, validNonce("7"), time.Now())
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/echo", strings.NewReader(`{"text":"replaced"}`))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header = signed.Header
	code, body, _ := send(t, req)
	if code != http.StatusOK || body != `body:{"text":"replaced"}` {
		t.Fatalf("请求体不参与签名，替换后应放行并读到实际请求体, got %d %s", code, body)
	}
}

// TestMiddleware_BodySignatureRejected 验证按请求体摘要计算的签名被拒：待签串末行固定为空串摘要，
// 沿用旧口径（签请求体）的客户端会得到签名不匹配错误，而不是被静默放行。
func TestMiddleware_BodySignatureRejected(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	payload := `{"text":"hello"}`
	req := signedRequest(t, http.MethodPost, srv.URL+"/echo", payload, "k1", testSecret, validNonce("17"), time.Now())
	oldStyle := strings.Join([]string{
		http.MethodPost, "/echo", "", req.Header.Get(HeaderTimestamp), req.Header.Get(HeaderNonce), bodyDigestHex([]byte(payload)),
	}, "\n")
	req.Header.Set(HeaderSignature, SignBody(testSecret, oldStyle))

	code, body, _ := send(t, req)
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMismatch.RuntimeCode()) {
		t.Fatalf("签请求体的旧口径应被拒 mismatch, got %d %s", code, body)
	}
}

// TestMiddleware_TamperedMethodRejects 验证方法也参与签名：换方法即签名失效。
func TestMiddleware_TamperedMethodRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	signed := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("8"), time.Now())
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/probe", strings.NewReader(""))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header = signed.Header
	code, body, _ := send(t, req)
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMismatch.RuntimeCode()) {
		t.Fatalf("换方法应被拒 且报签名不匹配, got %d %s", code, body)
	}
}

// TestMiddleware_TamperedQueryRejects 验证查询串参与签名。
func TestMiddleware_TamperedQueryRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	signed := signedRequest(t, http.MethodGet, srv.URL+"/probe?a=1", "", "k1", testSecret, validNonce("9"), time.Now())
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/probe?a=2", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header = signed.Header
	code, body, _ := send(t, req)
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMismatch.RuntimeCode()) {
		t.Fatalf("改查询串应被拒 且报签名不匹配, got %d %s", code, body)
	}
}

// TestMiddleware_ValidSignaturePasses 验证正确签名放行，并把序号、app_key 与绑定分组放进上下文。
func TestMiddleware_ValidSignaturePasses(t *testing.T) {
	srv := newTestServer(t, entries(
		"k1", testSecret, "ops-alert",
		"k2", "secret-2", "linapro-eval-360",
	), newFakeCache())

	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k2", "secret-2", validNonce("10"), time.Now()))
	if code != http.StatusOK || body != "ok:api-key-2:k2:linapro-eval-360" {
		t.Fatalf("应放行并标 api-key-2、分组 linapro-eval-360, got %d %q", code, body)
	}
}

// TestMiddleware_NonceReplayRejected 验证同一 app_key 的 nonce 第二次使用以 NONCE_REPLAYED 拒绝（HTTP 200）。
func TestMiddleware_NonceReplayRejected(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	nonce := validNonce("replay")

	code, _, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, nonce, time.Now()))
	if code != http.StatusOK {
		t.Fatalf("首次使用应放行, got %d", code)
	}

	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, nonce, time.Now()))
	if code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureNonceReplayed.RuntimeCode()) {
		t.Fatalf("重放应被拒 且报 nonce 重复, got %d %s", code, body)
	}
}

// TestMiddleware_NonceScopedPerAppKey 验证不同 app_key 的相同 nonce 互不影响。
func TestMiddleware_NonceScopedPerAppKey(t *testing.T) {
	srv := newTestServer(t, entries(
		"k1", testSecret, "ops-alert",
		"k2", "secret-2", "ops-alert",
	), newFakeCache())
	nonce := validNonce("shared")

	if code, _, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, nonce, time.Now())); code != http.StatusOK {
		t.Fatalf("k1 首次应放行, got %d", code)
	}
	if code, _, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k2", "secret-2", nonce, time.Now())); code != http.StatusOK {
		t.Fatalf("k2 使用同一 nonce 不应被判重放, got %d", code)
	}
}

// TestMiddleware_SignatureFailureDoesNotConsumeNonce 验证先验签后登记：
// 签名错误的前序请求不会占用 nonce，随后的合法请求仍能通过。
func TestMiddleware_SignatureFailureDoesNotConsumeNonce(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	nonce := validNonce("burn")

	if code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", "wrong-secret", nonce, time.Now())); code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMismatch.RuntimeCode()) {
		t.Fatalf("错误密钥应以 SIGNATURE_MISMATCH 拒绝, got %d %s", code, body)
	}
	if code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, nonce, time.Now())); code != http.StatusOK {
		t.Fatalf("签名失败不应消耗 nonce，合法请求应放行, got %d %s", code, body)
	}
}

// TestMiddleware_CacheUnavailableRejects 验证缓存不可用时失败关闭（503），
// 不降级为「跳过去重放行」。
func TestMiddleware_CacheUnavailableRejects(t *testing.T) {
	cache := newFakeCache()
	cache.incrErr = context.DeadlineExceeded
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), cache)

	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("11"), time.Now()))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, larkpushcode.CodeNonceStoreUnavailable.RuntimeCode()) {
		t.Fatalf("缓存不可用应 503 且报去重存储不可用, got %d %s", code, body)
	}
}

// TestMiddleware_NilCacheRejects 验证缓存依赖为 nil 时同样失败关闭。
func TestMiddleware_NilCacheRejects(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), nil)
	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("12"), time.Now()))
	if code != http.StatusServiceUnavailable || !strings.Contains(body, larkpushcode.CodeNonceStoreUnavailable.RuntimeCode()) {
		t.Fatalf("缓存为 nil 应 503, got %d %s", code, body)
	}
}

// TestMiddleware_BodyReadableAfterSignature 验证中间件不消费请求体：处理器仍能读到完整请求体。
func TestMiddleware_BodyReadableAfterSignature(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	payload := `{"recipients":[{"employee_no":"GZ001198"}],"text":"hello"}`

	code, body, _ := send(t, signedRequest(t, http.MethodPost, srv.URL+"/echo", payload, "k1", testSecret, validNonce("13"), time.Now()))
	if code != http.StatusOK {
		t.Fatalf("合法请求应 200, got %d %s", code, body)
	}
	if body != "body:"+payload {
		t.Fatalf("处理器应读到完整请求体, got %q", body)
	}
}

// TestMiddleware_LargeBodyNotLimited 验证中间件不对请求体设上限（上限由各处理器把关）：
// 1 MB 的请求体照常放行，处理器能读到完整内容。
func TestMiddleware_LargeBodyNotLimited(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())
	payload := strings.Repeat("b", 1<<20)
	req := signedRequest(t, http.MethodPost, srv.URL+"/echo", "", "k1", testSecret, validNonce("15"), time.Now())
	req.Body = io.NopCloser(strings.NewReader(payload))
	req.ContentLength = int64(len(payload))

	code, body, _ := send(t, req)
	if code != http.StatusOK {
		t.Fatalf("大请求体应由处理器而非中间件把关, got %d", code)
	}
	if !strings.HasPrefix(body, "body:bbbb") {
		t.Fatalf("处理器应读到请求体, got prefix %q", body[:9])
	}
}

// TestMiddleware_HeaderCheckBeforeLookup 验证缺签名头的请求不会触发查库。
func TestMiddleware_HeaderCheckBeforeLookup(t *testing.T) {
	keys := entries("k1", testSecret, "ops-alert")
	srv := newTestServer(t, keys, newFakeCache())
	req := signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", testSecret, validNonce("16"), time.Now())
	req.Header.Del(HeaderSignature)
	if code, body, _ := send(t, req); code != http.StatusOK || !strings.Contains(body, larkpushcode.CodeSignatureMissing.RuntimeCode()) {
		t.Fatalf("缺签名头应以 SIGNATURE_MISSING 拒绝, got %d %s", code, body)
	}
	if keys.lookups != 0 {
		t.Fatalf("缺签名头不应查库, lookups=%d", keys.lookups)
	}
}

// TestBuildStringToSign_StableForQueryOrder 验证查询参数顺序不影响待签串
// （客户端按任意顺序拼参数都不影响签名）。
func TestBuildStringToSign_StableForQueryOrder(t *testing.T) {
	first, err := url.Parse("/p?b=2&a=1")
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	second, err := url.Parse("/p?a=1&b=2")
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	a := BuildStringToSign(http.MethodGet, first.Path, first.Query(), "1700000000", "n")
	b := BuildStringToSign(http.MethodGet, second.Path, second.Query(), "1700000000", "n")
	if a != b {
		t.Fatalf("查询参数顺序不同不应影响待签串: %q vs %q", a, b)
	}
	if !strings.HasSuffix(a, "\n"+emptyBodyDigest()) {
		t.Fatalf("待签串末行应固定为空串摘要, got %q", a)
	}
}

// TestBuildStringToSign_CanonicalForm 验证待签串的逐段结构，防止实现漂移导致客户端对不上。
func TestBuildStringToSign_CanonicalForm(t *testing.T) {
	query, err := url.Parse("/x?space=a b&empty=")
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	got := BuildStringToSign(http.MethodPost, "/x", query.Query(), "1700000000", "nonce1234567890")
	want := strings.Join([]string{
		"POST",
		"/x",
		"empty=&space=a+b",
		"1700000000",
		"nonce1234567890",
		emptyBodyDigest(),
	}, "\n")
	if got != want {
		t.Fatalf("待签串不符:\nwant %q\ngot  %q", want, got)
	}
}

// emptyBodyDigest 返回空串的 SHA-256 摘要（测试侧独立计算，校验 EmptyBodyDigest 常量）。
func emptyBodyDigest() string {
	sum := sha256.Sum256(nil)
	return hex.EncodeToString(sum[:])
}

// bodyDigestHex 计算请求体摘要，用于构造「签请求体」的旧口径签名。
func bodyDigestHex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// TestMiddleware_ErrorBodyIsUnifiedEnvelope 验证鉴权失败时响应体是宿主统一结构：HTTP 200、code 非 0、
// errorCode 为具体签名错误码，不混入 HTTP 状态文本（FB-12）。
func TestMiddleware_ErrorBodyIsUnifiedEnvelope(t *testing.T) {
	srv := newTestServer(t, entries("k1", testSecret, "ops-alert"), newFakeCache())

	code, body, _ := send(t, signedRequest(t, http.MethodGet, srv.URL+"/probe", "", "k1", "wrong-secret", validNonce("20"), time.Now()))
	if code != http.StatusOK {
		t.Fatalf("签名错误应 HTTP 200, got %d", code)
	}
	var decoded envelope.Response
	if err := json.Unmarshal([]byte(body), &decoded); err != nil ||
		decoded.Code == 0 || decoded.ErrorCode != larkpushcode.CodeSignatureMismatch.RuntimeCode() {
		t.Fatalf("错误响应体应是统一结构且带 errorCode, got %q (%v)", body, err)
	}
}
