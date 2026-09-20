// report_sync_test.go 验证需求3「报表评分回写」关键行为（FB-10）：
//   - collectReportOps 遍历多个报表 ID 逐表拉取，单个报表失败不阻断其余报表；
//   - collectReportOps 按 applicationId 匹配：命中生成更新、未命中生成新建；
//   - collectReportOps 仅回写指定目标列（姓名/人才画像评分/匹配度等级），不覆盖其他列；
//   - mergeReportUpdates 按 recordID 合并、mergeReportCreates 按 applicationId 合并，避免重复项；
//   - RunReportSync 在报表 ID 列表为空或报表评分表未配置时直接跳过，不触发任何网络调用。
//
// 测试自包含且顺序无关：每个测试自行构造 Moka HTTP 替身与配置，不依赖共享状态。
package job

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
)

// reportDataPath 与 moka-recruit 客户端约定的 getReportData 路径一致。
const reportDataPath = "/api-platform/v1/getReportData"

// reportColumnsFixture 返回测试用的报表评分表列映射（与 defaultReportFieldMapping 需求3 一致）：
// Moka 报表源列标题（候选人/最终总分/匹配度定级备份）→ 飞书目标列名（姓名/人才画像评分/匹配度等级），
// 「申请」映射到飞书唯一键列 applicationId。两侧列名故意不同，覆盖 FB-12 的核心场景。
func reportColumnsFixture() reportColumns {
	return resolveReportColumns(map[string]string{
		config.ReportSourceApplicationTitle: "applicationId",
		"候选人":                               "姓名",
		"最终总分":                              "人才画像评分",
		"匹配度定级备份":                           "匹配度等级",
	})
}

// reportRow 是 getReportData 响应中单行数据的 JSON 结构，字段名对应报表 dataIndex。
// app 为「申请」列值（applicationId），name/score/match 为目标列值。
func reportRow(app, name, score, match string) map[string]string {
	return map[string]string{"app": app, "name": name, "score": score, "match": match}
}

// reportResponse 构造一个成功报表响应：headers 使用 **Moka 侧列标题**（申请/候选人/最终总分/
// 匹配度定级备份），与飞书目标列名不同，验证 mapReportColumns 按 report_field_mapping 正确桥接两侧。
// dataIndex 用 app/name/score/match 便于断言。
func reportResponse(rows ...map[string]string) string {
	headers := []map[string]string{
		{"dataIndex": "app", "title": "申请", "type": "text"},
		{"dataIndex": "name", "title": "候选人", "type": "text"},
		{"dataIndex": "score", "title": "最终总分", "type": "number"},
		{"dataIndex": "match", "title": "匹配度定级备份", "type": "text"},
	}
	payload := map[string]any{"headers": headers, "rows": rows}
	b, _ := json.Marshal(map[string]any{"code": 200, "msg": "success", "data": payload})
	return string(b)
}

// TestCollectReportOps_InsertsAndUpdates 验证按 applicationId 匹配的插入/更新语义：
// 命中索引的 applicationId 生成更新，未命中的生成新建（补齐 applicationId 列 + 目标列）。
func TestCollectReportOps_InsertsAndUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(reportResponse(
			reportRow("101", "张三", "80", "A"), // 已存在 → 更新
			reportRow("102", "李四", "90", "B"), // 不存在 → 新建
		)))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		MokaBaseURL:      srv.URL,
		RecruitReportIDs: []int64{1001},
	}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
	existingRows := map[string]existingRow{"101": {recordID: "rec1", fields: lark.Row{"applicationId": "101"}}}

	creates, updates, _ := collectReportOps(context.Background(), cfg, mokaClient, existingRows, reportColumnsFixture(), map[string]int{})

	if len(updates) != 1 {
		t.Fatalf("期望 1 条更新, got %d (%v)", len(updates), updates)
	}
	if updates[0].RecordID != "rec1" || updates[0].Fields["人才画像评分"] != "80" || updates[0].Fields["姓名"] != "张三" {
		t.Fatalf("更新内容错误: got recordID=%q fields=%v", updates[0].RecordID, updates[0].Fields)
	}
	if len(creates) != 1 {
		t.Fatalf("期望 1 条新建, got %d (%v)", len(creates), creates)
	}
	got := creates[0].Fields
	if got["applicationId"] != "102" || got["人才画像评分"] != "90" || got["姓名"] != "李四" || got["匹配度等级"] != "B" {
		t.Fatalf("新建内容错误: got fields=%v", got)
	}
}

