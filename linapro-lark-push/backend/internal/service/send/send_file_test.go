// send_file_test.go 覆盖 send_file.go：按推送应用懒上传且只传一次、图片 / 文件形态判定、
// 上传失败只影响该应用、参数校验。

package send

import (
	"context"
	"errors"
	"testing"

	"lina-core/pkg/bizerr"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// pngBytes 是一个最小 PNG 文件头，足以被识别为图片。
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// countKind 统计某类调用次数（按应用过滤，appID 为空表示不过滤）。
func countKind(calls []sentCall, kind, appID string) int {
	n := 0
	for _, c := range calls {
		if c.kind == kind && (appID == "" || c.appID == appID) {
			n++
		}
	}
	return n
}

// TestSendFile_UploadOncePerApp 验证每个推送应用只上传一次，同应用的多条明细复用同一 file_key，
// 且 PDF 以 file 消息、file_type=pdf 发送。
func TestSendFile_UploadOncePerApp(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1", "E2": "u_a_2"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b_1", "E2": "u_b_2"}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)
	res, err := svc.SendFile(context.Background(), larkpushcap.SendFileRequest{
		Group: testGroup,
		Recipients: []larkpushcap.Recipient{
			{EmployeeNo: "E1"}, {EmployeeNo: "E2"}, {ChatID: "oc_ops", PushAppID: "cli_push_a"},
		},
		FileName: "月报.pdf",
		Content:  []byte("%PDF-1.7 content"),
	})
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if len(res.Deliveries) != 5 || !res.AllSucceeded() {
		t.Fatalf("want 5 条成功明细, got %+v", res.Deliveries)
	}
	if countKind(*calls, "upload-file", "cli_push_a") != 1 || countKind(*calls, "upload-file", "cli_push_b") != 1 {
		t.Fatalf("每个推送应用应只上传一次, got %+v", *calls)
	}
	for _, c := range *calls {
		switch c.kind {
		case "upload-file":
			if c.text != larkim.FileTypePDF+":月报.pdf" {
				t.Fatalf("上传参数不符: %q", c.text)
			}
		case "file":
			if c.text != "file_"+c.appID {
				t.Fatalf("应使用本应用上传得到的 file_key, got %+v", c)
			}
		default:
			t.Fatalf("PDF 不应产生其它调用: %+v", c)
		}
	}
}

// TestSendFile_ImageAsImageMessage 验证图片内容上传为图片并发送 image 消息（即使扩展名不是图片）。
func TestSendFile_ImageAsImageMessage(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}}}
	svc, calls := newService(t, singleAppConfig(), idx, nil)
	res, err := svc.SendFile(context.Background(), larkpushcap.SendFileRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		FileName:   "截图.bin",
		Content:    pngBytes,
	})
	if err != nil || !res.AllSucceeded() {
		t.Fatalf("SendFile: res=%+v err=%v", res, err)
	}
	if countKind(*calls, "upload-image", "") != 1 || countKind(*calls, "image", "") != 1 || countKind(*calls, "upload-file", "") != 0 {
		t.Fatalf("图片应走 upload-image + image, got %+v", *calls)
	}
}

// TestSendFile_NoAddressableNoUpload 验证某企业没有可投递明细时不为该推送应用上传。
func TestSendFile_NoAddressableNoUpload(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1"}},
		"cli_b": {ByEmployeeNo: map[string]string{}},
	}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, nil)
	res, err := svc.SendFile(context.Background(), larkpushcap.SendFileRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}},
		FileName:   "a.txt",
		Content:    []byte("hello"),
	})
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if countKind(*calls, "upload-file", "cli_push_b") != 0 {
		t.Fatalf("企业 B 无身份，不应上传, got %+v", *calls)
	}
	if countKind(*calls, "upload-file", "cli_push_a") != 1 {
		t.Fatalf("企业 A 应上传一次, got %+v", *calls)
	}
	if len(res.Deliveries) != 2 {
		t.Fatalf("want 2 条明细（A 已发、B 无身份）, got %+v", res.Deliveries)
	}
}

// TestSendFile_UploadFailureOnlyAffectsApp 验证某推送应用上传失败只把该应用下的明细记为 failed，
// 且失败后不再重复上传。
func TestSendFile_UploadFailureOnlyAffectsApp(t *testing.T) {
	idx := map[string]empcap.LarkUserIDIndex{
		"cli_a": {ByEmployeeNo: map[string]string{"E1": "u_a_1", "E2": "u_a_2"}},
		"cli_b": {ByEmployeeNo: map[string]string{"E1": "u_b_1", "E2": "u_b_2"}},
	}
	fail := map[string]error{"upload:cli_push_b": errors.New("upload rejected")}
	svc, calls := newService(t, twoCompanyConfig("x"), idx, fail)
	res, err := svc.SendFile(context.Background(), larkpushcap.SendFileRequest{
		Group:      testGroup,
		Recipients: []larkpushcap.Recipient{{EmployeeNo: "E1"}, {EmployeeNo: "E2"}},
		FileName:   "a.zip",
		Content:    []byte("PK..."),
	})
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	for _, d := range res.Deliveries {
		want := larkpushcap.DeliveryStatusSent
		if d.PushAppID == "cli_push_b" {
			want = larkpushcap.DeliveryStatusFailed
		}
		if d.Status != want {
			t.Fatalf("明细状态不符: %+v", d)
		}
	}
	if countKind(*calls, "upload-file", "cli_push_b") != 1 {
		t.Fatalf("上传失败后不应重试, got %+v", *calls)
	}
	if countKind(*calls, "file", "cli_push_b") != 0 {
		t.Fatalf("上传失败的应用不应发送消息, got %+v", *calls)
	}
}

// TestSendFile_InvalidPayload 验证文件名为空、内容为空、超过上限都返回参数错误且不发起任何调用。
func TestSendFile_InvalidPayload(t *testing.T) {
	svc, calls := newService(t, singleAppConfig(), nil, nil)
	cases := map[string]larkpushcap.SendFileRequest{
		"文件名为空": {FileName: " ", Content: []byte("x")},
		"内容为空":  {FileName: "a.txt"},
		"超过上限":  {FileName: "a.bin", Content: make([]byte, larkim.MaxFileBytes+1)},
	}
	for name, req := range cases {
		req.Group = testGroup
		req.Recipients = []larkpushcap.Recipient{{ChatID: "oc_1"}}
		if _, err := svc.SendFile(context.Background(), req); !bizerr.Is(err, larkpushcode.CodeInvalidParam) {
			t.Fatalf("%s: want CodeInvalidParam, got %v", name, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("参数错误不应发起调用, got %+v", *calls)
	}
}

// TestNewFilePayload_StripsPath 验证文件名只保留最后一段，避免把调用方本地路径带进消息。
func TestNewFilePayload_StripsPath(t *testing.T) {
	for in, want := range map[string]string{
		"C:\\Users\\a\\报表.xlsx": "报表.xlsx",
		"/tmp/x/report.pdf":     "report.pdf",
		"plain.txt":             "plain.txt",
	} {
		p, err := newFilePayload(in, []byte("x"))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if p.name != want {
			t.Fatalf("%q: want %q, got %q", in, want, p.name)
		}
	}
}
