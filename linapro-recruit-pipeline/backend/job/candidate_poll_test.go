// candidate_poll_test.go 验证需求1.4「简历归属者回填」关键行为（FB-18）：
//   - loadOwnerMap 按报表「申请」列与 owner_email_column 列建 applicationId→工号 映射；
//   - loadOwnerMap 的 HR 邮箱查找做 TrimSpace+ToLower 归一化（报表值与配置 key 两侧）；
//   - loadOwnerMap 在 owner_report_id 未配置、缺「申请」列、缺邮箱列、报表失败时降级返回空 map；
//   - collectOwnerBackfill 只补空、不覆写已写过归属者的行。
//
// 测试自包含且顺序无关：每个测试自行构造 Moka HTTP 替身与配置，不依赖共享状态。
package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	lark "linapro-lark-sdk/larkbitable"

	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
)

// fakeOwnerResolver 是「多工号 → open_id 集合」解析替身：GZ001/GZ005/GZ009 → ou_1xx，
// 其余工号解析不到（空集合）。用于验证人员列经 Persons 旁路写入的是已解析 open_id。
func fakeOwnerResolver(employeeNos []string) []string {
	var ids []string
	for _, no := range employeeNos {
		switch no {
		case "GZ001":
			ids = append(ids, "ou_101")
		case "GZ005":
			ids = append(ids, "ou_105")
		case "GZ009":
			ids = append(ids, "ou_109")
		}
	}
	return ids
}

// ownerReportResponse 构造一个成功的 owner 报表响应。headers 用 Moka 侧列标题
// （「申请」+ 默认邮箱列「简历接收邮箱」），dataIndex 用 app/email 便于断言。
// 每个 row 是 {"app": applicationId, "email": HR 邮箱}。
func ownerReportResponse(emailTitle string, rows ...map[string]string) string {
	headers := []map[string]string{
		{"dataIndex": "app", "title": config.ReportSourceApplicationTitle, "type": "text"},
		{"dataIndex": "email", "title": emailTitle, "type": "text"},
	}
	b, _ := json.Marshal(map[string]any{
		"code": 200, "msg": "success",
		"data": map[string]any{"headers": headers, "rows": rows},
	})
	return string(b)
}

