// bindlog_test.go 覆盖绑定失败日志中间件的判定与请求体摘要。
// 中间件在生产路由上的端到端行为由 controller/push 的 HTTP 用例覆盖（那里挂同一套装配）。

package bindlog

import (
	"strings"
	"testing"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
)

// TestShouldLog 验证只接管非 bizerr 的框架层错误：已有错误码与自有日志的失败不能重复记录。
func TestShouldLog(t *testing.T) {
	if shouldLog(nil) {
		t.Fatal("成功请求不应记录绑定失败日志")
	}
	if shouldLog(bizerr.NewCode(larkpushcode.CodeInvalidParam, bizerr.P("item", "x"))) {
		t.Fatal("bizerr 走各自的日志与响应码，不应重复记录")
	}
	if shouldLog(gerror.New("reflect.Value.Convert: value of type string cannot be converted")) {
		return
	}
	t.Fatal("框架层错误应记录")
}

// TestSummarizeRaw 验证普通请求体：短体原样转义，长体按上限截断并标注原始长度。
func TestSummarizeRaw(t *testing.T) {
	if got := summarizeRaw(nil); got != "" {
		t.Fatalf("空请求体应为空串, got %q", got)
	}
	if got := summarizeRaw([]byte(`{"text":"hi"}`)); got != `"{\"text\":\"hi\"}"` {
		t.Fatalf("短请求体应原样转义, got %s", got)
	}

	long := strings.Repeat("a", maxBodyLogBytes+10)
	got := summarizeRaw([]byte(long))
	if !strings.HasPrefix(got, `"`+strings.Repeat("a", maxBodyLogBytes)) {
		t.Fatalf("应按上限保留前缀, got 长度 %d", len(got))
	}
	if !strings.Contains(got, "已截断") || !strings.Contains(got, "原始长度") {
		t.Fatalf("截断时应标注原始长度, got %s", got)
	}
}

// TestSummarizeForm 验证 multipart 摘要：字段按名排序、长值截断、文件只记名与大小不记内容。
func TestSummarizeForm(t *testing.T) {
	got := summarizeForm(
		map[string][]string{
			"recipients": {`[{"employee_no":"E1"}]`},
			"biz_ref":    {"REPORT-202610"},
			"remark":     {strings.Repeat("b", maxFormValueLogBytes+5)},
		},
		map[string][]string{"file": {"月报.pdf(1024 字节)"}},
	)

	if !strings.HasPrefix(got, `biz_ref="REPORT-202610"`) {
		t.Fatalf("字段应按名排序, got %s", got)
	}
	if !strings.Contains(got, `recipients="[{\"employee_no\":\"E1\"}]"`) {
		t.Fatalf("字段值应原样转义, got %s", got)
	}
	if !strings.Contains(got, "remark=") || !strings.Contains(got, "已截断") {
		t.Fatalf("超长字段值应截断并标注, got %s", got)
	}
	if !strings.Contains(got, "file=[文件 月报.pdf(1024 字节)]") {
		t.Fatalf("文件只应记文件名与大小, got %s", got)
	}
}

// TestSummarizeForm_Empty 验证没有字段也没有文件时返回空串，不产生只有分隔符的噪声。
func TestSummarizeForm_Empty(t *testing.T) {
	if got := summarizeForm(nil, nil); got != "" {
		t.Fatalf("空表单应为空串, got %q", got)
	}
}
