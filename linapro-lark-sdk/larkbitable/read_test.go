package larkbitable

import (
	"context"
	"strings"
	"testing"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// TestBatchGetByIDs_SplitsIntoBatches 验证 250 个 record_id 切成 100+100+50 三批，
// 且全部命中行以 record_id 为键返回。
func TestBatchGetByIDs_SplitsIntoBatches(t *testing.T) {
	rows := make(map[string]map[string]any, 250)
	ids := make([]string, 0, 250)
	for i := range 250 {
		id := "rec" + itoa(i)
		ids = append(ids, id)
		rows[id] = map[string]any{"col": "v" + itoa(i)}
	}
	f := &fakeHTTP{rows: rows, absentSet: map[string]bool{}}
	c := newTestClient(f)

	got, absent, err := c.BatchGetByIDs(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, ids, fieldTypesEmpty())
	if err != nil {
		t.Fatalf("BatchGetByIDs: %v", err)
	}
	if len(got) != 250 {
		t.Errorf("命中行数应为 250, got=%d", len(got))
	}
	if len(absent) != 0 {
		t.Errorf("absent 应为空, got=%v", absent)
	}
	wantBatchSizes := []int{100, 100, 50}
	if len(f.batchCalls) != len(wantBatchSizes) {
		t.Fatalf("应切 %d 批, 实际 %d 批", len(wantBatchSizes), len(f.batchCalls))
	}
	for i, want := range wantBatchSizes {
		if len(f.batchCalls[i]) != want {
			t.Errorf("第 %d 批应含 %d 个 id, got=%d", i+1, want, len(f.batchCalls[i]))
		}
	}
}

// TestBatchGetByIDs_AbsentRecords 验证已删除的 record_id 落入 absent、不出现在命中映射。
func TestBatchGetByIDs_AbsentRecords(t *testing.T) {
	f := &fakeHTTP{
		rows: map[string]map[string]any{
			"rec1": {"col": "a"},
			"rec3": {"col": "c"},
		},
		absentSet: map[string]bool{"rec2": true},
	}
	c := newTestClient(f)

	got, absent, err := c.BatchGetByIDs(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, []string{"rec1", "rec2", "rec3"}, fieldTypesEmpty())
	if err != nil {
		t.Fatalf("BatchGetByIDs: %v", err)
	}
	if _, ok := got["rec2"]; ok {
		t.Errorf("rec2 已删除, 不应出现在命中映射")
	}
	if len(got) != 2 {
		t.Errorf("命中行数应为 2, got=%d", len(got))
	}
	if len(absent) != 1 || absent[0] != "rec2" {
		t.Errorf("absent 应为 [rec2], got=%v", absent)
	}
}

// TestBatchGetByIDs_EmptyShortCircuits 验证空列表直接返回、不发起任何请求。
func TestBatchGetByIDs_EmptyShortCircuits(t *testing.T) {
	f := &fakeHTTP{rows: map[string]map[string]any{}, absentSet: map[string]bool{}}
	c := newTestClient(f)

	got, absent, err := c.BatchGetByIDs(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, nil, fieldTypesEmpty())
	if err != nil {
		t.Fatalf("BatchGetByIDs: %v", err)
	}
	if len(got) != 0 || len(absent) != 0 {
		t.Errorf("空列表应返回空结果, got=%v absent=%v", got, absent)
	}
	if len(f.batchCalls) != 0 {
		t.Errorf("空列表不应发起 batch_get 请求, 实际发起 %d 次", len(f.batchCalls))
	}
}

// TestListRecords_SingleColumnKey 验证单列 keyOf 闭包（TrimSpace）正确建键，
// 空键记录被跳过。
func TestListRecords_SingleColumnKey(t *testing.T) {
	f := &fakeHTTP{}
	f.listRecordsRows = []map[string]any{
		{"record_id": "r1", "fields": map[string]any{"工号": "A001", "姓名": "张三"}},
		{"record_id": "r2", "fields": map[string]any{"工号": "  A002  ", "姓名": "李四"}},
		{"record_id": "r3", "fields": map[string]any{"工号": "", "姓名": "无工号"}}, // 空键，跳过
	}
	c := newTestClient(f)

	keyOf := func(row Row) string {
		return strings.TrimSpace(row["工号"])
	}
	got, err := c.ListRecords(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, keyOf, fieldTypesEmpty())
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应返回 2 条记录（空键跳过），got=%d", len(got))
	}
	if got["A001"].RecordID != "r1" {
		t.Errorf("A001 应对应 r1, got=%s", got["A001"].RecordID)
	}
	if got["A002"].RecordID != "r2" {
		t.Errorf("A002（TrimSpace 后）应对应 r2, got=%s", got["A002"].RecordID)
	}
	if _, ok := got[""]; ok {
		t.Error("空键记录不应出现在结果中")
	}
}

