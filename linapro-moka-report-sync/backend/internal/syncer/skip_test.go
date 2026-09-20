package syncer

import (
	"maps"
	"testing"
	"time"
)

// beforeRule 构造一条 before 规则，便于用例书写。
func beforeRule(source, format, value string) SkipRule {
	return SkipRule{Kind: "before", Source: source, Format: format, Value: value}
}

// TestApplySkipRules_BeforeThresholdDropped 早于阈值的行被跳过。
func TestApplySkipRules_BeforeThresholdDropped(t *testing.T) {
	rows := []Row{{"申请时间": "2025-12"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "2026-01")})
	if dropped != 1 || len(kept) != 0 {
		t.Fatalf("早于阈值应丢弃，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 0 {
		t.Errorf("正常判定不应产生告警，got %v", warns)
	}
}

// TestApplySkipRules_EqualOrAfterKept 等于/晚于阈值保留。
func TestApplySkipRules_EqualOrAfterKept(t *testing.T) {
	rows := []Row{{"申请时间": "2026-01"}, {"申请时间": "2026-03"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "2026-01")})
	if dropped != 0 || len(kept) != 2 {
		t.Fatalf("等于/晚于阈值应全部保留，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 0 {
		t.Errorf("正常判定不应产生告警，got %v", warns)
	}
}

// TestApplySkipRules_SourceParseFailKept 源值解析失败保留该行并记 warn。
func TestApplySkipRules_SourceParseFailKept(t *testing.T) {
	rows := []Row{{"申请时间": "不是日期"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "2026-01")})
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("源值解析失败应保留，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 1 || warns[0].Rows != 1 {
		t.Errorf("应对无法判定的行记 1 条 warn，got %v", warns)
	}
}

// TestApplySkipRules_SourceEmptyKept 源值为空保留该行。
func TestApplySkipRules_SourceEmptyKept(t *testing.T) {
	rows := []Row{{"申请时间": ""}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "2026-01")})
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("源值为空应保留，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 1 {
		t.Errorf("源值为空应记 warn，got %v", warns)
	}
}

// TestApplySkipRules_ThresholdEmptyKeepsAll 阈值为空保留所有行。
func TestApplySkipRules_ThresholdEmptyKeepsAll(t *testing.T) {
	rows := []Row{{"申请时间": "2020-01"}, {"申请时间": "2026-01"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "")})
	if dropped != 0 || len(kept) != 2 {
		t.Fatalf("阈值为空应全部保留，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 1 || warns[0].Rows != 2 {
		t.Errorf("阈值为空应对全部行记 warn，got %v", warns)
	}
}

