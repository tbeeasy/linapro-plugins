// file_test.go 覆盖 file.go：文件类型推导、图片识别、上传请求构造与错误映射、
// 文件 / 图片消息的 content 与 msg_type。

package larkim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"unsafe"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// pngHeader 是一个最小 PNG 文件头，足以让 http.DetectContentType 识别为 image/png。
var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// fakeFileCreator 是 fileCreator 的假实现：记录请求，返回预置响应/错误。
type fakeFileCreator struct {
	gotReq  *larkimv1.CreateFileReq
	calls   int
	resp    *larkimv1.CreateFileResp
	respErr error
}

func (f *fakeFileCreator) Create(_ context.Context, req *larkimv1.CreateFileReq, _ ...larkcore.RequestOptionFunc) (*larkimv1.CreateFileResp, error) {
	f.calls++
	f.gotReq = req
	if f.respErr != nil {
		return nil, f.respErr
	}
	return f.resp, nil
}

// fakeImageCreator 是 imageCreator 的假实现。
type fakeImageCreator struct {
	calls   int
	resp    *larkimv1.CreateImageResp
	respErr error
}

func (f *fakeImageCreator) Create(_ context.Context, _ *larkimv1.CreateImageReq, _ ...larkcore.RequestOptionFunc) (*larkimv1.CreateImageResp, error) {
	f.calls++
	if f.respErr != nil {
		return nil, f.respErr
	}
	return f.resp, nil
}

// okFileResp 构造上传文件成功响应。
func okFileResp(fileKey string) *larkimv1.CreateFileResp {
	return &larkimv1.CreateFileResp{
		ApiResp: &larkcore.ApiResp{StatusCode: http.StatusOK},
		Data:    &larkimv1.CreateFileRespData{FileKey: larkcore.StringPtr(fileKey)},
	}
}

// okImageResp 构造上传图片成功响应。
func okImageResp(imageKey string) *larkimv1.CreateImageResp {
	return &larkimv1.CreateImageResp{
		ApiResp: &larkcore.ApiResp{StatusCode: http.StatusOK},
		Data:    &larkimv1.CreateImageRespData{ImageKey: larkcore.StringPtr(imageKey)},
	}
}

// fileBodyOf 用反射读回上传文件请求体（官方 SDK 把它挂在非导出的 apiReq.Body 上）。
func fileBodyOf(t *testing.T, f *fakeFileCreator) *larkimv1.CreateFileReqBody {
	t.Helper()
	if f.gotReq == nil {
		t.Fatal("未收到上传请求")
	}
	apiReq := reflect.ValueOf(f.gotReq).Elem().FieldByName("apiReq")
	if !apiReq.IsValid() || apiReq.IsNil() {
		t.Fatal("请求缺少 apiReq")
	}
	body := apiReq.Elem().FieldByName("Body")
	if !body.IsValid() || body.IsNil() {
		t.Fatal("请求体为空")
	}
	typed, ok := reflect.NewAt(body.Type(), unsafe.Pointer(body.UnsafeAddr())).Elem().Interface().(*larkimv1.CreateFileReqBody)
	if !ok {
		t.Fatal("请求体类型不符")
	}
	return typed
}

// TestFileTypeOf 验证扩展名映射：大小写不敏感，未知扩展名为 stream。
func TestFileTypeOf(t *testing.T) {
	cases := map[string]string{
		"report.PDF":   FileTypePDF,
		"a.docx":       FileTypeDoc,
		"b.xls":        FileTypeXls,
		"c.pptx":       FileTypePpt,
		"d.mp4":        FileTypeMP4,
		"e.opus":       FileTypeOpus,
		"f.zip":        FileTypeStream,
		"no-extension": FileTypeStream,
	}
	for name, want := range cases {
		if got := FileTypeOf(name); got != want {
			t.Errorf("FileTypeOf(%q)=%q, want %q", name, got, want)
		}
	}
}

// TestIsUploadableImage 验证按内容识别图片，且超过 10 MB 的图片不按图片上传。
func TestIsUploadableImage(t *testing.T) {
	if !IsUploadableImage(pngHeader) {
		t.Fatal("PNG 应识别为可上传图片")
	}
	if IsUploadableImage([]byte("%PDF-1.7 hello")) {
		t.Fatal("PDF 不应识别为图片")
	}
	if IsUploadableImage(nil) {
		t.Fatal("空内容不应识别为图片")
	}
	big := make([]byte, MaxImageBytes+1)
	copy(big, pngHeader)
	if IsUploadableImage(big) {
		t.Fatal("超过 10 MB 的图片应按文件发送")
	}
}

