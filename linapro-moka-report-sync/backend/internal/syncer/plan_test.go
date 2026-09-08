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

func TestPlan_CreateWhenAbsent(t *testing.T) {
	in := PlanInput{
		UniqueField: "姓名",
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
		UniqueField: "姓名",
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
		UniqueField: "姓名",
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
		UniqueField: "姓名",
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
		UniqueField: "姓名",
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
		UniqueField: "姓名",
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
