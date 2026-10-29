package larkim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// fakeMessageCreator 是 messageCreator 的假实现：记录收到的请求，返回预置响应/错误。
// 用于在不真正访问飞书的前提下校验请求构造与结果映射。
type fakeMessageCreator struct {
	gotReq  *larkimv1.CreateMessageReq
	calls   int
	resp    *larkimv1.CreateMessageResp
	respErr error
}

func (f *fakeMessageCreator) Create(_ context.Context, req *larkimv1.CreateMessageReq, _ ...larkcore.RequestOptionFunc) (*larkimv1.CreateMessageResp, error) {
	f.calls++
	f.gotReq = req
	if f.respErr != nil {
		return nil, f.respErr
	}
	return f.resp, nil
}

// okResp 构造一条飞书成功响应。
func okResp(messageID string) *larkimv1.CreateMessageResp {
	return &larkimv1.CreateMessageResp{
		ApiResp: &larkcore.ApiResp{
			StatusCode: http.StatusOK,
			Header:     http.Header{larkcore.HttpHeaderKeyLogId: []string{"log-ok"}},
		},
		Data: &larkimv1.CreateMessageRespData{MessageId: larkcore.StringPtr(messageID)},
	}
}

// errResp 构造一条飞书业务失败响应（如接收者不在应用可见范围）。
func errResp(code int, msg, logID string) *larkimv1.CreateMessageResp {
	return &larkimv1.CreateMessageResp{
		ApiResp: &larkcore.ApiResp{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{larkcore.HttpHeaderKeyLogId: []string{logID}},
		},
		CodeError: larkcore.CodeError{Code: code, Msg: msg},
	}
}

// newTestClient 返回注入了假发送接缝的 Client。
func newTestClient(f *fakeMessageCreator) *Client {
	return &Client{messages: f}
}

// bodyOf 读回请求体。
//
// 官方 SDK 的 Build() 把请求体挂在非导出的 apiReq.Body 上，导出的 Body 字段始终为 nil，
// 故这里用反射从 apiReq 取回；只读不改，字段缺失时直接判失败。
func bodyOf(t *testing.T, f *fakeMessageCreator) *larkimv1.CreateMessageReqBody {
	t.Helper()
	if f.gotReq == nil {
		t.Fatal("未收到发送请求")
	}
	apiReq := reflect.ValueOf(f.gotReq).Elem().FieldByName("apiReq")
	if !apiReq.IsValid() || apiReq.IsNil() {
		t.Fatal("请求缺少 apiReq")
	}
	body := apiReq.Elem().FieldByName("Body")
	if !body.IsValid() || body.IsNil() {
		t.Fatal("请求体为空")
	}
	typed, ok := reflect.NewAt(body.Type(), unsafe.Pointer(body.UnsafeAddr())).Elem().Interface().(*larkimv1.CreateMessageReqBody)
	if !ok {
		t.Fatal("请求体类型不符")
	}
	return typed
}

// receiveIDTypeOf 读回请求里的 receive_id_type 查询参数。
// 官方 SDK 把它藏在 CreateMessageReq 的非导出字段 apiReq（*larkcore.ApiReq）里，
// 没有导出访问器，故用反射读取——只读、不改，且字段缺失时直接判失败。
func receiveIDTypeOf(t *testing.T, f *fakeMessageCreator) string {
	t.Helper()
	if f.gotReq == nil {
		t.Fatal("未收到发送请求")
	}
	apiReq := reflect.ValueOf(f.gotReq).Elem().FieldByName("apiReq")
	if !apiReq.IsValid() || apiReq.IsNil() {
		t.Fatal("请求缺少 apiReq")
	}
	qp := reflect.NewAt(apiReq.Elem().FieldByName("QueryParams").Type(),
		unsafe.Pointer(apiReq.Elem().FieldByName("QueryParams").UnsafeAddr())).Elem()
	got := qp.MapIndex(reflect.ValueOf("receive_id_type"))
	if !got.IsValid() {
		return ""
	}
	// QueryParams 是 map[string][]string，取首元素（与 QueryParams.Get 语义一致）。
	vs, ok := got.Interface().([]string)
	if !ok || len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// TestSendText_Success 验证成功路径：以 msg_type=text 发出，返回飞书 message_id。
func TestSendText_Success(t *testing.T) {
	f := &fakeMessageCreator{resp: okResp("om_123")}
	got, err := newTestClient(f).SendText(context.Background(), ReceiveIDTypeUserID, "u_1", "服务器 CPU 超过 90%")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got != "om_123" {
		t.Fatalf("message_id want om_123, got %q", got)
	}
	body := bodyOf(t, f)
	if b := larkcore.StringValue(body.MsgType); b != msgTypeText {
		t.Fatalf("msg_type want %q, got %q", msgTypeText, b)
	}
	if b := larkcore.StringValue(body.ReceiveId); b != "u_1" {
		t.Fatalf("receive_id want u_1, got %q", b)
	}
	if got := receiveIDTypeOf(t, f); got != ReceiveIDTypeUserID {
		t.Fatalf("receive_id_type want %q, got %q", ReceiveIDTypeUserID, got)
	}
	want := `{"text":"服务器 CPU 超过 90%"}`
	if b := larkcore.StringValue(body.Content); b != want {
		t.Fatalf("content want %s, got %s", want, b)
	}
}

// TestSendText_FeishuError 验证飞书业务失败时错误信息带上 code 与 req_id，便于定位。
func TestSendText_FeishuError(t *testing.T) {
	f := &fakeMessageCreator{resp: errResp(230002, "user not in app availability", "req-abc")}
	_, err := newTestClient(f).SendText(context.Background(), ReceiveIDTypeUserID, "u_1", "hello")
	if err == nil {
		t.Fatal("飞书返回失败时应返回错误")
	}
	msg := err.Error()
	for _, want := range []string{"req-abc", "230002", "user not in app availability"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息缺少 %q: %s", want, msg)
		}
	}
}