// TestCollectReportOps_OnlyWritesConfiguredColumns 验证仅回写指定目标列（姓名/人才画像评分/匹配度等级），
// 不携带报表中其他未配置列。
func TestCollectReportOps_OnlyWritesConfiguredColumns(t *testing.T) {
	// 报表包含一个未配置的列（"性别"），回写结果不应包含该列。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		headers := []map[string]string{
			{"dataIndex": "app", "title": "申请", "type": "text"},
			{"dataIndex": "name", "title": "候选人", "type": "text"},
			{"dataIndex": "score", "title": "最终总分", "type": "number"},
			{"dataIndex": "match", "title": "匹配度定级备份", "type": "text"},
			{"dataIndex": "gender", "title": "性别", "type": "text"},
		}
		rows := []map[string]string{{"app": "103", "name": "王五", "score": "70", "match": "C", "gender": "男"}}
		payload := map[string]any{"headers": headers, "rows": rows}
		b, _ := json.Marshal(map[string]any{"code": 200, "msg": "success", "data": payload})
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		MokaBaseURL:      srv.URL,
		RecruitReportIDs: []int64{1001},
	}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))

	creates, updates, _ := collectReportOps(context.Background(), cfg, mokaClient, map[string]existingRow{}, reportColumnsFixture(), map[string]int{})

	if len(updates) != 0 {
		t.Fatalf("期望 0 条更新, got %d", len(updates))
	}
	if len(creates) != 1 {
		t.Fatalf("期望 1 条新建, got %d", len(creates))
	}
	if _, ok := creates[0].Fields["性别"]; ok {
		t.Fatalf("不应携带未配置的「性别」列, got fields=%v", creates[0].Fields)
	}
	if creates[0].Fields["人才画像评分"] != "70" {
		t.Fatalf("人才画像评分: got=%q want=70", creates[0].Fields["人才画像评分"])
	}
}

// TestCollectReportOps_MultipleReportsAndFailureContinues 验证遍历多个报表 ID 逐表拉取，
// 单表（1003）返回错误时记录 warn 并继续，其余报表的命中照常生成操作。
func TestCollectReportOps_MultipleReportsAndFailureContinues(t *testing.T) {
	var hitReportIDs []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			t.Errorf("unexpected moka path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ReportID int64 `json:"reportId"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode getReportData body: %v", err)
		}
		hitReportIDs = append(hitReportIDs, req.ReportID)
		switch req.ReportID {
		case 1001:
			_, _ = w.Write([]byte(reportResponse(reportRow("101", "张三", "80", "A"))))
		case 1002:
			_, _ = w.Write([]byte(reportResponse(reportRow("102", "李四", "90", "B"))))
		case 1003:
			// 单表失败：返回非 2xx，触发 PostJSON 报错，验证不阻断其余报表。
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected reportId: %d", req.ReportID)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		MokaBaseURL:      srv.URL,
		RecruitReportIDs: []int64{1001, 1002, 1003},
	}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
	existingRows := map[string]existingRow{
		"101": {recordID: "rec1", fields: lark.Row{"applicationId": "101"}},
		"102": {recordID: "rec2", fields: lark.Row{"applicationId": "102"}},
	}

	creates, updates, _ := collectReportOps(context.Background(), cfg, mokaClient, existingRows, reportColumnsFixture(), map[string]int{})

	if len(creates) != 0 {
		t.Fatalf("expected 0 creates, got %d", len(creates))
	}
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates (101/102), got %d", len(updates))
	}
	got := map[string]string{}
	for _, u := range updates {
		got[u.RecordID] = asString(u.Fields["人才画像评分"])
	}
	if got["rec1"] != "80" {
		t.Fatalf("rec1 人才画像评分: got=%q want=80", got["rec1"])
	}
	if got["rec2"] != "90" {
		t.Fatalf("rec2 人才画像评分: got=%q want=90", got["rec2"])
	}
	// 三个报表 ID 都应被尝试，失败的单表（1003）不阻断 1001/1002 的回写。
	if len(hitReportIDs) != 3 {
		t.Fatalf("expected 3 GetReportData calls, got %d (%v)", len(hitReportIDs), hitReportIDs)
	}
}

