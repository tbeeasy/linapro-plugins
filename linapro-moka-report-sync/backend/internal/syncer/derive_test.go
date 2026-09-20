package syncer

import (
	"testing"
)

// parseDateForTest 是测试用的简单日期解析器，复用 parseDateFallback。
func parseDateForTest(s string) (int64, bool) {
	t := parseDateFallback(s)
	if t.IsZero() {
		return 0, false
	}
	return t.UnixMilli(), true
}

func dateParams(targets map[string]string) map[string]any {
	return map[string]any{
		"parse":   func(s string) (int64, bool) { return parseDateForTest(s) },
		"targets": targets,
	}
}

// TestDeriveDate_DashSeparator 验证 "2026-09" 格式提取年月。
func TestDeriveDate_DashSeparator(t *testing.T) {
	out := deriveDate([]string{"2026-09"}, dateParams(map[string]string{"年": "2006", "月": "01"}))
	if out["年"] != "2026" {
		t.Errorf("年 应为 2026, got %q", out["年"])
	}
	if out["月"] != "09" {
		t.Errorf("月 应为 09, got %q", out["月"])
	}
}

// TestDeriveDate_SlashSeparator 验证 "2026/09" 格式提取年月。
func TestDeriveDate_SlashSeparator(t *testing.T) {
	out := deriveDate([]string{"2026/09"}, dateParams(map[string]string{"年": "2006", "月": "01"}))
	if out["年"] != "2026" || out["月"] != "09" {
		t.Errorf("slash 分隔符: 年=%q 月=%q", out["年"], out["月"])
	}
}

// TestDeriveDate_ChineseSeparator 验证 "2026年09月" 格式提取年月。
func TestDeriveDate_ChineseSeparator(t *testing.T) {
	out := deriveDate([]string{"2026年09月"}, dateParams(map[string]string{"年": "2006", "月": "01"}))
	if out["年"] != "2026" || out["月"] != "09" {
		t.Errorf("中文分隔符: 年=%q 月=%q", out["年"], out["月"])
	}
}

// TestDeriveDate_ParseFailLeaveEmpty 验证非日期源值时所有目标列留空。
func TestDeriveDate_ParseFailLeaveEmpty(t *testing.T) {
	out := deriveDate([]string{"非日期字符串"}, dateParams(map[string]string{"年": "2006", "月": "01"}))
	if out != nil {
		t.Errorf("解析失败应返回 nil, got %v", out)
	}
}

// TestDeriveDate_EmptySourceLeaveEmpty 验证源值为空时返回 nil。
func TestDeriveDate_EmptySourceLeaveEmpty(t *testing.T) {
	out := deriveDate([]string{""}, dateParams(map[string]string{"年": "2006"}))
	if out != nil {
		t.Errorf("空源值应返回 nil, got %v", out)
	}
}

// TestDeriveSplit_Slash 验证按 "/" 分隔符拆分。
func TestDeriveSplit_Slash(t *testing.T) {
	out := deriveSplit([]string{"研发/后端"}, map[string]any{
		"by":    "/",
		"order": []string{"一级", "二级"},
	})
	if out["一级"] != "研发" || out["二级"] != "后端" {
		t.Errorf("split /: 一级=%q 二级=%q", out["一级"], out["二级"])
	}
}

// TestDeriveSplit_InsufficientParts 验证分割结果不足时剩余目标列留空。
func TestDeriveSplit_InsufficientParts(t *testing.T) {
	out := deriveSplit([]string{"研发"}, map[string]any{
		"by":    "/",
		"order": []string{"一级", "二级"},
	})
	if out["一级"] != "研发" {
		t.Errorf("一级应为 研发, got %q", out["一级"])
	}
	if out["二级"] != "" {
		t.Errorf("二级不足时应为空串, got %q", out["二级"])
	}
}

