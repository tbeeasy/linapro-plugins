package larkim

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildActionCard_WithButton 验证带按钮卡片的 JSON 结构合法、字段正确。
func TestBuildActionCard_WithButton(t *testing.T) {
	got, err := BuildActionCard("【360评估】", "请尽快完成本周期的 360 评估", "立即填写", "https://emp.example.com/todo")
	if err != nil {
		t.Fatalf("BuildActionCard: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("卡片 JSON 非法: %v\n%s", err, got)
	}
	if raw["schema"] != "2.0" {
		t.Fatalf("schema want 2.0, got %v", raw["schema"])
	}
	header, _ := raw["header"].(map[string]any)
	if header == nil {
		t.Fatal("header 缺失")
	}
	title, _ := header["title"].(map[string]any)
	if title["content"] != "【360评估】" {
		t.Fatalf("header.title.content want 【360评估】, got %v", title["content"])
	}

	if !strings.Contains(got, "立即填写") {
		t.Fatalf("卡片应含按钮文案 \"立即填写\", got %s", got)
	}
	if !strings.Contains(got, "https://emp.example.com/todo") {
		t.Fatalf("卡片应含按钮 URL, got %s", got)
	}
}

// TestBuildActionCard_NoButton 验证按钮文案或 URL 为空时不生成 action 元素。
func TestBuildActionCard_NoButton(t *testing.T) {
	got, err := BuildActionCard("标题", "正文", "", "")
	if err != nil {
		t.Fatalf("BuildActionCard: %v", err)
	}
	if strings.Contains(got, "\"button\"") {
		t.Fatalf("无按钮时不应当出现 button 元素, got %s", got)
	}
}

// TestBuildActionCard_Empty 验证标题与正文同时为空时拒绝构造。
func TestBuildActionCard_Empty(t *testing.T) {
	if _, err := BuildActionCard("", "", "", ""); err == nil {
		t.Fatal("标题与正文同时为空时应返回错误")
	}
}

// TestSplitLines 验证换行拆分与尾空串清理。
func TestSplitLines(t *testing.T) {
	got := splitLines("a\nb\n\nc\n")
	want := []string{"a", "b", "", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitLines len: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitLines[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