// TestCollectReportOps_OnlyUpdatesChangedColumns 验证命中现有行后仅更新变更列：
// 现有行姓名/匹配度等级已与报表一致，仅人才画像评分变化 → 更新只含人才画像评分一列。
func TestCollectReportOps_OnlyUpdatesChangedColumns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(reportResponse(reportRow("101", "张三", "88", "A"))))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{MokaBaseURL: srv.URL, RecruitReportIDs: []int64{1001}}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
	// 现有行：姓名/匹配度等级已一致，人才画像评分为旧值 80。
	existingRows := map[string]existingRow{
		"101": {recordID: "rec1", fields: lark.Row{"applicationId": "101", "姓名": "张三", "人才画像评分": "80", "匹配度等级": "A"}},
	}

	creates, updates, frozen := collectReportOps(context.Background(), cfg, mokaClient, existingRows, reportColumnsFixture(), map[string]int{})

	if len(creates) != 0 || frozen != 0 {
		t.Fatalf("期望无新建无冻结, got creates=%d frozen=%d", len(creates), frozen)
	}
	if len(updates) != 1 {
		t.Fatalf("期望 1 条更新, got %d", len(updates))
	}
	if len(updates[0].Fields) != 1 || updates[0].Fields["人才画像评分"] != "88" {
		t.Fatalf("更新应只含人才画像评分一列, got=%v", updates[0].Fields)
	}
}

// TestCollectReportOps_AllEqualFreezes 验证全部目标列与现有值相等时冻结、不产出写入。
func TestCollectReportOps_AllEqualFreezes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(reportResponse(reportRow("101", "张三", "80", "A"))))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{MokaBaseURL: srv.URL, RecruitReportIDs: []int64{1001}}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
	existingRows := map[string]existingRow{
		"101": {recordID: "rec1", fields: lark.Row{"applicationId": "101", "姓名": "张三", "人才画像评分": "80", "匹配度等级": "A"}},
	}

	creates, updates, frozen := collectReportOps(context.Background(), cfg, mokaClient, existingRows, reportColumnsFixture(), map[string]int{})

	if len(creates) != 0 || len(updates) != 0 || frozen != 1 {
		t.Fatalf("全列相等应冻结, got creates=%d updates=%d frozen=%d", len(creates), len(updates), frozen)
	}
}

// TestCollectReportOps_NumberEquivalentFreezes 验证数字列按 float64 等价时冻结：
// 现有回读 "80"（Bitable 数字列），报表值 "80.0"，人才画像评分列声明为 TypeNumber。
func TestCollectReportOps_NumberEquivalentFreezes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(reportResponse(reportRow("101", "张三", "80.0", "A"))))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{MokaBaseURL: srv.URL, RecruitReportIDs: []int64{1001}}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
	existingRows := map[string]existingRow{
		"101": {recordID: "rec1", fields: lark.Row{"applicationId": "101", "姓名": "张三", "人才画像评分": "80", "匹配度等级": "A"}},
	}
	fieldTypes := map[string]int{"人才画像评分": larkbitablesdk.TypeNumber}

	creates, updates, frozen := collectReportOps(context.Background(), cfg, mokaClient, existingRows, reportColumnsFixture(), fieldTypes)

	if len(creates) != 0 || len(updates) != 0 || frozen != 1 {
		t.Fatalf("数字 float64 等价应冻结, got creates=%d updates=%d frozen=%d", len(creates), len(updates), frozen)
	}
}

