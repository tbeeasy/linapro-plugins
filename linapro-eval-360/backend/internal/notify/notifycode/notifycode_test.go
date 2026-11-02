// notifycode_test.go 覆盖失败类错误码的 count / list 参数构造（单份发布据此回报失败评价者）。
package notifycode

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/bizerr"
)

// TestFailedParams_Truncates: 失败清单超过上限时只列前 maxFailedListed 人并加省略号，count 仍为总数。
func TestFailedParams_Truncates(t *testing.T) {
	labels := make([]string, 0, maxFailedListed+5)
	for i := 0; i < maxFailedListed+5; i++ {
		labels = append(labels, "E"+strconv.Itoa(i))
	}
	err := bizerr.NewCode(CodePushFailed, FailedParams(labels)...)
	e, ok := bizerr.As(err)
	if !ok {
		t.Fatalf("不是业务错误: %v", err)
	}
	if count := gvar.New(e.Params()["count"]).Int(); count != maxFailedListed+5 {
		t.Fatalf("count 应为总人数 %d, got %d", maxFailedListed+5, count)
	}
	wantList := strings.Join(labels[:maxFailedListed], ", ") + " ..."
	if list := gvar.New(e.Params()["list"]).String(); list != wantList {
		t.Fatalf("list want %q, got %q", wantList, list)
	}
}

// TestFailedParams_NoTruncation: 未超上限时完整列出、不加省略号。
func TestFailedParams_NoTruncation(t *testing.T) {
	err := bizerr.NewCode(CodeEvaluatorsUnreachable, FailedParams([]string{"E001", "7"})...)
	e, _ := bizerr.As(err)
	if count := gvar.New(e.Params()["count"]).Int(); count != 2 {
		t.Fatalf("count want 2, got %d", count)
	}
	if list := gvar.New(e.Params()["list"]).String(); list != "E001, 7" {
		t.Fatalf("list want %q, got %q", "E001, 7", list)
	}
}
