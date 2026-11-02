// push_test.go 覆盖对外推送接口控制器：结果投影、签名鉴权接入、请求体上限与统一响应结构（FB-12）。

package push

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"

	v1 "lina-plugin-linapro-lark-push/backend/api/push/v1"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/bodylimit"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
	"lina-plugin-linapro-lark-push/backend/internal/service/apikey"
	"lina-plugin-linapro-lark-push/backend/internal/testutil/envelope"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/plugin/capability/cachecap"
)

// TestToResponse_OnlyFailedItems 验证响应只列失败项与飞书错误三元组（FB-11），
// 成功明细与推送应用、错误详情等不回给调用方。
func TestToResponse_OnlyFailedItems(t *testing.T) {
	res := toResponse(larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		{EmployeeNo: "E1", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusSent, MessageID: "om_1"},
		{EmployeeNo: "E1", PushAppID: "cli_push_b", Status: larkpushcap.DeliveryStatusFailed,
			FeishuCode: 230002, FeishuMsg: "bot not in chat", FeishuReqID: "req-1", Error: "not in availability"},
	}})

	if res.AllSucceeded {
		t.Fatal("有失败条时 AllSucceeded 应为 false")
	}
	want := []*v1.FailedRes{{EmployeeNo: "E1", LarkCode: 230002, LarkMsg: "bot not in chat", LarkReqID: "req-1"}}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Fatalf("failed want %+v, got %+v", want, res.Failed)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"cli_push", "om_1", "not in availability", "deliveries"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("响应不应包含 %q: %s", leaked, raw)
		}
	}
}

