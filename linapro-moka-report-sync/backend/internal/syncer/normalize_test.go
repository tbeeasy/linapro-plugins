package syncer

import (
	"strconv"
	"testing"
)

// TestNormalizeColumns_FullDateString 验证「纯日期字符串被归一为毫秒时间戳字符串」，模拟
// Moka 侧（"2025-06-01"）与 Bitable 回读侧（写侧写成的毫秒数字串）在规划比较时对齐。
func TestNormalizeColumns_FullDateString(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": "2025-06-01"},
	}
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	if got := rows[0]["转正日期"]; got != "1750000000000" {
		t.Fatalf("转正日期: got %q, want 毫秒字符串 1750000000000", got)
	}
	// 姓名（非指定列）必须保持原样。
	if got := rows[0]["姓名"]; got != "张伟" {
		t.Fatalf("姓名 不应被改动，got %q", got)
	}
}

// TestNormalizeColumns_EmptyAndInvalidPreserved 验证空值、无法解析的值保留原样，
// 不会被误改成空或误判为日期。
func TestNormalizeColumns_EmptyAndInvalidPreserved(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": ""},
		{"姓名": "李四", "转正日期": "待定"},
	}
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	if got := rows[0]["转正日期"]; got != "" {
		t.Fatalf("空值应保留原样，got %q", got)
	}
	if got := rows[1]["转正日期"]; got != "待定" {
		t.Fatalf("无法解析的值应保留原样，got %q", got)
	}
}

// TestNormalizeColumns_Idempotent 验证幂等：毫秒数字串再经本函数仍返回同一毫秒字符串，
// 不会因重复调用在 Moka 侧与回读侧之间重新引入差异。
func TestNormalizeColumns_Idempotent(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": "1750000000000"},
	}
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	first := rows[0]["转正日期"]
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	if got := rows[0]["转正日期"]; got != first || got != "1750000000000" {
		t.Fatalf("幂等性失败：first=%q after=%q", first, got)
	}
}

// TestNormalizeColumns_OnlySpecifiedCols 验证仅传入的列被归一，未传入的列即便内容形似时间也不改动。
func TestNormalizeColumns_OnlySpecifiedCols(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": "2025-06-01", "入职日期": "2025-01-01"},
	}
	// 只归一「转正日期」，转正日期成为毫秒串；「入职日期」不在 cols 里不处理。
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	if got := rows[0]["转正日期"]; got != "1750000000000" {
		t.Fatalf("转正日期: got %q, want 毫秒字符串", got)
	}
	if got := rows[0]["入职日期"]; got != "2025-01-01" {
		t.Fatalf("未被指定为归一列不应改动，got %q", got)
	}
}

// fakeParseMillis 是测试替身：解析 "2025-06-01" 为固定毫秒（模拟 ParseEpochMillisCST 对
// 纯日期的东八区零点语义）；其余字符串原样转数字，交给 strconv 决定是否可解析。
func fakeParseMillis(v string) (int64, bool) {
	switch v {
	case "2025-06-01":
		return 1750000000000, true
	case "2025-10-06":
		return 1759680000000, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