// TestMergeReportUpdates_MergesByRecordID 验证同一 applicationId 在多个报表中命中时按 recordID 合并，
// 后出现的报表覆盖先出现的同列值，最终每个 recordID 只保留一条更新。
func TestMergeReportUpdates_MergesByRecordID(t *testing.T) {
	updates := []lark.UpdateOp{
		{RecordID: "rec1", Fields: lark.WriteRow{"人才画像评分": "80"}},
		{RecordID: "rec2", Fields: lark.WriteRow{"人才画像评分": "90"}},
		{RecordID: "rec1", Fields: lark.WriteRow{"人才画像评分": "85"}},
	}

	merged := mergeReportUpdates(updates)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged updates, got %d", len(merged))
	}
	byID := map[string]lark.WriteRow{}
	for _, u := range merged {
		byID[u.RecordID] = u.Fields
	}
	if got := byID["rec1"]["人才画像评分"]; got != "85" {
		t.Fatalf("rec1 应被后出现的报表覆盖: got=%q want=85", got)
	}
	if got := byID["rec2"]["人才画像评分"]; got != "90" {
		t.Fatalf("rec2 人才画像评分: got=%q want=90", got)
	}
}

// TestMergeReportCreates_MergesByApplicationID 验证同一 applicationId 在多个报表中命中时
// 按 applicationId 合并新建，后出现的报表覆盖先出现的同列值，避免创建重复行。
func TestMergeReportCreates_MergesByApplicationID(t *testing.T) {
	creates := []lark.CreateOp{
		{Fields: lark.WriteRow{"applicationId": "101", "人才画像评分": "80"}},
		{Fields: lark.WriteRow{"applicationId": "102", "人才画像评分": "90"}},
		{Fields: lark.WriteRow{"applicationId": "101", "人才画像评分": "85"}},
	}

	merged := mergeReportCreates(creates, "applicationId")

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged creates, got %d", len(merged))
	}
	byApp := map[string]lark.WriteRow{}
	for _, c := range merged {
		byApp[asString(c.Fields["applicationId"])] = c.Fields
	}
	if got := byApp["101"]["人才画像评分"]; got != "85" {
		t.Fatalf("applicationId=101 应被后出现的报表覆盖: got=%q want=85", got)
	}
	if got := byApp["102"]["人才画像评分"]; got != "90" {
		t.Fatalf("applicationId=102 人才画像评分: got=%q want=90", got)
	}
}

// TestRunReportSync_SkipsWhenNoReportIDs 验证报表 ID 列表为空时直接跳过本轮，
// 不发起任何 Moka 或 Bitable 网络调用。
func TestRunReportSync_SkipsWhenNoReportIDs(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{MokaBaseURL: srv.URL, ReportBitableTableID: "tblReport"}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))

	if err := RunReportSync(context.Background(), cfg, mokaClient); err != nil {
		t.Fatalf("RunReportSync with empty report IDs should return nil, got %v", err)
	}
	if hit {
		t.Fatalf("RunReportSync 在报表 ID 为空时不应发起网络调用")
	}
}

// TestRunReportSync_SkipsWhenReportTableUnconfigured 验证报表评分表 table id 未配置时
// 直接跳过本轮（不回退候选人决策表），不发起任何 Moka 或 Bitable 网络调用，
// 避免把报表评分误写进候选人决策表。
func TestRunReportSync_SkipsWhenReportTableUnconfigured(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	// 有报表 ID 但报表表 table id 为空：应在拉取报表前就跳过。
	cfg := &config.Config{
		MokaBaseURL:          srv.URL,
		RecruitReportIDs:     []int64{1001},
		ReportBitableTableID: "", // 未配置
	}
	mokaClient := mokabackend.NewClient(cfg.MokaBaseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))

	if err := RunReportSync(context.Background(), cfg, mokaClient); err != nil {
		t.Fatalf("RunReportSync 在报表表未配置时应返回 nil, got %v", err)
	}
	if hit {
		t.Fatalf("RunReportSync 在报表表未配置时不应发起网络调用")
	}
}