// TestToResponse_AllSucceededOmitsFailed 验证全部成功时只返回 all_succeeded。
func TestToResponse_AllSucceededOmitsFailed(t *testing.T) {
	res := toResponse(larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		{EmployeeNo: "E1", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusSent, MessageID: "om_1"},
	}})
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"all_succeeded":true}` {
		t.Fatalf("全部成功时响应应只有 all_succeeded, got %s", raw)
	}
}

// TestToResponse_NoIdentity 验证 no_identity 只在此人所有企业都无身份时才算一个失败项：
// 只在一家企业有身份是按企业扇出的正常结果，不能让整次请求变成 502。
func TestToResponse_NoIdentity(t *testing.T) {
	res := toResponse(larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		// E1 在 A 有身份并送达、在 B 无身份：成功。
		{EmployeeNo: "E1", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusSent},
		{EmployeeNo: "E1", PushAppID: "cli_push_b", Status: larkpushcap.DeliveryStatusNoIdentity},
		// E2 两家都无身份：一个失败项。
		{EmployeeNo: "E2", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusNoIdentity},
		{EmployeeNo: "E2", PushAppID: "cli_push_b", Status: larkpushcap.DeliveryStatusNoIdentity},
		// E3 在 A 被飞书拒绝、在 B 无身份：只报飞书失败那条。
		{EmployeeNo: "E3", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusFailed, FeishuCode: 99991663},
		{EmployeeNo: "E3", PushAppID: "cli_push_b", Status: larkpushcap.DeliveryStatusNoIdentity},
	}})
	want := []*v1.FailedRes{
		{EmployeeNo: "E2", LarkMsg: "no_identity"},
		{EmployeeNo: "E3", LarkCode: 99991663, LarkMsg: "failed"},
	}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Fatalf("failed want %+v, got %+v", want, res.Failed)
	}
	if res.AllSucceeded {
		t.Fatal("有失败项时 AllSucceeded 应为 false")
	}

	// 只有「一家送达、另一家无身份」时整体成功。
	ok := toResponse(larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		{EmployeeNo: "E1", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusSent},
		{EmployeeNo: "E1", PushAppID: "cli_push_b", Status: larkpushcap.DeliveryStatusNoIdentity},
	}})
	if !ok.AllSucceeded || len(ok.Failed) != 0 {
		t.Fatalf("一家送达、另一家无身份应判为成功, got %+v", ok)
	}
}

// TestToResponse_RecipientIdentifier 验证失败项按寻址方式标识收件人：工号寻址不回解析出的 user_id，
// user_id 寻址回 user_id，群聊回 chat_id；非飞书侧原因的 lark_msg 为失败状态。
func TestToResponse_RecipientIdentifier(t *testing.T) {
	res := toResponse(larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		{EmployeeNo: "E1", UserID: "on_resolved", Status: larkpushcap.DeliveryStatusCollision},
		{UserID: "on_direct", Status: larkpushcap.DeliveryStatusFailed, FeishuCode: 230013, FeishuMsg: "no permission"},
		{ChatID: "oc_x", Status: larkpushcap.DeliveryStatusFailed, FeishuCode: 230001, FeishuMsg: "chat not found", FeishuReqID: "req-2"},
		{EmployeeNo: "E2", Status: larkpushcap.DeliveryStatusUserIDMissing},
	}})
	want := []*v1.FailedRes{
		{EmployeeNo: "E1", LarkMsg: "collision"},
		{UserID: "on_direct", LarkCode: 230013, LarkMsg: "no permission"},
		{ChatID: "oc_x", LarkCode: 230001, LarkMsg: "chat not found", LarkReqID: "req-2"},
		{EmployeeNo: "E2", LarkMsg: "user_id_missing"},
	}
	if !reflect.DeepEqual(res.Failed, want) {
		t.Fatalf("failed want %+v, got %+v", want, res.Failed)
	}
}

// TestToResponse_EmptyDeliveries 验证空明细不判为成功（空请求本身就是失败）。
func TestToResponse_EmptyDeliveries(t *testing.T) {
	res := toResponse(larkpushcap.SendResult{})
	if res.AllSucceeded {
		t.Fatal("无任何明细时不应判为全部成功")
	}
	if len(res.Failed) != 0 {
		t.Fatalf("无明细时不应有失败项, got %+v", res.Failed)
	}
}

// TestToStatusResponse_ProjectsEveryField 验证自检报告字段被完整投影到响应。
func TestToStatusResponse_ProjectsEveryField(t *testing.T) {
	res := toStatusResponse(larkpushcap.StatusReport{
		PluginReady:  true,
		ConfigValid:  false,
		ConfigIssues: []string{"push_groups.linapro-eval-360[0].identity_app_id: 悬空"},
		PushApps: []larkpushcap.PushAppStatus{{
			Group: "linapro-eval-360",
			AppID: "cli_push_a", Name: "A", IdentityAppID: "cli_a",
			CredentialReady:      true,
			AddressableEmployees: 12, BlankUserIDEmployees: 3, Collisions: 1,
		}},
	})
	if !res.PluginReady || res.ConfigValid {
		t.Fatalf("布尔字段投影不符: %+v", res)
	}
	if len(res.ConfigIssues) != 1 || len(res.PushApps) != 1 {
		t.Fatalf("清单投影不符: %+v", res)
	}
	app := res.PushApps[0]
	if app.Group != "linapro-eval-360" {
		t.Fatalf("分组投影不符: %+v", app)
	}
	if app.AddressableEmployees != 12 || app.BlankUserIDRows != 3 || app.Collisions != 1 {
		t.Fatalf("计数投影不符: %+v", app)
	}
}

// recordingSender 是 larkpushcap.Sender 的测试替身：记录收到的请求；result / err 非零时
// 作为 SendText / SendFile 的返回值，否则返回一条成功明细。
type recordingSender struct {
	reqs     []larkpushcap.SendTextRequest
	fileReqs []larkpushcap.SendFileRequest
	result   *larkpushcap.SendResult
	err      error
	status   larkpushcap.StatusReport
}

// outcome 返回替身配置的投递结果。
func (s *recordingSender) outcome() (larkpushcap.SendResult, error) {
	if s.err != nil {
		return larkpushcap.SendResult{}, s.err
	}
	if s.result != nil {
		return *s.result, nil
	}
	return larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{{Status: larkpushcap.DeliveryStatusSent}}}, nil
}

// SendText 记录文本投递请求。
func (s *recordingSender) SendText(_ context.Context, req larkpushcap.SendTextRequest) (larkpushcap.SendResult, error) {
	s.reqs = append(s.reqs, req)
	return s.outcome()
}

func (s *recordingSender) SendCard(context.Context, larkpushcap.SendCardRequest) (larkpushcap.SendResult, error) {
	return larkpushcap.SendResult{}, nil
}

// SendFile 记录文件投递请求。
func (s *recordingSender) SendFile(_ context.Context, req larkpushcap.SendFileRequest) (larkpushcap.SendResult, error) {
	s.fileReqs = append(s.fileReqs, req)
	return s.outcome()
}

func (s *recordingSender) ListPushApps(context.Context, string) ([]larkpushcap.PushApp, error) {
	return nil, nil
}

// Status 返回替身配置的自检报告。
func (s *recordingSender) Status(context.Context) larkpushcap.StatusReport {
	return s.status
}

// testAppKey 与 testSecret 是本文件测试用的签名凭据（非真实凭据）；testChatAppKey 是另一把
// 在后台配置了群聊的凭据，两把共用同一 secret。
const (
	testAppKey     = "ops-alert-client"
	testChatAppKey = "ops-alert-chat-client"
	testSecret     = "controller-unit-test-secret"
	testChatID     = "oc_ops_configured"
)

// keyStore 是 apikey.Service 的替身：两把启用凭据都绑定 ops-alert 分组，testChatAppKey 另配置了群聊。
type keyStore struct {
	apikey.Service
}

// FindEnabledByAppKey 返回固定的签名凭据。
func (keyStore) FindEnabledByAppKey(_ context.Context, appKey string) (*apikey.Credential, error) {
	switch appKey {
	case testAppKey:
		return &apikey.Credential{Id: 1, AppKey: testAppKey, Secret: testSecret, Group: "ops-alert"}, nil
	case testChatAppKey:
		return &apikey.Credential{
			Id: 2, AppKey: testChatAppKey, Secret: testSecret, Group: "ops-alert",
			ChatID: testChatID, ChatPushAppID: "cli_push_a",
		}, nil
	default:
		return nil, nil
	}
}

// memoryCache 是 cachecap.Service 的最小内存替身，用于让 nonce 去重可用。
type memoryCache struct {
	counts map[string]int64
}

// newMemoryCache 构造一个空的内存缓存替身。
func newMemoryCache() *memoryCache {
	return &memoryCache{counts: make(map[string]int64)}
}

// Incr 累加计数并返回最新快照。
func (c *memoryCache) Incr(
	_ context.Context,
	namespace string,
	key string,
	delta int64,
	_ time.Duration,
) (*cachecap.CacheItem, error) {
	full := namespace + "/" + key
	c.counts[full] += delta
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: c.counts[full]}, nil
}

// Get 返回已写入的计数条目。
func (c *memoryCache) Get(_ context.Context, namespace, key string) (*cachecap.CacheItem, bool, error) {
	value, ok := c.counts[namespace+"/"+key]
	if !ok {
		return nil, false, nil
	}
	return &cachecap.CacheItem{Key: key, ValueKind: cachecap.CacheValueKindInt, IntValue: value}, true, nil
}

// GetMany 是本测试不需要的批量读取。
func (c *memoryCache) GetMany(context.Context, cachecap.GetManyInput) (*cachecap.GetManyOutput, error) {
	return &cachecap.GetManyOutput{}, nil
}

// Set 是本测试不需要的字符串写入。
func (c *memoryCache) Set(context.Context, string, string, string, time.Duration) (*cachecap.CacheItem, error) {
	return &cachecap.CacheItem{}, nil
}

// SetMany 是本测试不需要的批量写入。
func (c *memoryCache) SetMany(context.Context, cachecap.SetManyInput) (*cachecap.SetManyOutput, error) {
	return &cachecap.SetManyOutput{}, nil
}

// Delete 删除计数条目。
func (c *memoryCache) Delete(_ context.Context, namespace, key string) error {
	delete(c.counts, namespace+"/"+key)
	return nil
}

// DeleteMany 批量删除计数条目。
func (c *memoryCache) DeleteMany(_ context.Context, in cachecap.DeleteManyInput) error {
	for _, key := range in.Keys {
		delete(c.counts, in.Namespace+"/"+key)
	}
	return nil
}

// Expire 只返回过期时间，测试不依赖真实过期。
func (c *memoryCache) Expire(_ context.Context, _, _ string, ttl time.Duration) (bool, *time.Time, error) {
	expireAt := time.Now().Add(ttl)
	return true, &expireAt, nil
}

// 与 plugin.go 一致的请求体上限：文本 256 KB；文件 30 MB + 1 MB multipart 封装。
const (
	testTextLimit = 256 << 10
	testFileLimit = 31 << 20
)

// newPushServer 启动按生产装配的最小服务：统一响应（测试替身）→ 签名 → 按路由请求体上限 → Bind 控制器。
// sender 为 nil 时模拟推送能力未就绪。
func newPushServer(t *testing.T, sender larkpushcap.Sender) string {
	t.Helper()
	s := ghttp.GetServer("push-controller-test-" + t.Name())
	s.SetAddr("127.0.0.1:0")
	s.SetDumpRouterMap(false)
	c := NewV1(sender)
	cache := newMemoryCache()
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(
			envelope.Middleware(),
			signature.Middleware(keyStore{}, cache, nil),
			bodylimit.Middleware(
				bodylimit.Rule{PathSuffix: "/message/text", Limit: testTextLimit},
				bodylimit.Rule{PathSuffix: "/message/file", Limit: testFileLimit},
			),
		)
		group.Bind(c)
	})
	if err := s.Start(); err != nil {
		t.Fatalf("启动测试服务: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	return "http://" + s.GetListenedAddress()
}

// nextNonce 生成一个 32 字符的随机 nonce（满足 16~64 长度要求）。
//
// 用随机值而不是包级递增计数器：计数器会让测试之间产生隐式耦合（同一测试内的第二次调用
// 反而会与第一次相同而被判重放），随机值则天然自包含、顺序无关。
func nextNonce(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("生成 nonce: %v", err)
	}
	return hex.EncodeToString(buf)
}

// apiResponse 是统一响应结构的解码目标，data 留作原始 JSON 供各用例按需解码。
type apiResponse struct {
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	ErrorCode string          `json:"errorCode"`
}

// call 是一次请求的 HTTP 状态码与解码后的统一响应。
type call struct {
	status int
	res    apiResponse
	raw    string
}

// postText 以默认凭据（未配置群聊）提交 JSON 请求体。
func postText(t *testing.T, baseURL, body string) call {
	t.Helper()
	return doSigned(t, http.MethodPost, testAppKey, baseURL+"/message/text", "application/json", []byte(body))
}

// postFile 以默认凭据向文件接口提交 multipart 请求体。
func postFile(t *testing.T, baseURL, contentType string, body []byte) call {
	t.Helper()
	return doSigned(t, http.MethodPost, testAppKey, baseURL+"/message/file", contentType, body)
}

// doSigned 以指定 app_key 签名发起请求并解码统一响应。待签串只含方法、路径、查询串、时间戳与
// nonce（末行固定为空串摘要）。
func doSigned(t *testing.T, method, appKey, target, contentType string, body []byte) call {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("解析 URL: %v", err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := nextNonce(t)
	toSign := signature.BuildStringToSign(method, u.Path, u.Query(), timestamp, nonce)

	req, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set(signature.HeaderAppKey, appKey)
	req.Header.Set(signature.HeaderTimestamp, timestamp)
	req.Header.Set(signature.HeaderNonce, nonce)
	req.Header.Set(signature.HeaderSignature, signature.SignBody(testSecret, toSign))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return do(t, req)
}

// do 执行请求并把响应体解码为统一响应；响应体不是 JSON 时直接判失败（所有响应都必须是统一结构）。
func do(t *testing.T, req *http.Request) call {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体: %v", err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatalf("关闭响应体: %v", err)
	}
	out := call{status: resp.StatusCode, raw: string(raw)}
	if err = json.Unmarshal(raw, &out.res); err != nil {
		t.Fatalf("响应体应是统一结构 JSON, got %d %q (%v)", resp.StatusCode, raw, err)
	}
	return out
}

// assertOK 断言 HTTP 200 且 code=0。
func assertOK(t *testing.T, name string, c call) {
	t.Helper()
	if c.status != http.StatusOK || c.res.Code != 0 || c.res.ErrorCode != "" {
		t.Fatalf("%s: 应 HTTP 200 且 code=0, got %d %s", name, c.status, c.raw)
	}
}

// assertRejected 断言 HTTP 状态码与 errorCode，且 code 非 0。
func assertRejected(t *testing.T, name string, c call, wantStatus int, want *bizerr.Code) {
	t.Helper()
	if c.status != wantStatus || c.res.Code == 0 || c.res.ErrorCode != want.RuntimeCode() {
		t.Fatalf("%s: 应 HTTP %d 且 errorCode=%s, got %d %s", name, wantStatus, want.RuntimeCode(), c.status, c.raw)
	}
}

// TestSendText_GroupFromSignature 验证投递分组取自签名凭据绑定，且请求体试图指定 group 时
// 以参数错误（HTTP 200 + INVALID_PARAM）拒绝、不触发投递；合法请求返回 code=0 与 all_succeeded。
func TestSendText_GroupFromSignature(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	assertRejected(t, "请求体带 group",
		postText(t, baseURL, `{"recipients":[{"employee_no":"E1"}],"text":"hi","group":"linapro-eval-360"}`),
		http.StatusOK, larkpushcode.CodeInvalidParam)
	if len(sender.reqs) != 0 {
		t.Fatalf("被拒请求不应触发投递, got %+v", sender.reqs)
	}

	ok := postText(t, baseURL, `{"recipients":[{"employee_no":"E1"}],"text":"hi"}`)
	assertOK(t, "合法请求", ok)
	if string(ok.res.Data) != `{"all_succeeded":true}` {
		t.Fatalf("全部成功时 data 应只有 all_succeeded, got %s", ok.res.Data)
	}
	if len(sender.reqs) != 1 || sender.reqs[0].Group != "ops-alert" {
		t.Fatalf("投递分组应取自凭据绑定的 ops-alert, got %+v", sender.reqs)
	}
}

// TestSendText_UnsignedRequestRejected 验证未带签名头的请求以 HTTP 200 + SIGNATURE_MISSING 拒绝，
// 响应体为统一结构，且不触发投递。
func TestSendText_UnsignedRequestRejected(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	req, err := http.NewRequest(http.MethodPost, baseURL+"/message/text",
		strings.NewReader(`{"recipients":[{"employee_no":"E1"}],"text":"hi"}`))
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	assertRejected(t, "未签名请求", do(t, req), http.StatusOK, larkpushcode.CodeSignatureMissing)
	if len(sender.reqs) != 0 {
		t.Fatalf("未签名请求不应触发投递, got %+v", sender.reqs)
	}
}

// TestSendText_ChatFromCredential 验证群聊取自签名凭据配置：配置了群聊的凭据，个人收件人之外追加该群；
// 只传正文（不传 recipients）时只发到该群。
func TestSendText_ChatFromCredential(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)
	target := baseURL + "/message/text"

	assertOK(t, "个人 + 群聊", doSigned(t, http.MethodPost, testChatAppKey, target, "application/json",
		[]byte(`{"recipients":[{"employee_no":"E1"}],"text":"hi"}`)))
	assertOK(t, "只发群聊", doSigned(t, http.MethodPost, testChatAppKey, target, "application/json",
		[]byte(`{"text":"only chat"}`)))
	if len(sender.reqs) != 2 {
		t.Fatalf("应投递两次, got %d", len(sender.reqs))
	}
	got := sender.reqs[0].Recipients
	if len(got) != 2 || got[0].EmployeeNo != "E1" || got[1].ChatID != testChatID || got[1].PushAppID != "cli_push_a" {
		t.Fatalf("收件人应为个人 + 凭据配置的群聊: %+v", got)
	}
	if only := sender.reqs[1].Recipients; len(only) != 1 || only[0].ChatID != testChatID {
		t.Fatalf("只发群聊时收件人应只有凭据配置的群: %+v", only)
	}
}

// TestSendText_ClientCannotChooseChat 验证调用方不能指定群聊：请求体带 chats 以未知字段拒绝；
// 凭据未配置群聊又不传 recipients 时拒绝；两者都是参数错误且不触发投递。
func TestSendText_ClientCannotChooseChat(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	assertRejected(t, "请求体带 chats", postText(t, baseURL, `{"chats":[{"chat_id":"oc_any"}],"text":"hi"}`),
		http.StatusOK, larkpushcode.CodeInvalidParam)
	noRecipient := postText(t, baseURL, `{"text":"hi"}`)
	assertRejected(t, "无收件人且凭据未配置群聊", noRecipient, http.StatusOK, larkpushcode.CodeInvalidParam)
	if !strings.Contains(noRecipient.res.Message, "未配置群聊") {
		t.Fatalf("错误信息应说明凭据未配置群聊, got %s", noRecipient.raw)
	}
	if len(sender.reqs) != 0 {
		t.Fatalf("被拒请求不应触发投递, got %+v", sender.reqs)
	}
}

// TestSendText_PartialFailure 验证有收件人投递失败时返回 HTTP 200、code 非 0、
// errorCode=SEND_PARTIAL_FAILED，失败明细在 data.failed（原为 HTTP 502，FB-12）。
func TestSendText_PartialFailure(t *testing.T) {
	sender := &recordingSender{result: &larkpushcap.SendResult{Deliveries: []larkpushcap.Delivery{
		{EmployeeNo: "E1", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusSent},
		{EmployeeNo: "E2", PushAppID: "cli_push_a", Status: larkpushcap.DeliveryStatusFailed,
			FeishuCode: 230013, FeishuMsg: "Bot has NO availability to this user.", FeishuReqID: "req-9"},
	}}}
	baseURL := newPushServer(t, sender)

	c := postText(t, baseURL, `{"recipients":[{"employee_no":"E1"},{"employee_no":"E2"}],"text":"hi"}`)
	assertRejected(t, "部分失败", c, http.StatusOK, larkpushcode.CodeSendPartialFailed)
	var data v1.SendResult
	if err := json.Unmarshal(c.res.Data, &data); err != nil {
		t.Fatalf("data 应是投递结果: %s (%v)", c.res.Data, err)
	}
	want := []*v1.FailedRes{{EmployeeNo: "E2", LarkCode: 230013, LarkMsg: "Bot has NO availability to this user.", LarkReqID: "req-9"}}
	if data.AllSucceeded || !reflect.DeepEqual(data.Failed, want) {
		t.Fatalf("data.failed 应只含失败的 E2, got %s", c.res.Data)
	}
}

// TestSendText_ServerSideErrors 验证服务端问题保留 5xx：推送配置问题（分组无可用推送应用）返回 500，
// 推送能力未就绪返回 503；响应体仍为统一结构并带 errorCode。
func TestSendText_ServerSideErrors(t *testing.T) {
	body := `{"recipients":[{"employee_no":"E1"}],"text":"hi"}`

	noApp := &recordingSender{err: bizerr.NewCode(larkpushcode.CodeNoPushApp, bizerr.P("group", "ops-alert"))}
	assertRejected(t, "分组无推送应用", postText(t, newPushServer(t, noApp), body),
		http.StatusInternalServerError, larkpushcode.CodeNoPushApp)

	t.Run("sender 未就绪", func(t *testing.T) {
		assertRejected(t, "sender 未就绪", postText(t, newPushServer(t, nil), body),
			http.StatusServiceUnavailable, larkpushcode.CodeSenderUnbound)
	})
}

// TestStatus 验证自检接口：正常时 HTTP 200、code=0、data 为自检报告；推送能力未就绪时 HTTP 503。
func TestStatus(t *testing.T) {
	sender := &recordingSender{status: larkpushcap.StatusReport{PluginReady: true, ConfigValid: true}}
	c := doSigned(t, http.MethodGet, testAppKey, newPushServer(t, sender)+"/status", "", nil)
	assertOK(t, "自检", c)
	var data v1.StatusRes
	if err := json.Unmarshal(c.res.Data, &data); err != nil || !data.PluginReady || !data.ConfigValid {
		t.Fatalf("data 应为自检报告, got %s (%v)", c.res.Data, err)
	}

	t.Run("sender 未就绪", func(t *testing.T) {
		c := doSigned(t, http.MethodGet, testAppKey, newPushServer(t, nil)+"/status", "", nil)
		assertRejected(t, "sender 未就绪", c, http.StatusServiceUnavailable, larkpushcode.CodeSenderUnbound)
		if string(c.res.Data) != "null" {
			t.Fatalf("未就绪时 data 应为空，原因由 errorCode 表达, got %s", c.res.Data)
		}
	})
}

// multipartBody 构造文件接口的 multipart 请求体；fields 为普通字段，fileName 为空时不带文件。
func multipartBody(t *testing.T, fields map[string]string, fileName string, content []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			t.Fatalf("写字段 %s: %v", name, err)
		}
	}
	if fileName != "" {
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatalf("创建文件字段: %v", err)
		}
		if _, err = part.Write(content); err != nil {
			t.Fatalf("写文件内容: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("结束 multipart: %v", err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// TestSendFile_MultipartProjected 验证文件接口按签名凭据绑定的分组投递，并把文件、收件人、凭据配置的
// 群聊与业务单号完整交给门面；签名不含请求体。
func TestSendFile_MultipartProjected(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	content := bytes.Repeat([]byte("x"), testTextLimit+10) // 超过文本上限，验证文件接口用自己的上限
	body, contentType := multipartBody(t, map[string]string{
		"recipients": `[{"employee_no":"E1"}]`,
		"biz_ref":    "report-20261028",
	}, "月报.pdf", content)
	assertOK(t, "合法文件请求", doSigned(t, http.MethodPost, testChatAppKey, baseURL+"/message/file", contentType, body))
	if len(sender.fileReqs) != 1 {
		t.Fatalf("应投递一次, got %d", len(sender.fileReqs))
	}
	req := sender.fileReqs[0]
	if req.Group != "ops-alert" || req.FileName != "月报.pdf" || req.BizRef != "report-20261028" {
		t.Fatalf("文件请求字段不符: group=%s name=%s biz_ref=%s", req.Group, req.FileName, req.BizRef)
	}
	if !bytes.Equal(req.Content, content) {
		t.Fatalf("文件内容应完整传递, got %d 字节", len(req.Content))
	}
	if len(req.Recipients) != 2 || req.Recipients[0].EmployeeNo != "E1" || req.Recipients[1].ChatID != testChatID {
		t.Fatalf("收件人应为个人 + 凭据配置的群聊: %+v", req.Recipients)
	}
}

// TestSendFile_InvalidFormRejected 验证未知字段（含试图指定分组或群聊）、缺文件、收件人 JSON 非法、
// 无收件人且凭据未配置群聊、非 multipart 请求体均以参数错误拒绝且不触发投递。
func TestSendFile_InvalidFormRejected(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	cases := map[string]struct {
		fields   map[string]string
		fileName string
	}{
		"带 group 字段":    {fields: map[string]string{"group": "linapro-eval-360"}, fileName: "a.txt"},
		"缺少文件":          {fields: map[string]string{"recipients": `[{"employee_no":"E1"}]`}},
		"recipients 非法": {fields: map[string]string{"recipients": `{"employee_no":"E1"}`}, fileName: "a.txt"},
		"带 chats 字段":    {fields: map[string]string{"chats": `[{"chat_id":"oc_1"}]`}, fileName: "a.txt"},
		"无收件人且未配置群聊":    {fields: map[string]string{"biz_ref": "x"}, fileName: "a.txt"},
	}
	for name, tc := range cases {
		body, contentType := multipartBody(t, tc.fields, tc.fileName, []byte("hello"))
		assertRejected(t, name, postFile(t, baseURL, contentType, body), http.StatusOK, larkpushcode.CodeInvalidParam)
	}
	assertRejected(t, "非 multipart", postFile(t, baseURL, "application/json", []byte(`{}`)),
		http.StatusOK, larkpushcode.CodeInvalidParam)
	if len(sender.fileReqs) != 0 {
		t.Fatalf("被拒请求不应触发投递, got %d", len(sender.fileReqs))
	}
}

// TestSendFile_BodyTooLargeRejected 验证请求体超过 31 MB 时返回 HTTP 413 + BODY_TOO_LARGE 且不触发投递。
func TestSendFile_BodyTooLargeRejected(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	body, contentType := multipartBody(t, map[string]string{"recipients": `[{"employee_no":"E1"}]`}, "big.bin",
		bytes.Repeat([]byte("x"), testFileLimit))
	assertRejected(t, "文件请求体超限", postFile(t, baseURL, contentType, body),
		http.StatusRequestEntityTooLarge, larkpushcode.CodeBodyTooLarge)
	if len(sender.fileReqs) != 0 {
		t.Fatalf("被拒请求不应触发投递, got %d", len(sender.fileReqs))
	}
}

// TestSendText_BodyTooLargeRejected 验证文本接口请求体超过 256 KB 时返回 HTTP 413 + BODY_TOO_LARGE
// 且不触发投递——签名中间件不读请求体，上限由 bodylimit 中间件把关。
func TestSendText_BodyTooLargeRejected(t *testing.T) {
	sender := &recordingSender{}
	baseURL := newPushServer(t, sender)

	body := `{"recipients":[{"employee_no":"E1"}],"text":"` + strings.Repeat("a", testTextLimit) + `"}`
	assertRejected(t, "文本请求体超限", postText(t, baseURL, body),
		http.StatusRequestEntityTooLarge, larkpushcode.CodeBodyTooLarge)
	if len(sender.reqs) != 0 {
		t.Fatalf("被拒请求不应触发投递, got %+v", sender.reqs)
	}
}