// newOwnerReportServer 启动一个只应答 getReportData 的 Moka 替身，返回其 URL。
func newOwnerReportServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != reportDataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newOwnerMokaClient 构造指向替身服务的 Moka 客户端。
func newOwnerMokaClient(baseURL string) *mokabackend.Client {
	return mokabackend.NewClient(baseURL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
}

// TestLoadOwnerMap_BuildsApplicationToEmployeeNo 验证正常路径：报表行按「申请」列与
// 邮箱列建映射，HR 邮箱经 owner_email_mapping 转为工号。
func TestLoadOwnerMap_BuildsApplicationToEmployeeNo(t *testing.T) {
	url := newOwnerReportServer(t, ownerReportResponse(
		"简历接收邮箱",
		map[string]string{"app": "101", "email": "hr1@x.com"},
		map[string]string{"app": "102", "email": "hr2@x.com"},
	))
	cfg := &config.Config{
		MokaBaseURL:       url,
		OwnerReportID:     2001,
		OwnerEmailColumn:  "简历接收邮箱",
		OwnerEmailMapping: map[string]string{"hr1@x.com": "GZ001", "hr2@x.com": "GZ002"},
	}

	got := loadOwnerMap(context.Background(), newOwnerMokaClient(url), cfg)

	if len(got) != 2 {
		t.Fatalf("期望 2 条映射, got %d (%v)", len(got), got)
	}
	if got["101"] != "GZ001" {
		t.Fatalf("applicationId=101 工号: got=%q want=GZ001", got["101"])
	}
	if got["102"] != "GZ002" {
		t.Fatalf("applicationId=102 工号: got=%q want=GZ002", got["102"])
	}
}

// TestLoadOwnerMap_EmailNormalization 验证邮箱归一化：报表值带大小写与前后空格时，
// 仍能命中 owner_email_mapping（配置 key 在 loadSysConfig 侧已归一化，此处直接给小写 key）。
// 同时验证未配映射的邮箱被跳过（降级，不阻断其余行）。
func TestLoadOwnerMap_EmailNormalization(t *testing.T) {
	url := newOwnerReportServer(t, ownerReportResponse(
		"简历接收邮箱",
		map[string]string{"app": "201", "email": "  HR1@X.CoM  "}, // 大小写 + 空格 → 应归一化命中
		map[string]string{"app": "202", "email": "unknown@x.com"}, // 未配映射 → 跳过
		map[string]string{"app": "203", "email": ""},              // 邮箱为空 → 跳过
	))
	cfg := &config.Config{
		MokaBaseURL:       url,
		OwnerReportID:     2001,
		OwnerEmailColumn:  "简历接收邮箱",
		OwnerEmailMapping: map[string]string{"hr1@x.com": "GZ001"},
	}

	got := loadOwnerMap(context.Background(), newOwnerMokaClient(url), cfg)

	if len(got) != 1 {
		t.Fatalf("期望 1 条映射（仅归一化命中的那条）, got %d (%v)", len(got), got)
	}
	if got["201"] != "GZ001" {
		t.Fatalf("大小写/空格邮箱应归一化命中: got=%q want=GZ001", got["201"])
	}
}

// TestLoadOwnerMap_MissingColumnsDegrade 验证缺列降级：报表缺「申请」列或缺配置的 HR 邮箱列时
// 返回空 map（记 warning），不阻断轮询。
func TestLoadOwnerMap_MissingColumnsDegrade(t *testing.T) {
	// 缺邮箱列：报表邮箱列标题与 owner_email_column 配置不一致。
	t.Run("缺HR邮箱列", func(t *testing.T) {
		url := newOwnerReportServer(t, ownerReportResponse(
			"另一个邮箱列", map[string]string{"app": "301", "email": "hr1@x.com"},
		))
		cfg := &config.Config{
			MokaBaseURL:       url,
			OwnerReportID:     2001,
			OwnerEmailColumn:  "简历接收邮箱", // 报表中不存在
			OwnerEmailMapping: map[string]string{"hr1@x.com": "GZ001"},
		}
		if got := loadOwnerMap(context.Background(), newOwnerMokaClient(url), cfg); len(got) != 0 {
			t.Fatalf("缺邮箱列应降级返回空 map, got %v", got)
		}
	})

	// 缺「申请」列：headers 中没有 join 键列。
	t.Run("缺申请列", func(t *testing.T) {
		headers := []map[string]string{{"dataIndex": "email", "title": "简历接收邮箱", "type": "text"}}
		b, _ := json.Marshal(map[string]any{
			"code": 200, "msg": "success",
			"data": map[string]any{"headers": headers, "rows": []map[string]string{{"email": "hr1@x.com"}}},
		})
		url := newOwnerReportServer(t, string(b))
		cfg := &config.Config{
			MokaBaseURL:       url,
			OwnerReportID:     2001,
			OwnerEmailColumn:  "简历接收邮箱",
			OwnerEmailMapping: map[string]string{"hr1@x.com": "GZ001"},
		}
		if got := loadOwnerMap(context.Background(), newOwnerMokaClient(url), cfg); len(got) != 0 {
			t.Fatalf("缺「申请」列应降级返回空 map, got %v", got)
		}
	})
}

// TestLoadOwnerMap_SkipsWhenUnconfigured 验证 owner_report_id 未配置或 owner_email_mapping 为空时
// 直接返回空 map，且**不发起任何网络调用**（替身对任何请求返回 500，被调用即失败可见）。
func TestLoadOwnerMap_SkipsWhenUnconfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("未配置时不应发起 GetReportData 调用")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	client := newOwnerMokaClient(srv.URL)

	t.Run("owner_report_id未配置", func(t *testing.T) {
		cfg := &config.Config{
			MokaBaseURL:       srv.URL,
			OwnerEmailColumn:  "简历接收邮箱",
			OwnerEmailMapping: map[string]string{"hr1@x.com": "GZ001"},
		}
		if got := loadOwnerMap(context.Background(), client, cfg); len(got) != 0 {
			t.Fatalf("owner_report_id 未配置应返回空 map, got %v", got)
		}
	})

	t.Run("owner_email_mapping未配置", func(t *testing.T) {
		cfg := &config.Config{
			MokaBaseURL:      srv.URL,
			OwnerReportID:    2001,
			OwnerEmailColumn: "简历接收邮箱",
		}
		if got := loadOwnerMap(context.Background(), client, cfg); len(got) != 0 {
			t.Fatalf("owner_email_mapping 未配置应返回空 map, got %v", got)
		}
	})
}

