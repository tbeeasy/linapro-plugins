// middleware_payload_test.go 锁定请求体与响应体捕获的边界行为：处理器未读取时可事后读到、
// Body 被读走且未归还时落占位、multipart 不含文件内容、二进制只计数、脱敏在落库前完成。

package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
)

// testMaskFields 返回默认脱敏名单，模拟未追加配置的情形。
func testMaskFields() map[string]struct{} {
	fields := make(map[string]struct{}, len(defaultMaskedFields))
	for name := range defaultMaskedFields {
		fields[name] = struct{}{}
	}
	return fields
}

// TestBuildTextRequestBodyReadsUnparsedBody 锁定：处理器没读 Body 时也能事后读到完整内容。
//
// 这是「提交了但被拒」最重要的一条路径——例如鉴权在解析前就返回 401。
func TestBuildTextRequestBodyReadsUnparsedBody(t *testing.T) {
	payload := `{"larkAppId":"cli_x","code":"2g1abc"}`
	request := &ghttp.Request{
		Request: httptest.NewRequest(
			http.MethodPost,
			"/x/linapro-employee-core/api/v1/emp/login",
			strings.NewReader(payload),
		),
	}
	request.Header.Set("Content-Type", "application/json")

	body := buildRecordRequestBody(request, testMaskFields())
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("请求体应为合法 JSON，实际为 %q: %v", body, err)
	}
	if decoded["larkAppId"] != "cli_x" {
		t.Fatalf("请求体应完整保留 larkAppId，实际为 %v", decoded)
	}
	// 飞书预授权码刻意不脱敏：它是宿主统一响应结构里的 code 同名字段，
	// 全局脱敏会把每一条响应的错误码也变成占位（design D5）。
	if decoded["code"] != "2g1abc" {
		t.Fatalf("code 不应被脱敏，实际为 %v", decoded["code"])
	}
}

// TestBuildTextRequestBodyKeepsUnreadablePlaceholder 锁定：Body 读不出来时落占位而不是 panic。
//
// 触发条件与 design D3 一致：下游关闭了 Body，或路由请求体上限（MaxBytesReader）已 latch 住错误。
// 两种都表现为 Read 返回错误。
func TestBuildTextRequestBodyKeepsUnreadablePlaceholder(t *testing.T) {
	httpRequest := httptest.NewRequest(http.MethodPost, "/x/api", nil)
	httpRequest.Body = errorReader{err: errors.New("use of closed network connection")}
	request := &ghttp.Request{Request: httpRequest}

	body := buildRecordRequestBody(request, testMaskFields())
	if body != placeholderBodyUnreadable {
		t.Fatalf("Body 不可读时应落占位 %q，实际为 %q", placeholderBodyUnreadable, body)
	}
}

// TestBuildTextRequestBodyKeepsNotRetainedPlaceholder 锁定：读完为空但声明了长度时写占位。
func TestBuildTextRequestBodyKeepsNotRetainedPlaceholder(t *testing.T) {
	httpRequest := httptest.NewRequest(http.MethodPost, "/x/api", nil)
	httpRequest.Body = http.NoBody
	httpRequest.ContentLength = 128
	request := &ghttp.Request{Request: httpRequest}

	body := buildRecordRequestBody(request, testMaskFields())
	expected := fmt.Sprintf(placeholderBodyNotRetained, 128)
	if body != expected {
		t.Fatalf("声明了长度却读不到内容时应落 %q，实际为 %q", expected, body)
	}
}

// errorReader 模拟一个读取即失败的请求体（下游已关闭，或 MaxBytesReader 已 latch 住错误）。
type errorReader struct {
	err error
}