// TestUploadFile_Success 验证上传请求带 file_type / file_name，并返回 file_key。
func TestUploadFile_Success(t *testing.T) {
	f := &fakeFileCreator{resp: okFileResp("file_v2_abc")}
	c := &Client{files: f}
	key, err := c.UploadFile(context.Background(), FileTypePDF, "report.pdf", []byte("%PDF-1.7"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if key != "file_v2_abc" {
		t.Fatalf("file_key want file_v2_abc, got %q", key)
	}
	body := fileBodyOf(t, f)
	if larkcore.StringValue(body.FileType) != FileTypePDF || larkcore.StringValue(body.FileName) != "report.pdf" {
		t.Fatalf("上传参数不符: type=%q name=%q", larkcore.StringValue(body.FileType), larkcore.StringValue(body.FileName))
	}
}

// TestUploadFile_FeishuError 验证飞书拒绝上传时返回带 code / req_id 的结构化错误。
func TestUploadFile_FeishuError(t *testing.T) {
	f := &fakeFileCreator{resp: &larkimv1.CreateFileResp{
		ApiResp: &larkcore.ApiResp{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{larkcore.HttpHeaderKeyLogId: []string{"req-up"}},
		},
		CodeError: larkcore.CodeError{Code: 234001, Msg: "invalid file"},
	}}
	c := &Client{files: f}
	_, err := c.UploadFile(context.Background(), FileTypeStream, "a.bin", []byte("x"))
	var fe *FeishuError
	if !errors.As(err, &fe) {
		t.Fatalf("应返回 FeishuError，got %v", err)
	}
	if fe.Code != 234001 || fe.ReqID != "req-up" {
		t.Fatalf("错误字段不符: %+v", fe)
	}
}

// TestUploadFile_InvalidSize 验证空文件与超限文件不发起请求。
func TestUploadFile_InvalidSize(t *testing.T) {
	f := &fakeFileCreator{resp: okFileResp("k")}
	c := &Client{files: f}
	if _, err := c.UploadFile(context.Background(), FileTypeStream, "a.bin", nil); err == nil {
		t.Fatal("空文件应报错")
	}
	if _, err := c.UploadFile(context.Background(), FileTypeStream, "a.bin", make([]byte, MaxFileBytes+1)); err == nil {
		t.Fatal("超限文件应报错")
	}
	if f.calls != 0 {
		t.Fatalf("参数错误不应发起上传，calls=%d", f.calls)
	}
}

// TestUploadImage_Success 验证图片上传返回 image_key，传输错误被包装返回。
func TestUploadImage_Success(t *testing.T) {
	f := &fakeImageCreator{resp: okImageResp("img_v3_abc")}
	c := &Client{images: f}
	key, err := c.UploadImage(context.Background(), pngHeader)
	if err != nil || key != "img_v3_abc" {
		t.Fatalf("UploadImage: key=%q err=%v", key, err)
	}

	failing := &Client{images: &fakeImageCreator{respErr: errors.New("dial timeout")}}
	if _, err := failing.UploadImage(context.Background(), pngHeader); err == nil {
		t.Fatal("传输失败应返回错误")
	}
}

// TestSendFileAndImage 验证文件 / 图片消息的 msg_type 与 content JSON。
func TestSendFileAndImage(t *testing.T) {
	cases := []struct {
		name    string
		send    func(c *Client) (string, error)
		msgType string
		key     string
		value   string
	}{
		{
			name: "file",
			send: func(c *Client) (string, error) {
				return c.SendFile(context.Background(), ReceiveIDTypeChatID, "oc_1", "file_k")
			},
			msgType: msgTypeFile, key: "file_key", value: "file_k",
		},
		{
			name: "image",
			send: func(c *Client) (string, error) {
				return c.SendImage(context.Background(), ReceiveIDTypeUserID, "u_1", "img_k")
			},
			msgType: msgTypeImage, key: "image_key", value: "img_k",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMessageCreator{resp: okResp("om_x")}
			id, err := tc.send(newTestClient(f))
			if err != nil || id != "om_x" {
				t.Fatalf("send: id=%q err=%v", id, err)
			}
			body := bodyOf(t, f)
			if larkcore.StringValue(body.MsgType) != tc.msgType {
				t.Fatalf("msg_type want %s, got %s", tc.msgType, larkcore.StringValue(body.MsgType))
			}
			var content map[string]string
			if err := json.Unmarshal([]byte(larkcore.StringValue(body.Content)), &content); err != nil {
				t.Fatalf("content 非法: %v", err)
			}
			if content[tc.key] != tc.value {
				t.Fatalf("content[%s] want %s, got %v", tc.key, tc.value, content)
			}
		})
	}
}

// TestSendFile_EmptyKey 验证空 key 不发起请求。
func TestSendFile_EmptyKey(t *testing.T) {
	f := &fakeMessageCreator{resp: okResp("om_x")}
	c := newTestClient(f)
	if _, err := c.SendFile(context.Background(), ReceiveIDTypeChatID, "oc_1", " "); err == nil {
		t.Fatal("空 file_key 应报错")
	}
	if _, err := c.SendImage(context.Background(), ReceiveIDTypeChatID, "oc_1", ""); err == nil {
		t.Fatal("空 image_key 应报错")
	}
	if f.calls != 0 {
		t.Fatalf("参数错误不应发起请求，calls=%d", f.calls)
	}
}
