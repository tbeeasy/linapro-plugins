package syncer

import (
	"reflect"
	"testing"
)

func TestCoalesceRows_FullPlusDashPlaceholders(t *testing.T) {
	// 报表中的“一满一空”场景：同一 uniqueField 值出现两次，一行完整，
	// 另一行字段全是 "-"（已归一化为 ""）。
	rows := []Row{
		{"姓名": "张三", "部门": "研发", "工号": "1001", "手机": "13812345678"},
		{"姓名": "张三", "部门": "研发", "工号": "", "手机": ""},
	}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 merged row, got %d: %+v", len(out), out)
	}
	want := Row{"姓名": "张三", "部门": "研发", "工号": "1001", "手机": "13812345678"}
	if !reflect.DeepEqual(out[0], want) {
		t.Fatalf("merged row = %v, want %v", out[0], want)
	}
}

func TestCoalesceRows_ComplementaryFill(t *testing.T) {
	// “各填一半”场景：每行填充对方留空的字段；
	// 无论行顺序如何，结果必须包含所有非空值。
	rows := []Row{
		{"姓名": "李四", "工号": "1002", "手机": "", "邮箱": ""},
		{"姓名": "李四", "工号": "", "手机": "13912345678", "邮箱": "li@example.com"},
	}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 merged row, got %d: %+v", len(out), out)
	}
	want := Row{"姓名": "李四", "工号": "1002", "手机": "13912345678", "邮箱": "li@example.com"}
	if !reflect.DeepEqual(out[0], want) {
		t.Fatalf("merged row = %v, want %v", out[0], want)
	}
}

func TestCoalesceRows_TrueConflictKeepsBaseAndReports(t *testing.T) {
	// 真冲突：同一字段两行都非空但值不同。
	// 先出现的值胜出，分歧被上报。
	rows := []Row{
		{"姓名": "王五", "部门": "研发", "手机": "13811112222"},
		{"姓名": "王五", "部门": "销售", "手机": "13811112222"},
	}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(out) != 1 {
		t.Fatalf("expected 1 merged row, got %d: %+v", len(out), out)
	}
	if got := out[0]["部门"]; got != "研发" {
		t.Fatalf("expected base value 研发 kept, got %q", got)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", conflicts)
	}
	c := conflicts[0]
	if c.Key != "王五" || c.Field != "部门" || c.Kept != "研发" || c.Dropped != "销售" {
		t.Fatalf("unexpected conflict record: %+v", c)
	}
}

func TestCoalesceRows_IdenticalOverlapNoConflict(t *testing.T) {
	// 两行同一字段的非空值相同，不构成冲突。
	rows := []Row{
		{"姓名": "赵六", "部门": "研发", "手机": "13800000000"},
		{"姓名": "赵六", "部门": "研发", "手机": "", "工号": "1006"},
	}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts for identical overlap, got %+v", conflicts)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 merged row, got %d: %+v", len(out), out)
	}
	want := Row{"姓名": "赵六", "部门": "研发", "手机": "13800000000", "工号": "1006"}
	if !reflect.DeepEqual(out[0], want) {
		t.Fatalf("merged row = %v, want %v", out[0], want)
	}
}

func TestCoalesceRows_KeepsRowsWithoutName(t *testing.T) {
	// uniqueField 为空的行原样通过（规划阶段会将其计为 SkippedNoName），
	// 且永不产生冲突。
	rows := []Row{
		{"姓名": "", "部门": "研发"},
		{"姓名": "", "部门": "销售"},
		{"姓名": "钱七", "部门": "研发"},
		{"姓名": "钱七", "部门": "研发"},
	}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 rows (2 no-name + 1 merged), got %d: %+v", len(out), out)
	}
	if out[0]["部门"] != "研发" || out[1]["部门"] != "销售" {
		t.Fatalf("no-name rows must pass through unchanged: %+v", out)
	}
	if out[2]["部门"] != "研发" {
		t.Fatalf("expected merged name-keyed row, got %+v", out[2])
	}
}

func TestCoalesceRows_SingleRowPassesThrough(t *testing.T) {
	rows := []Row{{"姓名": "孙八", "部门": "研发"}}
	out, conflicts := CoalesceRows(rows, "姓名")
	if len(conflicts) != 0 || len(out) != 1 {
		t.Fatalf("expected single row unchanged, got %+v conflicts, %d rows", conflicts, len(out))
	}
	if !reflect.DeepEqual(out[0], rows[0]) {
		t.Fatalf("single row must pass through unchanged: %v", out[0])
	}
}