// Read 始终返回错误。
func (r errorReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// Close 无操作。
func (r errorReader) Close() error {
	return nil
}

// TestBuildRecordResponseBodyMasksToken 锁定响应体里的 token 落库前被替换。
func TestBuildRecordResponseBodyMasksToken(t *testing.T) {
	payload := `{"code":0,"token":"jwt-value-should-not-persist","name":"张三"}`

	body := sanitizeResponsePayload("application/json", payload, false, testMaskFields())
	if strings.Contains(body, "jwt-value-should-not-persist") {
		t.Fatalf("响应体中的 token 必须被脱敏，实际为 %q", body)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("响应体应为合法 JSON: %v", err)
	}
	if decoded["token"] != redactedValue {
		t.Fatalf("token 应为 %q，实际为 %v", redactedValue, decoded["token"])
	}
	// code 是宿主统一响应结构的整数业务码，不能被脱敏。
	if decoded["code"] != float64(0) {
		t.Fatalf("code 应保留原值，实际为 %v", decoded["code"])
	}
	if decoded["name"] != "张三" {
		t.Fatalf("非敏感字段应原样保留，实际为 %v", decoded["name"])
	}
}

// TestBuildRecordResponseBodyBinaryPlaceholder 锁定二进制响应只记类型与字节数。
func TestBuildRecordResponseBodyBinaryPlaceholder(t *testing.T) {
	body := sanitizeResponsePayload("application/octet-stream", "binary-payload", false, testMaskFields())
	if !strings.HasPrefix(body, "[BINARY application/octet-stream ") {
		t.Fatalf("二进制响应应记占位，实际为 %q", body)
	}
	if strings.Contains(body, "binary-payload") {
		t.Fatalf("二进制内容不得写入记录: %q", body)
	}
}

// TestBuildRecordResponseBodyStreamPlaceholder 锁定已接管连接的响应记 [STREAM]。
func TestBuildRecordResponseBodyStreamPlaceholder(t *testing.T) {
	body := sanitizeResponsePayload("text/event-stream", "data: x", true, testMaskFields())
	if body != placeholderStream {
		t.Fatalf("已 hijack 的响应应记 %q，实际为 %q", placeholderStream, body)
	}
}

// TestSanitizeJSONTextHandlesNestedValues 锁定任意嵌套层级的脱敏与大小写不敏感。
func TestSanitizeJSONTextHandlesNestedValues(t *testing.T) {
	input := `{"Items":[{"Answers":[{"TextValue":"ok","Password":"p1"}]}],"SECRET_KEY":"s"}`
	body := sanitizeJSONText(input, testMaskFields())
	if strings.Contains(body, `"p1"`) || strings.Contains(body, `"s"`) {
		t.Fatalf("嵌套敏感字段必须被脱敏，实际为 %q", body)
	}
	if !strings.Contains(body, "ok") {
		t.Fatalf("非敏感字段应保留，实际为 %q", body)
	}
}

// TestBuildRecordHeadersRedactsAuthorization 锁定员工令牌头被替换。
func TestBuildRecordHeadersRedactsAuthorization(t *testing.T) {
	request := &ghttp.Request{Request: httptest.NewRequest(http.MethodGet, "/x/api", nil)}
	request.Header.Set("Authorization", "Bearer emp-jwt-value")
	request.Header.Set("User-Agent", "test-agent")

	headers := buildRecordHeaders(request, testMaskFields())
	if strings.Contains(headers, "emp-jwt-value") {
		t.Fatalf("Authorization 必须被脱敏，实际为 %q", headers)
	}
	if !strings.Contains(headers, "test-agent") {
		t.Fatalf("非敏感请求头应原样保留，实际为 %q", headers)
	}
}

// TestBuildRecordQueryMasksSensitiveParams 锁定查询串里的敏感参数被替换。
func TestBuildRecordQueryMasksSensitiveParams(t *testing.T) {
	request := &ghttp.Request{
		Request: httptest.NewRequest(http.MethodGet, "/x/api?token=abc&page=1", nil),
	}
	query := buildRecordQuery(request, testMaskFields())
	if strings.Contains(query, "abc") {
		t.Fatalf("查询串中的 token 必须被脱敏，实际为 %q", query)
	}
	if !strings.Contains(query, "page=1") {
		t.Fatalf("非敏感参数应保留，实际为 %q", query)
	}
}

// TestIsMultipartAndBinaryContentType 锁定内容类型分类。
func TestIsMultipartAndBinaryContentType(t *testing.T) {
	if !isMultipartContentType("multipart/form-data; boundary=xyz") {
		t.Fatal("multipart/form-data 应判为 multipart")
	}
	if isMultipartContentType("application/json") {
		t.Fatal("application/json 不是 multipart")
	}
	if !isBinaryContentType("application/octet-stream") {
		t.Fatal("octet-stream 应判为二进制")
	}
	if !isBinaryContentType("image/png") {
		t.Fatal("image/* 应判为二进制")
	}
	if isBinaryContentType("application/json") {
		t.Fatal("application/json 不是二进制")
	}
}

// newMultipartRequest 构造一个含普通字段、敏感字段与一个文件的 multipart 请求。
func newMultipartRequest(t *testing.T, fileContent string) *ghttp.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writer.WriteField("remark", "季度材料"); err != nil {
		t.Fatalf("写入普通字段失败: %v", err)
	}
	if err := writer.WriteField("password", "p@ss"); err != nil {
		t.Fatalf("写入敏感字段失败: %v", err)
	}
	part, err := writer.CreateFormFile("attachment", "report.txt")
	if err != nil {
		t.Fatalf("创建文件字段失败: %v", err)
	}
	if _, err = part.Write([]byte(fileContent)); err != nil {
		t.Fatalf("写入文件内容失败: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("结束 multipart 失败: %v", err)
	}

	httpRequest := httptest.NewRequest(http.MethodPost, "/x/api/upload", &buffer)
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	return &ghttp.Request{Request: httpRequest}
}

// TestBuildMultipartBodyOmitsFileContent 锁定：multipart 只记普通字段与文件元数据，文件内容不落库。
func TestBuildMultipartBodyOmitsFileContent(t *testing.T) {
	const fileContent = "FILE-CONTENT-MUST-NOT-BE-RECORDED"
	request := newMultipartRequest(t, fileContent)
	// 模拟处理器已解析表单（GoFrame 的 Parse 同样走 ParseMultipartForm）。
	if err := request.Request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("解析 multipart 失败: %v", err)
	}

	body := buildRecordRequestBody(request, testMaskFields())
	if strings.Contains(body, fileContent) {
		t.Fatalf("文件内容不应落库，实际为 %q", body)
	}
	expectedFile := fmt.Sprintf("[FILE attachment name=report.txt type=application/octet-stream size=%d]", len(fileContent))
	for _, want := range []string{"remark=季度材料", "password=" + redactedValue, expectedFile} {
		if !strings.Contains(body, want) {
			t.Fatalf("请求体应包含 %q，实际为 %q", want, body)
		}
	}
	if strings.Contains(body, "p@ss") {
		t.Fatalf("multipart 敏感字段应脱敏，实际为 %q", body)
	}
}

// TestBuildMultipartBodyKeepsUnparsedPlaceholder 锁定：处理器在解析前就拒绝时，如实落「未解析」占位，
// 且不会为了记录日志去读取（缓冲）整个上传内容。
func TestBuildMultipartBodyKeepsUnparsedPlaceholder(t *testing.T) {
	const fileContent = "FILE-CONTENT-MUST-NOT-BE-RECORDED"
	request := newMultipartRequest(t, fileContent)

	body := buildRecordRequestBody(request, testMaskFields())
	expected := fmt.Sprintf(placeholderMultipartUnparsed, request.ContentLength)
	if body != expected {
		t.Fatalf("未解析的 multipart 应落 %q，实际为 %q", expected, body)
	}
	if request.Request.MultipartForm != nil {
		t.Fatal("记录日志不应替处理器解析 multipart")
	}
}