// TestMapReportColumns_BridgesMokaTitleToLarkColumn 是 FB-12 的核心回归：报表 headers 使用
// Moka 侧列标题（申请/候选人/最终总分/匹配度定级备份），与飞书目标列名（applicationId/姓名/
// 人才画像评分/匹配度等级）不同。验证 mapReportColumns 按 report_field_mapping 的 key（Moka 标题）
// 定位 dataIndex、以 value（飞书列名）作为 colIndex 的键，三列全部命中，不再误报 not found。
func TestMapReportColumns_BridgesMokaTitleToLarkColumn(t *testing.T) {
	headers := []mokabackend.ReportHeader{
		{DataIndex: "app", Title: "申请"},
		{DataIndex: "name", Title: "候选人"},
		{DataIndex: "score", Title: "最终总分"},
		{DataIndex: "match", Title: "匹配度定级备份"},
	}

	appDataIndex, colIndex, err := mapReportColumns(context.Background(), headers, reportColumnsFixture())
	if err != nil {
		t.Fatalf("mapReportColumns 不应报错: %v", err)
	}
	if appDataIndex != "app" {
		t.Fatalf("「申请」列 dataIndex: got=%q want=app", appDataIndex)
	}
	// colIndex 的键是飞书目标列名，值是 Moka 报表 dataIndex。
	want := map[string]string{"姓名": "name", "人才画像评分": "score", "匹配度等级": "match"}
	if len(colIndex) != len(want) {
		t.Fatalf("colIndex 应含 %d 个目标列, got %d (%v)", len(want), len(colIndex), colIndex)
	}
	for larkCol, wantDI := range want {
		if got := colIndex[larkCol]; got != wantDI {
			t.Fatalf("飞书列 %q → dataIndex: got=%q want=%q", larkCol, got, wantDI)
		}
	}
}

// TestMapReportColumns_MissingApplicationErrors 验证报表缺少固定「申请」列时返回 error（供逐表跳过）。
func TestMapReportColumns_MissingApplicationErrors(t *testing.T) {
	headers := []mokabackend.ReportHeader{
		{DataIndex: "name", Title: "候选人"},
	}
	if _, _, err := mapReportColumns(context.Background(), headers, reportColumnsFixture()); err == nil {
		t.Fatalf("缺少「申请」列时应返回 error")
	}
}

// TestMapReportColumns_MissingSourceColumnSkipped 验证某个值列的 Moka 源标题在报表中缺失时
// 仅跳过该列（记录 warn），其余列正常映射，不阻断整轮。
func TestMapReportColumns_MissingSourceColumnSkipped(t *testing.T) {
	// 报表缺少「最终总分」列，其余列齐全。
	headers := []mokabackend.ReportHeader{
		{DataIndex: "app", Title: "申请"},
		{DataIndex: "name", Title: "候选人"},
		{DataIndex: "match", Title: "匹配度定级备份"},
	}

	_, colIndex, err := mapReportColumns(context.Background(), headers, reportColumnsFixture())
	if err != nil {
		t.Fatalf("mapReportColumns 不应报错: %v", err)
	}
	if _, ok := colIndex["人才画像评分"]; ok {
		t.Fatalf("缺失源列「最终总分」对应的飞书列「人才画像评分」不应出现在 colIndex: %v", colIndex)
	}
	if colIndex["姓名"] != "name" || colIndex["匹配度等级"] != "match" {
		t.Fatalf("其余列应正常映射, got=%v", colIndex)
	}
}

// TestMapReportColumns_MultiSourceToSingleLarkColumn 是 FB-14 的回归：匹配度列来自三个不同
// 报表数据源、各表字段名无法统一（匹配度等级-初/中/高），但都映射到飞书同一列「匹配度等级」。
// 每个报表只含其中一个源列，验证 mapReportColumns 都能把该源列 dataIndex 解析到飞书「匹配度等级」。
func TestMapReportColumns_MultiSourceToSingleLarkColumn(t *testing.T) {
	cols := resolveReportColumns(map[string]string{
		config.ReportSourceApplicationTitle: "applicationId",
		"匹配度等级-初":                           "匹配度等级",
		"匹配度等级-中":                           "匹配度等级",
		"匹配度等级-高":                           "匹配度等级",
	})

	// 三个数据源各自的报表，匹配度列标题不同但都应回写飞书「匹配度等级」。
	cases := []struct{ title, dataIndex string }{
		{"匹配度等级-初", "m1"},
		{"匹配度等级-中", "m2"},
		{"匹配度等级-高", "m3"},
	}
	for _, c := range cases {
		headers := []mokabackend.ReportHeader{
			{DataIndex: "app", Title: "申请"},
			{DataIndex: c.dataIndex, Title: c.title},
		}
		_, colIndex, err := mapReportColumns(context.Background(), headers, cols)
		if err != nil {
			t.Fatalf("源列 %q: mapReportColumns 不应报错: %v", c.title, err)
		}
		if got := colIndex["匹配度等级"]; got != c.dataIndex {
			t.Fatalf("源列 %q 应映射到飞书「匹配度等级」列 dataIndex=%q, got=%q (colIndex=%v)", c.title, c.dataIndex, got, colIndex)
		}
	}
}