// TestListRecords_MultiColumnKey 验证复合键闭包（两列拼接）正确建键。
func TestListRecords_MultiColumnKey(t *testing.T) {
	f := &fakeHTTP{}
	f.listRecordsRows = []map[string]any{
		{"record_id": "r1", "fields": map[string]any{"工号": "A001", "考勤月": "2026-09"}},
		{"record_id": "r2", "fields": map[string]any{"工号": "A002", "考勤月": "2026-09"}},
		{"record_id": "r3", "fields": map[string]any{"工号": "A001", "考勤月": ""}}, // 考勤月空 → 键空，跳过
	}
	c := newTestClient(f)

	keyOf := func(row Row) string {
		a := strings.TrimSpace(row["工号"])
		b := strings.TrimSpace(row["考勤月"])
		if a == "" || b == "" {
			return ""
		}
		return a + "-" + b
	}
	got, err := c.ListRecords(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, keyOf, fieldTypesEmpty())
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应返回 2 条记录（复合键含空列的跳过），got=%d", len(got))
	}
	if got["A001-2026-09"].RecordID != "r1" {
		t.Errorf("复合键 A001-2026-09 应对应 r1, got=%v", got["A001-2026-09"])
	}
	if got["A002-2026-09"].RecordID != "r2" {
		t.Errorf("复合键 A002-2026-09 应对应 r2, got=%v", got["A002-2026-09"])
	}
}

// TestListRecordsByID_PersonBypass 验证 ListRecordsByID 对含 TypeUser 列的表回读人员 open_id
// 集合到 Persons 旁路：文本投影 Fields 存去重姓名串，Persons 存官方 Person 结构（含 id=open_id）。
func TestListRecordsByID_PersonBypass(t *testing.T) {
	f := &fakeHTTP{}
	f.listRecordsRows = []map[string]any{
		{"record_id": "r1", "fields": map[string]any{
			"面试官": []any{
				map[string]any{"id": "ou_zhang", "name": "张三"},
				map[string]any{"id": "ou_li", "name": "李四"},
			},
			"轮次": "一面",
		}},
	}
	c := newTestClient(f)
	fieldTypes := map[string]int{"面试官": larkbitablesdk.TypeUser, "轮次": larkbitablesdk.TypeText}

	got, err := c.ListRecordsByID(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, fieldTypes)
	if err != nil {
		t.Fatalf("ListRecordsByID: %v", err)
	}
	rec, ok := got["r1"]
	if !ok {
		t.Fatalf("应以 record_id r1 为键返回记录, got=%v", got)
	}
	if rec.RecordID != "r1" {
		t.Errorf("RecordID 应为 r1, got=%q", rec.RecordID)
	}
	// 文本投影：人员列回读去重姓名串。
	if rec.Fields["面试官"] != "张三,李四" {
		t.Errorf("Fields[面试官] 应为去重姓名串, got=%q", rec.Fields["面试官"])
	}
	if rec.Fields["轮次"] != "一面" {
		t.Errorf("Fields[轮次] 应为 一面, got=%q", rec.Fields["轮次"])
	}
	// 人员旁路：解析出 open_id 集合。
	persons := rec.Persons["面试官"]
	if len(persons) != 2 {
		t.Fatalf("Persons[面试官] 应有 2 人, got=%d", len(persons))
	}
	ids := map[string]bool{}
	for _, p := range persons {
		if p != nil && p.Id != nil {
			ids[*p.Id] = true
		}
	}
	if !ids["ou_zhang"] || !ids["ou_li"] {
		t.Errorf("Persons[面试官] 应含 ou_zhang/ou_li, got=%v", ids)
	}
}

// TestListRecordsByID_NoPersonColumn 验证无 TypeUser 列时人员旁路为空、文本投影不受影响。
func TestListRecordsByID_NoPersonColumn(t *testing.T) {
	f := &fakeHTTP{}
	f.listRecordsRows = []map[string]any{
		{"record_id": "r1", "fields": map[string]any{"applicationId": "101", "评分": "90"}},
	}
	c := newTestClient(f)
	fieldTypes := map[string]int{"applicationId": larkbitablesdk.TypeText, "评分": larkbitablesdk.TypeNumber}

	got, err := c.ListRecordsByID(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, fieldTypes)
	if err != nil {
		t.Fatalf("ListRecordsByID: %v", err)
	}
	rec, ok := got["r1"]
	if !ok {
		t.Fatalf("应返回 r1, got=%v", got)
	}
	if len(rec.Persons) != 0 {
		t.Errorf("无人员列时 Persons 应为空, got=%v", rec.Persons)
	}
	if rec.Fields["applicationId"] != "101" || rec.Fields["评分"] != "90" {
		t.Errorf("文本投影不应受影响, got=%v", rec.Fields)
	}
}
