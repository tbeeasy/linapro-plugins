package syncer

import (
	"testing"
)

func fieldSet(cols ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		m[c] = struct{}{}
	}
	return m
}

// key1 构造单列 KeySpec，等价于原 uniqueField。
func key1(field string) KeySpec {
	return KeySpec{Fields: []string{field}, Sep: ""}
}

func TestPlan_CreateWhenAbsent(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if len(p.Creates) != 1 || len(p.Updates) != 0 || p.Frozen != 0 {
		t.Fatalf("expected 1 create, got %+v", p)
	}
	if p.Creates[0].Name != "张伟" || p.Creates[0].Fields["性别"] != "男性" {
		t.Fatalf("unexpected create op: %+v", p.Creates[0])
	}
}

func TestPlan_RewriteWhenIntersectionColumnEmpty(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "民族": ""}},
		},
	}
	p := Plan(in)
	if len(p.Updates) != 1 || len(p.Creates) != 0 || p.Frozen != 0 {
		t.Fatalf("expected 1 update, got %+v", p)
	}
	u := p.Updates[0]
	if u.RecordID != "rec1" || u.Fields["民族"] != "汉族" {
		t.Fatalf("unexpected update op: %+v", u)
	}
}

func TestPlan_FreezeWhenAllIntersectionColumnsFilled(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "民族": "汉族"}},
		},
	}
	p := Plan(in)
	if p.Frozen != 1 || len(p.Updates) != 0 || len(p.Creates) != 0 {
		t.Fatalf("expected 1 frozen, got %+v", p)
	}
}

func TestPlan_IgnoresTableOnlyColumns(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别"},
		TableFields: fieldSet("姓名", "性别", "备注"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "备注": ""}},
		},
	}
	p := Plan(in)
	if p.Frozen != 1 || len(p.Updates) != 0 {
		t.Fatalf("manual empty column must not force rewrite; got %+v", p)
	}
	for _, c := range p.Intersection {
		if c == "备注" {
			t.Fatal("备注 must not be in the intersection")
		}
	}
}

func TestPlan_SkipsRowsWithoutName(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别"},
		TableFields: fieldSet("姓名", "性别"),
		Rows: []Row{
			{"姓名": "", "性别": "男性"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if p.SkippedNoName != 1 || len(p.Creates) != 0 {
		t.Fatalf("expected 1 skipped-no-name, got %+v", p)
	}
}

func TestPlan_OnlyWritesIntersectionColumns(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "内部编码"},
		TableFields: fieldSet("姓名", "性别"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "内部编码": "X1"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if len(p.Creates) != 1 {
		t.Fatalf("expected 1 create, got %+v", p)
	}
	if _, ok := p.Creates[0].Fields["内部编码"]; ok {
		t.Fatal("create must not carry a column absent from the table")
	}
}

func TestFlattenReport_MapsTitlesToValues(t *testing.T) {
	data := &ReportData{
		Headers: []ReportHeader{
			{DataIndex: "c_1", Title: "姓名", Type: "HEADER"},
			{DataIndex: "c_2", Title: "性别", Type: "HEADER"},
		},
		Rows: []map[string]any{
			{"c_1": "张伟", "c_2": "男性"},
		},
	}
	cols, rows := FlattenReport(data)
	if len(cols) != 2 || cols[0] != "姓名" || cols[1] != "性别" {
		t.Fatalf("unexpected cols: %v", cols)
	}
	if len(rows) != 1 || rows[0]["姓名"] != "张伟" || rows[0]["性别"] != "男性" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestStringify_NumberStaysInteger(t *testing.T) {
	if got := stringify(float64(83475647)); got != "83475647" {
		t.Fatalf("expected integer formatting, got %q", got)
	}
	if got := stringify(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}
}

// TestStringify_RichTextSegments 验证 stringify 对 Bitable 读取方抽取出的富文本分段切片
// （[]string）逐段归一后拼接：每段 trim、"-" 归零。
func TestStringify_RichTextSegments(t *testing.T) {
	if got := stringify([]string{"第一段", "-", "第二段"}); got != "第一段第二段" {
		t.Fatalf("expected per-segment normalize then join, got %q", got)
	}
}

// TestPlan_DateNormalizedFreeze 复现「日期列反复更新」缺陷并锁定修复：
// Bitable 回读值为写侧编码的毫秒字符串（如 "1759680000000"），而 Moka 侧是原始日期文本
// （如 "2025-10-06"）。若 Moka 侧保留文本，Plan 会把它与毫秒字符串比较判为差异而每轮重写；
// 修复后 Moka 侧经 NormalizeColumns 归一成同一毫秒字符串，Plan 应冻结该记录。
func TestPlan_DateNormalizedFreeze(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": "2025-10-06"},
	}
	// Bitable 回读：写侧把日期写成毫秒（东八区零点），读回收 Stringify 的毫秒数字串。
	existing := map[string]ExistingRecord{
		"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "转正日期": "1759680000000"}},
	}
	// 归一前：原始日期文本与毫秒字符串不等，被误判为更新。
	raw := Plan(PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "转正日期"},
		TableFields: fieldSet("姓名", "转正日期"),
		Rows:        rows,
		Existing:    existing,
	})
	if len(raw.Updates) != 1 {
		t.Fatalf("归一前应被误判为 1 条更新，got updates=%d", len(raw.Updates))
	}
	// 归一后：dates 列解析为同一毫秒字符串，diff 为空，冻结。
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	frozen := Plan(PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "转正日期"},
		TableFields: fieldSet("姓名", "转正日期"),
		Rows:        rows,
		Existing:    existing,
	})
	if frozen.Frozen != 1 || len(frozen.Updates) != 0 {
		t.Fatalf("归一后应冻结 1 条且无更新，got frozen=%d updates=%d", frozen.Frozen, len(frozen.Updates))
	}
}

