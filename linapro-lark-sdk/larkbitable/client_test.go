package larkbitable

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// fakeHTTP 实现 larkcore.HttpClient（仅需 Do 方法），拦截飞书请求：
// 鉴权请求返回固定 token；batch_get 请求按传入的 record_ids 回放命中行，
// 并把 absentSet 中的 id 放入 absent_record_ids。同时记录每批传入的 record_ids
// 以便断言切批行为。
type fakeHTTP struct {
	// batchCalls 记录每次 batch_get 收到的 record_ids（按调用顺序）。
	batchCalls [][]string
	// rows 为全部可命中的行，按 record_id 索引。
	rows map[string]map[string]any
	// absentSet 中的 record_id 会被归入 absent_record_ids，不出现在 records。
	absentSet map[string]bool
	// writeCalls 记录每次 batch_create/batch_update 收到的 records 数组（按调用顺序），
	// 供断言空字段记录被跳过、以及全空批次不发起请求。
	writeCalls [][]map[string]any
	// writeUserIDTypes 记录每次写入请求下发的 user_id_type 查询参数（按调用顺序）。
	// 人员字段以何种 id 语义写入由它决定，空串表示未下发该参数。
	writeUserIDTypes []string
	// listRecordsRows 为 list（全量列举）接口的预设行，格式为
	// []map[string]any，每项含 "record_id" 和 "fields" 两个键。
	listRecordsRows []map[string]any
}

func (f *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	path := req.URL.Path

	// 鉴权：返回固定 tenant_access_token。
	if strings.HasSuffix(path, "/auth/v3/tenant_access_token/internal") ||
		strings.HasSuffix(path, "/auth/v3/app_access_token/internal") {
		return jsonResp(`{"code":0,"msg":"ok","tenant_access_token":"t","app_access_token":"t","expire":7200}`), nil
	}

	// batch_get：解析 record_ids，回放命中行与 absent。
	if strings.HasSuffix(path, "/records/batch_get") {
		var body struct {
			RecordIds []string `json:"record_ids"`
		}
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)
		f.batchCalls = append(f.batchCalls, body.RecordIds)

		type recordJSON struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		}
		var records []recordJSON
		var absent []string
		for _, id := range body.RecordIds {
			if f.absentSet[id] {
				absent = append(absent, id)
				continue
			}
			if fields, ok := f.rows[id]; ok {
				records = append(records, recordJSON{RecordID: id, Fields: fields})
			}
		}
		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"records":           records,
				"absent_record_ids": absent,
			},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// list（全量拉取）：回放 listRecordsRows，不分页。
	if strings.Contains(path, "/records") && req.Method == http.MethodGet {
		type recJSON struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		}
		records := make([]recJSON, 0, len(f.listRecordsRows))
		for _, r := range f.listRecordsRows {
			id, _ := r["record_id"].(string)
			fields, _ := r["fields"].(map[string]any)
			records = append(records, recJSON{RecordID: id, Fields: fields})
		}
		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{
				"items":    records,
				"has_more": false,
			},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	// batch_create/batch_update：记录本次请求的 records（含各记录 fields），
	// 回放一批与请求等长的 record_id，供断言空字段记录被跳过。
	if strings.HasSuffix(path, "/records/batch_create") || strings.HasSuffix(path, "/records/batch_update") {
		var body struct {
			Records []struct {
				Fields map[string]any `json:"fields"`
			} `json:"records"`
		}
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)

		fieldsList := make([]map[string]any, 0, len(body.Records))
		records := make([]map[string]any, 0, len(body.Records))
		for i, r := range body.Records {
			fieldsList = append(fieldsList, r.Fields)
			records = append(records, map[string]any{"record_id": "new" + itoa(i)})
		}
		f.writeCalls = append(f.writeCalls, fieldsList)
		f.writeUserIDTypes = append(f.writeUserIDTypes, req.URL.Query().Get("user_id_type"))

		out := map[string]any{
			"code": 0,
			"msg":  "success",
			"data": map[string]any{"records": records},
		}
		b, _ := json.Marshal(out)
		return jsonResp(string(b)), nil
	}

	return jsonResp(`{"code":0,"msg":"ok"}`), nil
}

func jsonResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
}

// newTestClient 构造一个注入了 fake HttpClient 的 Client（同包可直接装配私有字段）。
func newTestClient(f *fakeHTTP) *Client {
	return &Client{
		client:  lark.NewClient("id", "secret", lark.WithHttpClient(f)),
		encoder: DefaultEncoder(),
	}
}

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

// fieldTypesEmpty 返回空字段类型映射。测试中这些记录仅含纯字符串/数字单元格，无需类型
// 即可正确回读；空/未知类型统一走 CellFallback。
func fieldTypesEmpty() map[string]int {
	return map[string]int{}
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

// itoa 是 strconv.Itoa 的短别名，便于批量构造测试 record_id。
func itoa(i int) string {
	return strconv.Itoa(i)
}
