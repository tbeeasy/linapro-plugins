package larkbitable

import (
	"context"
	"testing"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// personCellClient 构造一个启用 open_id 语义、注入了 fake HttpClient 的 Client：
// 人员列的值由调用方经 UpdateOp.Persons 旁路解析完毕后传入。
func personCellClient(f *fakeHTTP) *Client {
	return newTestClient(f).WithEncoder(Encoder{UserIDType: UserIDTypeOpenID})
}

// TestBatchUpdate_SkipsEmptyFieldRecords 验证：当某条更新的唯一差异列是人员列、但其 open_id
// 集合为空（编码后该列被跳列，fields 变空）时，该记录被跳过、不发往飞书；否则飞书会把整批
// 判为非法数据（99992402）。有 id 的记录仍正常写入。
func TestBatchUpdate_SkipsEmptyFieldRecords(t *testing.T) {
	f := &fakeHTTP{rows: map[string]map[string]any{}, absentSet: map[string]bool{}}
	c := personCellClient(f)
	fieldTypes := map[string]int{"姓名": larkbitablesdk.TypeUser}

	err := c.BatchUpdate(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, []UpdateOp{
		{RecordID: "rec_ok", Fields: WriteRow{}, Persons: map[string][]string{"姓名": {"ou_zhang"}}}, // 有 id：应写入
		{RecordID: "rec_bad", Fields: WriteRow{}, Persons: map[string][]string{"姓名": {}}},          // 空集合：跳列后 fields 空，应跳过
	}, fieldTypes)
	if err != nil {
		t.Fatalf("BatchUpdate: %v", err)
	}
	if len(f.writeCalls) != 1 {
		t.Fatalf("应发起 1 次 batch_update，实际 %d 次", len(f.writeCalls))
	}
	if got := len(f.writeCalls[0]); got != 1 {
		t.Fatalf("应只写入 1 条有 id 的记录（空字段记录被跳过），实际 %d 条", got)
	}
	// user_id_type 必须下发为 open_id：旁路传入的是该应用作用域的 open_id，
	// 参数缺失或被改写都会让飞书按错误语义解释 id。
	if len(f.writeUserIDTypes) != 1 || f.writeUserIDTypes[0] != UserIDTypeOpenID {
		t.Fatalf("应下发 user_id_type=open_id，实际 %v", f.writeUserIDTypes)
	}
}

// TestBatchCreate_WritesPersonCell 验证人员列经 Persons 旁路写入：多个 open_id 编码为
// [{id}] 对象数组、Row 中同名人员的裸文本被忽略、并按 open_id 下发 user_id_type。
func TestBatchCreate_WritesPersonCell(t *testing.T) {
	f := &fakeHTTP{rows: map[string]map[string]any{}, absentSet: map[string]bool{}}
	c := personCellClient(f)
	fieldTypes := map[string]int{"面试官": larkbitablesdk.TypeUser, "轮次": larkbitablesdk.TypeText}

	_, err := c.BatchCreate(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, []CreateOp{
		{
			Fields:  WriteRow{"面试官": "GZ000040", "轮次": "一面"}, // 人员列的裸工号应被忽略
			Persons: map[string][]string{"面试官": {"ou_a", "ou_b"}},
		},
	}, fieldTypes)
	if err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if len(f.writeCalls) != 1 || len(f.writeCalls[0]) != 1 {
		t.Fatalf("应发起 1 次 batch_create 写入 1 条记录，实际 %+v", f.writeCalls)
	}
	written := f.writeCalls[0][0]
	persons, ok := written["面试官"].([]any)
	if !ok || len(persons) != 2 {
		t.Fatalf("面试官列应写为 2 元素对象数组，got %T %v", written["面试官"], written["面试官"])
	}
	for i, wantID := range []string{"ou_a", "ou_b"} {
		cell, ok := persons[i].(map[string]any)
		if !ok || cell["id"] != wantID {
			t.Fatalf("第 %d 个人员元素应为 id=%s，got %v", i, wantID, persons[i])
		}
	}
	if got := written["轮次"]; got != "一面" {
		t.Fatalf("普通文本列应原样写入，got %v", got)
	}
	if len(f.writeUserIDTypes) != 1 || f.writeUserIDTypes[0] != UserIDTypeOpenID {
		t.Fatalf("应下发 user_id_type=open_id，实际 %v", f.writeUserIDTypes)
	}
}

// TestBatchUpdate_AllEmptySkipsRequest 验证：整批记录字段全空时不发起任何请求。
func TestBatchUpdate_AllEmptySkipsRequest(t *testing.T) {
	f := &fakeHTTP{rows: map[string]map[string]any{}, absentSet: map[string]bool{}}
	c := personCellClient(f)
	fieldTypes := map[string]int{"姓名": larkbitablesdk.TypeUser}

	err := c.BatchUpdate(context.Background(), Table{AppToken: "tok", TableID: "tbl"}, []UpdateOp{
		{RecordID: "rec_bad", Fields: WriteRow{"姓名": "外部人"}}, // 人员列裸文本被忽略，编码后全空
	}, fieldTypes)
	if err != nil {
		t.Fatalf("BatchUpdate: %v", err)
	}
	if len(f.writeCalls) != 0 {
		t.Fatalf("全空批次不应发起 batch_update，实际 %d 次", len(f.writeCalls))
	}
}