// TestApplySkipRules_ThresholdInvalidKeepsAll 阈值格式非法时不丢弃任何行并记 warn。
func TestApplySkipRules_ThresholdInvalidKeepsAll(t *testing.T) {
	rows := []Row{{"申请时间": "2020-01"}, {"申请时间": "2026-01"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{beforeRule("申请时间", "Y-m", "非法阈值")})
	if dropped != 0 || len(kept) != 2 {
		t.Fatalf("阈值非法应全部保留，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 1 || warns[0].Rows != 2 {
		t.Errorf("阈值非法应对全部行记 warn，got %v", warns)
	}
}

// TestApplySkipRules_MultipleRulesUnion 多条规则取并集：任一命中即丢弃。
func TestApplySkipRules_MultipleRulesUnion(t *testing.T) {
	rows := []Row{
		{"申请时间": "2025-12", "招聘月份": "2026-05"}, // 被规则1丢弃
		{"申请时间": "2026-05", "招聘月份": "2025-01"}, // 被规则2丢弃
		{"申请时间": "2026-05", "招聘月份": "2026-05"}, // 两条都不命中，保留
	}
	rules := []SkipRule{
		beforeRule("申请时间", "Y-m", "2026-01"),
		beforeRule("招聘月份", "Y-m", "2026-01"),
	}
	kept, dropped, warns := ApplySkipRules(rows, rules)
	if dropped != 2 || len(kept) != 1 {
		t.Fatalf("并集应丢弃 2 行保留 1 行，got dropped=%d kept=%d", dropped, len(kept))
	}
	if kept[0]["申请时间"] != "2026-05" || kept[0]["招聘月份"] != "2026-05" {
		t.Errorf("保留的行不对，got %v", kept[0])
	}
	if len(warns) != 0 {
		t.Errorf("正常判定不应产生告警，got %v", warns)
	}
}

// TestApplySkipRules_DerivedColumnAsSource 派生列作为 source 参与过滤。
func TestApplySkipRules_DerivedColumnAsSource(t *testing.T) {
	// 模拟 ApplyDerivedColumns 已从 申请时间 派生出 招聘月份。
	rows := []Row{
		{"申请时间": "2025-12-15", "招聘月份": "2025-12"},
		{"申请时间": "2026-02-15", "招聘月份": "2026-02"},
	}
	kept, dropped, _ := ApplySkipRules(rows, []SkipRule{beforeRule("招聘月份", "Y-m", "2026-01")})
	if dropped != 1 || len(kept) != 1 || kept[0]["招聘月份"] != "2026-02" {
		t.Fatalf("应按派生列 招聘月份 过滤，got dropped=%d kept=%v", dropped, kept)
	}
}

// TestApplySkipRules_NoRules 无规则时原样返回。
func TestApplySkipRules_NoRules(t *testing.T) {
	rows := []Row{{"申请时间": "2020-01"}}
	kept, dropped, warns := ApplySkipRules(rows, nil)
	if dropped != 0 || len(kept) != 1 || len(warns) != 0 {
		t.Fatalf("无规则应原样返回，got dropped=%d kept=%d warns=%d", dropped, len(kept), len(warns))
	}
}

// TestApplySkipRules_UnknownKind 未知 kind 不丢行并记 warn。
func TestApplySkipRules_UnknownKind(t *testing.T) {
	rows := []Row{{"申请时间": "2020-01"}}
	kept, dropped, warns := ApplySkipRules(rows, []SkipRule{{Kind: "after", Source: "申请时间", Format: "Y-m", Value: "2026-01"}})
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("未知 kind 不应丢行，got dropped=%d kept=%d", dropped, len(kept))
	}
	if len(warns) != 1 || warns[0].Rows != 1 {
		t.Errorf("未知 kind 应记 warn，got %v", warns)
	}
}

// TestParseByFormat_Dialect 验证 gtime 方言（Y-m）生效。
func TestParseByFormat_Dialect(t *testing.T) {
	ms, ok := parseByFormat("2026-09", "Y-m")
	if !ok {
		t.Fatalf("Y-m 方言应解析成功")
	}
	// 东八区 2026-09-01 00:00:00 的毫秒时间戳。
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)).UnixMilli()
	if ms != want {
		t.Errorf("解析结果应为东八区 2026-09-01 零点，got %d want %d", ms, want)
	}
}

// TestParseByFormat_TimezoneAnchor 跨时区锚定一致：把 time.Local 换成非东八区，
// 同一输入应产出相同的时间戳（不依赖进程本地时区）。
func TestParseByFormat_TimezoneAnchor(t *testing.T) {
	orig := time.Local
	defer func() { time.Local = orig }()

	time.Local = time.UTC
	msUTC, okUTC := parseByFormat("2026-01", "Y-m")

	time.Local = time.FixedZone("UTC-8", -8*3600)
	msWest, okWest := parseByFormat("2026-01", "Y-m")

	if !okUTC || !okWest {
		t.Fatalf("两种时区下都应解析成功")
	}
	if msUTC != msWest {
		t.Errorf("不同 time.Local 下应产出相同时间戳，got %d vs %d", msUTC, msWest)
	}
}

// TestApplySkipRules_TimezoneConsistentVerdict 同一输入在非东八区 time.Local 下产出相同判定。
func TestApplySkipRules_TimezoneConsistentVerdict(t *testing.T) {
	orig := time.Local
	defer func() { time.Local = orig }()

	rows := []Row{{"申请时间": "2025-12"}, {"申请时间": "2026-02"}}
	rule := []SkipRule{beforeRule("申请时间", "Y-m", "2026-01")}

	time.Local = time.UTC
	keptA, droppedA, _ := ApplySkipRules(cloneRows(rows), rule)

	time.Local = time.FixedZone("UTC-8", -8*3600)
	keptB, droppedB, _ := ApplySkipRules(cloneRows(rows), rule)

	if droppedA != droppedB || len(keptA) != len(keptB) {
		t.Errorf("跨时区判定应一致，got A(dropped=%d kept=%d) B(dropped=%d kept=%d)",
			droppedA, len(keptA), droppedB, len(keptB))
	}
}

// cloneRows 深拷贝行切片，避免用例间共享底层 map。
func cloneRows(src []Row) []Row {
	out := make([]Row, len(src))
	for i, r := range src {
		nr := make(Row, len(r))
		maps.Copy(nr, r)
		out[i] = nr
	}
	return out
}