// TestMapReportColumns_MultiSourcePartialMissingStillMaps 是 FB-15 的核心回归：匹配度目标列由三个
// 候选源列（初/中/高）供给，当前报表只含其中一个（匹配度等级-中），另两个缺失。验证按目标列分组后
// 只要任一候选命中即成功——「匹配度等级」正常映射到命中源列的 dataIndex，不因另两个候选缺失而受影响。
func TestMapReportColumns_MultiSourcePartialMissingStillMaps(t *testing.T) {
	cols := resolveReportColumns(map[string]string{
		config.ReportSourceApplicationTitle: "applicationId",
		"匹配度等级-初":                           "匹配度等级",
		"匹配度等级-中":                           "匹配度等级",
		"匹配度等级-高":                           "匹配度等级",
	})
	// 报表只含「匹配度等级-中」，另两个候选源列缺失。
	headers := []mokabackend.ReportHeader{
		{DataIndex: "app", Title: "申请"},
		{DataIndex: "mid", Title: "匹配度等级-中"},
	}

	_, colIndex, err := mapReportColumns(context.Background(), headers, cols)
	if err != nil {
		t.Fatalf("mapReportColumns 不应报错: %v", err)
	}
	if got := colIndex["匹配度等级"]; got != "mid" {
		t.Fatalf("命中候选源列「匹配度等级-中」应映射到 dataIndex=mid, got=%q (colIndex=%v)", got, colIndex)
	}
}

// TestMapReportColumns_MultiSourceAllMissingSkipped 是 FB-15 的补充回归：匹配度三个候选源列全部
// 缺失时，「匹配度等级」目标列被跳过（不出现在 colIndex），其余单源列（姓名）正常映射。
// 验证「全部候选源列都缺失才跳过该目标列」，且不影响其他目标列。
func TestMapReportColumns_MultiSourceAllMissingSkipped(t *testing.T) {
	cols := resolveReportColumns(map[string]string{
		config.ReportSourceApplicationTitle: "applicationId",
		"候选人":                               "姓名",
		"匹配度等级-初":                           "匹配度等级",
		"匹配度等级-中":                           "匹配度等级",
		"匹配度等级-高":                           "匹配度等级",
	})
	// 报表含「申请」「候选人」，但匹配度三个候选源列全部缺失。
	headers := []mokabackend.ReportHeader{
		{DataIndex: "app", Title: "申请"},
		{DataIndex: "name", Title: "候选人"},
	}

	_, colIndex, err := mapReportColumns(context.Background(), headers, cols)
	if err != nil {
		t.Fatalf("mapReportColumns 不应报错: %v", err)
	}
	if _, ok := colIndex["匹配度等级"]; ok {
		t.Fatalf("匹配度三候选源列全缺时「匹配度等级」应被跳过, got colIndex=%v", colIndex)
	}
	if colIndex["姓名"] != "name" {
		t.Fatalf("单源列「姓名」应正常映射, got=%v", colIndex)
	}
}

// TestMergeReportUpdates_MultiSourceMatchLevelLastWins 验证同一 applicationId 在多个匹配度数据源
// 报表中命中时，回写飞书同一列「匹配度等级」按 recordID 合并、后出现报表覆盖先出现值（FB-14）。
func TestMergeReportUpdates_MultiSourceMatchLevelLastWins(t *testing.T) {
	updates := []lark.UpdateOp{
		{RecordID: "rec1", Fields: lark.WriteRow{"匹配度等级": "初级"}}, // 数据源1（匹配度等级-初）
		{RecordID: "rec1", Fields: lark.WriteRow{"匹配度等级": "高级"}}, // 数据源3（匹配度等级-高），后出现覆盖
	}

	merged := mergeReportUpdates(updates)

	if len(merged) != 1 {
		t.Fatalf("同一 recordID 应合并为 1 条, got %d", len(merged))
	}
	if got := merged[0].Fields["匹配度等级"]; got != "高级" {
		t.Fatalf("匹配度等级 应被后出现的数据源覆盖: got=%q want=高级", got)
	}
}