// TestCollectOwnerBackfill_OnlyFillsEmpty 验证回填「只补空、不覆写」：
//   - 归属者列为空且 ownerMap 命中 → 收集 UpdateOp（补空）；
//   - 归属者列已有值（人员列回读为姓名）→ 跳过，即使报表仍返回该 applicationId（不覆写）；
//   - 归属者列为空但 ownerMap 未命中 → 跳过（报表尚未收录）；
//   - applicationId 列为空的行 → 跳过。
func TestCollectOwnerBackfill_OnlyFillsEmpty(t *testing.T) {
	const appCol, ownerCol = "applicationId", "简历归属者"
	records := map[string]lark.ExistingRecord{
		"rec1": {Fields: lark.Row{appCol: "101", ownerCol: ""}},    // 空 + 命中 → 补
		"rec2": {Fields: lark.Row{appCol: "102", ownerCol: "张三"}},  // 已写 → 不覆写
		"rec3": {Fields: lark.Row{appCol: "103", ownerCol: ""}},    // 空但报表未收录 → 跳过
		"rec4": {Fields: lark.Row{appCol: "", ownerCol: ""}},       // 无 applicationId → 跳过
		"rec5": {Fields: lark.Row{appCol: "105", ownerCol: "   "}}, // 只有空白 → 视为未写, 命中则补
	}
	ownerMap := map[string]string{"101": "GZ001", "102": "GZ002", "105": "GZ005"}

	updates := collectOwnerBackfill(records, ownerMap, appCol, ownerCol, fakeOwnerResolver)

	if len(updates) != 2 {
		t.Fatalf("期望 2 条回填（rec1/rec5）, got %d (%v)", len(updates), updates)
	}
	// 回填的归属者经 Persons 旁路写入（人员列不进 Fields），值与工号一一对应。
	got := make(map[string]string, len(updates))
	for _, u := range updates {
		if ids := u.Persons[ownerCol]; len(ids) == 1 {
			got[u.RecordID] = ids[0]
		}
	}
	if got["rec1"] != "ou_101" {
		t.Fatalf("rec1 应补空为 ou_101, got=%q", got["rec1"])
	}
	if got["rec5"] != "ou_105" {
		t.Fatalf("rec5（空白视为未写）应补为 ou_105, got=%q", got["rec5"])
	}
	if _, ok := got["rec2"]; ok {
		t.Fatalf("rec2 已写归属者, 不应被覆写: %v", updates)
	}
	if _, ok := got["rec3"]; ok {
		t.Fatalf("rec3 报表未收录, 不应回填: %v", updates)
	}
	if _, ok := got["rec4"]; ok {
		t.Fatalf("rec4 无 applicationId, 不应回填: %v", updates)
	}
}

// TestCollectOwnerBackfill_NormalizesApplicationID 验证回填侧 applicationId 走 normalizeID 归一化，
// 兼容 Bitable 数字列回读的科学计数法（与插入侧索引口径一致）。
func TestCollectOwnerBackfill_NormalizesApplicationID(t *testing.T) {
	const appCol, ownerCol = "applicationId", "简历归属者"
	records := map[string]lark.ExistingRecord{
		"rec1": {Fields: lark.Row{appCol: "1.01e+02", ownerCol: ""}}, // 科学计数法 → 归一化为 101
	}
	ownerMap := map[string]string{"101": "GZ001"}

	updates := collectOwnerBackfill(records, ownerMap, appCol, ownerCol, fakeOwnerResolver)

	if len(updates) != 1 || len(updates[0].Persons[ownerCol]) != 1 || updates[0].Persons[ownerCol][0] != "ou_101" {
		t.Fatalf("科学计数法 applicationId 应归一化后命中回填, got %v", updates)
	}
}