// TestPlan_CompositeKeySkipsKeyFieldsInDiff 验证复合键组成列在 diffFields 中被跳过。
func TestPlan_CompositeKeySkipsKeyFieldsInDiff(t *testing.T) {
	spec := KeySpec{Fields: []string{"工号", "考勤月"}, Sep: "-"}
	rows := []Row{
		{"工号": "A001", "考勤月": "2026-09", "出勤天数": "22"},
	}
	existing := map[string]ExistingRecord{
		"A001-2026-09": {
			RecordID: "rec1",
			// 键列值相同，出勤天数为空（触发重写）
			Fields: Row{"工号": "A001", "考勤月": "2026-09", "出勤天数": ""},
		},
	}
	p := Plan(PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "考勤月", "出勤天数"},
		TableFields: fieldSet("工号", "考勤月", "出勤天数"),
		Rows:        rows,
		Existing:    existing,
	})
	if len(p.Updates) != 1 {
		t.Fatalf("出勤天数为空应触发 1 条更新，got updates=%d", len(p.Updates))
	}
	diff := p.Updates[0].Fields
	// 键组成列不应出现在 diff 中。
	if _, ok := diff["工号"]; ok {
		t.Error("键列 工号 不应出现在 diff 中")
	}
	if _, ok := diff["考勤月"]; ok {
		t.Error("键列 考勤月 不应出现在 diff 中")
	}
	if diff["出勤天数"] != "22" {
		t.Errorf("出勤天数应在 diff 中为 22，got %q", diff["出勤天数"])
	}
}

// TestPlan_CompositeKeyEmptyFieldSkipped 验证复合键任一列为空时行被跳过。
func TestPlan_CompositeKeyEmptyFieldSkipped(t *testing.T) {
	spec := KeySpec{Fields: []string{"工号", "考勤月"}, Sep: "-"}
	rows := []Row{
		{"工号": "A001", "考勤月": "", "出勤天数": "22"}, // 考勤月空 → 键为空 → 跳过
	}
	p := Plan(PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "考勤月", "出勤天数"},
		TableFields: fieldSet("工号", "考勤月", "出勤天数"),
		Rows:        rows,
		Existing:    map[string]ExistingRecord{},
	})
	if p.SkippedNoName != 1 || len(p.Creates) != 0 {
		t.Fatalf("复合键含空列应跳过，got skipped=%d creates=%d", p.SkippedNoName, len(p.Creates))
	}
}
