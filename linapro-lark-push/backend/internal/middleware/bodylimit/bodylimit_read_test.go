// bodylimit_read_test.go 覆盖按路由请求体上限中间件：规则匹配与上限展示文本。
// 超限 413、表单解析与请求体可重复读取的端到端行为由 controller/push 的 HTTP 用例覆盖。

package bodylimit

import "testing"

// TestMatch 验证按路径后缀取第一条匹配规则，未登记的路由不设限，上限非正的规则被忽略。
func TestMatch(t *testing.T) {
	rules := []Rule{
		{PathSuffix: "/message/text", Limit: 256 << 10},
		{PathSuffix: "/message/file", Limit: 31 << 20},
		{PathSuffix: "/status", Limit: 0},
	}
	if rule, ok := match(rules, "/x/linapro-lark-push/open/v1/message/text"); !ok || rule.Limit != 256<<10 {
		t.Fatalf("文本路由应命中 256 KB 规则, got %+v %v", rule, ok)
	}
	if rule, ok := match(rules, "/x/linapro-lark-push/open/v1/message/file"); !ok || rule.Limit != 31<<20 {
		t.Fatalf("文件路由应命中 31 MB 规则, got %+v %v", rule, ok)
	}
	if _, ok := match(rules, "/x/linapro-lark-push/open/v1/status"); ok {
		t.Fatal("上限非正的规则应被忽略")
	}
	if _, ok := match(rules, "/x/linapro-lark-push/open/v1/other"); ok {
		t.Fatal("未登记的路由不应命中")
	}
}

// TestSizeLabel 验证上限展示文本：整 MB 显示 MB，否则显示 KB。
func TestSizeLabel(t *testing.T) {
	if got := sizeLabel(256 << 10); got != "256 KB" {
		t.Fatalf("want 256 KB, got %s", got)
	}
	if got := sizeLabel(31 << 20); got != "31 MB" {
		t.Fatalf("want 31 MB, got %s", got)
	}
}