// TestDeriveSplit_EmptySource 验证源值为空时返回 nil。
func TestDeriveSplit_EmptySource(t *testing.T) {
	out := deriveSplit([]string{""}, map[string]any{"by": "/", "order": []string{"一级"}})
	if out != nil {
		t.Errorf("空源值应返回 nil, got %v", out)
	}
}

// TestDeriveRegex_TwoGroups 验证正则两捕获组映射到两目标列。
func TestDeriveRegex_TwoGroups(t *testing.T) {
	out := deriveRegex([]string{"202609"}, map[string]any{
		"pattern": `(\d{4})(\d{2})`,
		"targets": map[string]string{"年": "1", "月": "2"},
	})
	if out["年"] != "2026" || out["月"] != "09" {
		t.Errorf("regex 捕获组: 年=%q 月=%q", out["年"], out["月"])
	}
}

// TestDeriveRegex_NoMatch 验证不匹配时返回 nil。
func TestDeriveRegex_NoMatch(t *testing.T) {
	out := deriveRegex([]string{"abc"}, map[string]any{
		"pattern": `(\d{4})(\d{2})`,
		"targets": map[string]string{"年": "1", "月": "2"},
	})
	if out != nil {
		t.Errorf("不匹配应返回 nil, got %v", out)
	}
}

// TestApplyDerivedColumns_DateRule 验证 ApplyDerivedColumns 对行应用 date 规则并追加目标列。
func TestApplyDerivedColumns_DateRule(t *testing.T) {
	rows := []Row{
		{"入职年月": "2026-09", "工号": "A001"},
	}
	cols := []string{"入职年月", "工号"}
	rule := DeriveRule{
		Kind:    "date",
		Sources: []string{"入职年月"},
		Targets: map[string]string{"年": "2006", "月": "01"},
		Params: map[string]any{
			"parse":   func(s string) (int64, bool) { return parseDateForTest(s) },
			"targets": map[string]string{"年": "2006", "月": "01"},
		},
	}
	outCols := ApplyDerivedColumns(rows, cols, []DeriveRule{rule})

	// 目标列应追加到列集合。
	colSet := make(map[string]struct{})
	for _, c := range outCols {
		colSet[c] = struct{}{}
	}
	if _, ok := colSet["年"]; !ok {
		t.Error("年 应追加到列集合")
	}
	if _, ok := colSet["月"]; !ok {
		t.Error("月 应追加到列集合")
	}
	// 行数据应包含派生值。
	if rows[0]["年"] != "2026" {
		t.Errorf("行 年 应为 2026, got %q", rows[0]["年"])
	}
	if rows[0]["月"] != "09" {
		t.Errorf("行 月 应为 09, got %q", rows[0]["月"])
	}
}

// TestApplyDerivedColumns_ParseFailEmpty 验证源值无法解析时目标列留空（不写脏值）。
func TestApplyDerivedColumns_ParseFailEmpty(t *testing.T) {
	rows := []Row{{"入职年月": "非日期", "工号": "A001"}}
	cols := []string{"入职年月", "工号"}
	rule := DeriveRule{
		Kind:    "date",
		Sources: []string{"入职年月"},
		Targets: map[string]string{"年": "2006"},
		Params: map[string]any{
			"parse":   func(s string) (int64, bool) { return parseDateForTest(s) },
			"targets": map[string]string{"年": "2006"},
		},
	}
	ApplyDerivedColumns(rows, cols, []DeriveRule{rule})
	// 解析失败时行中不应存在 年 键，或值为空。
	if v, ok := rows[0]["年"]; ok && v != "" {
		t.Errorf("解析失败时 年 应为空, got %q", v)
	}
}

// TestApplyDerivedColumns_NoRules 验证无规则时 cols 原样返回。
func TestApplyDerivedColumns_NoRules(t *testing.T) {
	rows := []Row{{"工号": "A001"}}
	cols := []string{"工号"}
	out := ApplyDerivedColumns(rows, cols, nil)
	if len(out) != 1 || out[0] != "工号" {
		t.Errorf("无规则时 cols 应原样返回, got %v", out)
	}
}