// TestSendText_TransportError 验证底层请求失败被包装为错误而不是当作成功。
func TestSendText_TransportError(t *testing.T) {
	f := &fakeMessageCreator{respErr: errors.New("dial tcp timeout")}
	if _, err := newTestClient(f).SendText(context.Background(), ReceiveIDTypeUserID, "u_1", "hi"); err == nil {
		t.Fatal("底层请求失败时应返回错误")
	}
}

// TestSendText_MissingMessageID 验证飞书成功但不返回 message_id 时判为错误。
func TestSendText_MissingMessageID(t *testing.T) {
	f := &fakeMessageCreator{resp: &larkimv1.CreateMessageResp{
		ApiResp: &larkcore.ApiResp{StatusCode: http.StatusOK, Header: http.Header{}},
		Data:    &larkimv1.CreateMessageRespData{},
	}}
	if _, err := newTestClient(f).SendText(context.Background(), ReceiveIDTypeUserID, "u_1", "hi"); err == nil {
		t.Fatal("缺少 message_id 时应返回错误")
	}
}

// TestSendText_EmptyReceiver 验证接收者缺参时不发起请求。
func TestSendText_EmptyReceiver(t *testing.T) {
	f := &fakeMessageCreator{resp: okResp("om_1")}
	if _, err := newTestClient(f).SendText(context.Background(), "", "", "hi"); err == nil {
		t.Fatal("接收者为空时应返回错误")
	}
	if f.calls != 0 {
		t.Fatalf("参数非法时不应发起飞书请求，实际调用 %d 次", f.calls)
	}
}

// TestSendText_InvalidText 验证空文本与超长文本在发送前被拦下，不发起请求。
func TestSendText_InvalidText(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "空文本", text: ""},
		{name: "仅空白", text: "  \n\t "},
		{name: "超长", text: strings.Repeat("字", MaxTextRunes+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeMessageCreator{resp: okResp("om_1")}
			if _, err := newTestClient(f).SendText(context.Background(), ReceiveIDTypeUserID, "u_1", c.text); err == nil {
				t.Fatal("非法文本应返回错误")
			}
			if f.calls != 0 {
				t.Fatalf("非法文本不应发起飞书请求，实际调用 %d 次", f.calls)
			}
		})
	}
}

// TestBuildTextContent_SpecialChars 验证引号、反斜杠、换行、制表符与 emoji 编码后
// 仍是合法 JSON 且往返一致。
func TestBuildTextContent_SpecialChars(t *testing.T) {
	text := "引号\" 反斜杠\\ 换行\n制表\t控制 emoji😀 生僻字𠮷"
	content, err := BuildTextContent(text)
	if err != nil {
		t.Fatalf("BuildTextContent: %v", err)
	}
	if !json.Valid([]byte(content)) {
		t.Fatalf("产出的不是合法 JSON: %s", content)
	}
	var decoded textContent
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatalf("解析 content 失败: %v", err)
	}
	if decoded.Text != text {
		t.Fatalf("往返不一致:\n want %q\n got  %q", text, decoded.Text)
	}
}

// TestBuildTextContent_LengthBoundary 验证长度上限按字符计：恰好上限通过，多一字符失败。
func TestBuildTextContent_LengthBoundary(t *testing.T) {
	atLimit := strings.Repeat("字", MaxTextRunes)
	if _, err := BuildTextContent(atLimit); err != nil {
		t.Fatalf("恰好达到上限应通过: %v", err)
	}
	if _, err := BuildTextContent(atLimit + "字"); err == nil {
		t.Fatal("超过上限应返回错误")
	}
}

// TestBuildTextContent_MultibyteCounting 验证 emoji 按字符而非字节计长度：
// 2000 个 emoji 是 8000 字节，但仍在字符上限内。
func TestBuildTextContent_MultibyteCounting(t *testing.T) {
	text := strings.Repeat("😀", MaxTextRunes)
	if utf8.RuneCountInString(text) != MaxTextRunes {
		t.Fatalf("用例构造错误：字符数 %d", utf8.RuneCountInString(text))
	}
	if _, err := BuildTextContent(text); err != nil {
		t.Fatalf("按字符计应通过: %v", err)
	}
}

// TestBuildTextContent_InvalidUTF8 验证非法 UTF-8 字节被拒绝（JSON 编码会把它们
// 替换成 U+FFFD，从而静默损坏原文）。
func TestBuildTextContent_InvalidUTF8(t *testing.T) {
	if _, err := BuildTextContent("坏字节\xff\xfe"); err == nil {
		t.Fatal("非法 UTF-8 应返回错误")
	}
}
