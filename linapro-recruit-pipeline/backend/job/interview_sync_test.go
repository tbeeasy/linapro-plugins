// interview_sync_test.go 验证需求2「面试状态同步」重构后（FB-7）的关键行为：
//   - 组合键工具（compositeKey/normalizeID/buildCompositeIndex）按 applicationId+轮次 建索引，
//     并兼容 Bitable 数字列回读的科学计数法。
//   - syncUnarchived 路径一：未归档候选人逐面试轮次 upsert，已应约视频面试补视频链接，
//     未应约取归档原因；命中组合键更新、未命中新建。
//   - syncArchived 路径二：已归档候选人仅更新是否应约 + 未应约原因，无匹配行时跳过。
//   - beijingYesterdayRange 生成北京时间「昨日0点~当前」范围。
//
// 测试自包含且顺序无关：每个测试自行构造 Moka HTTP 替身与配置，不依赖共享状态。
package job

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
)

// defaultTestColumns 返回测试用的面试状态表列名集合。
func defaultTestColumns() interviewColumns {
	return interviewColumns{
		applicationID:  "申请ID",
		interviewRound: "面试轮次",
		candidateName:  "姓名",
		interviewType:  "面试方式",
		interviewTime:  "面试时间",
		interviewer:    "面试官",
		videoURL:       "视频面试链接",
		attendResult:   "是否应约",
		unattendReason: "未应约原因",
	}
}

// testInterviewerResolver 是「多工号 → open_id 集合」解析替身（empcap 复数版的替身）：
// 每个工号映射到 ou_<工号>，未传工号返回空集合。用于验证面试官人员列经 Persons 旁路写入。
func testInterviewerResolver(employeeNos []string) []string {
	ids := make([]string, 0, len(employeeNos))
	for _, no := range employeeNos {
		if no != "" {
			ids = append(ids, "ou_"+no)
		}
	}
	return ids
}

// TestNormalizeID 验证 applicationId 值归一化：整数字符串、科学计数法、空值。
func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"425436988":      "425436988",
		"4.25436988e+08": "425436988",
		"  123  ":        "123",
		"":               "",
		"abc":            "abc",
	}
	for in, want := range cases {
		if got := normalizeID(in); got != want {
			t.Errorf("normalizeID(%q)=%q want %q", in, got, want)
		}
	}
}

// TestCompositeKey 验证组合键对 applicationId 数值归一化、对轮次去空白后一致。
func TestCompositeKey(t *testing.T) {
	k1 := compositeKey("425436988", "初试")
	k2 := compositeKey("4.25436988e+08", " 初试 ")
	if k1 != k2 {
		t.Fatalf("组合键应一致: %q vs %q", k1, k2)
	}
}

// TestBuildExistingRows_CompositeKey 验证按 applicationId+轮次 组合键投影现有记录：
// applicationId 科学计数法归一化、applicationId/轮次均空的行被跳过、人员列 open_id 集合旁路携带。
func TestBuildExistingRows_CompositeKey(t *testing.T) {
	existing := map[string]lark.ExistingRecord{
		"rec1": {Fields: lark.Row{"申请ID": "425436988", "面试轮次": "初试"}},
		"rec2": {Fields: lark.Row{"申请ID": "4.25436988e+08", "面试轮次": "复试"}},
		"rec3": {Fields: lark.Row{"申请ID": "", "面试轮次": ""}}, // 应跳过
	}
	keyOf := func(f lark.Row) string {
		k := compositeKey(f["申请ID"], f["面试轮次"])
		if k == compositeKeySep {
			return ""
		}
		return k
	}
	rows := buildExistingRows(existing, keyOf)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[compositeKey("425436988", "初试")].recordID != "rec1" {
		t.Errorf("初试行索引错误: %v", rows)
	}
	if rows[compositeKey("425436988", "复试")].recordID != "rec2" {
		t.Errorf("复试行索引错误(科学计数法归一化失败): %v", rows)
	}
}

// TestBeijingYesterdayRange 验证时间范围以北京时间昨日0点为下界、当前时刻为上界，
// 并序列化为 UTC 的 Zulu 格式（末尾字面量 Z），这是 Moka 服务端唯一接受的写法。
func TestBeijingYesterdayRange(t *testing.T) {
	now := time.Date(2026, 8, 27, 10, 30, 0, 0, time.UTC)
	startTime := yesterdayStartInBeijing(now)
	start, end := formatMokaTimeRange(startTime, now)
	// 北京时间昨日 0 点 2026-08-26T00:00+08:00 == UTC 2026-08-25T16:00:00Z。
	if start != "2026-08-25T16:00:00.000Z" {
		t.Fatalf("start: got %q want 2026-08-25T16:00:00.000Z", start)
	}
	// end 为 now（UTC 10:30），直接以 Z 结尾。
	if end != "2026-08-27T10:30:00.000Z" {
		t.Fatalf("end: got %q want 2026-08-27T10:30:00.000Z", end)
	}
}

// mokaEhrServer 构造一个 Moka HTTP 替身，按 archived 请求参数返回不同的 ehrApplications 响应，
// 并对 interview-information 返回给定的视频链接响应。captureVideo 捕获视频接口请求体。
func mokaEhrServer(t *testing.T, unarchivedResp, archivedResp, videoResp string, captureVideo *string) *mokabackend.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api-platform/v2/data/ehrApplications":
			if strings.Contains(string(body), `"archived":true`) {
				_, _ = w.Write([]byte(archivedResp))
			} else {
				_, _ = w.Write([]byte(unarchivedResp))
			}
		case "/api-platform/v1/interview/interview-information":
			if captureVideo != nil {
				*captureVideo = string(body)
			}
			_, _ = w.Write([]byte(videoResp))
		default:
			t.Errorf("unexpected moka path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return mokabackend.NewClient(srv.URL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))
}

// TestSyncUnarchived_CreatesAndUpdatesWithVideoAndReason 验证路径一：
//   - 已命中组合键的轮次走 update；未命中的走 create（补齐 applicationId+轮次列）。
//   - 已应约视频面试且 interviewInfo 内链接为空时，调 interview-information 补视频链接。
//   - 未应约轮次写入归档原因作为未应约原因。
func TestSyncUnarchived_CreatesAndUpdatesWithVideoAndReason(t *testing.T) {
	// 候选人 A(123)：初试=已应约视频面试(链接空,需补)；复试=已取消(未应约)。
	// 候选人 B(456)：初试=已应约现场面试(不调视频接口)。
	const unarchived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":123,"name":"张三","archived":false,"archiveReasons":"候选人时间冲突"},
		 "interviewInfo":[
			{"round":1,"roundName":"初试","interviewType":"视频面试","status":"未结束","startTime":1706522400000,"intervieweeVideoUrl":"","interviewerFeedbacks":[{"interviewer":{"name":"面试官甲","employeeId":"GZ000060"}}]},
			{"round":2,"roundName":"复试","interviewType":"现场面试","status":"已取消","startTime":1706608800000,"intervieweeVideoUrl":"","interviewerFeedbacks":[{"interviewer":{"name":"面试官乙","employeeId":"GZ000061"}}]}
		 ]},
		{"basicInfo":{"applicationId":456,"name":"李四","archived":false},
		 "interviewInfo":[
			{"round":1,"roundName":"初试","interviewType":"现场面试","status":"已结束","startTime":1706522400000,"intervieweeVideoUrl":"","interviewerFeedbacks":[{"interviewer":{"name":"面试官丙","employeeId":"GZ000070"}},{"interviewer":{"name":"面试官丁","employeeId":"GZ000071"}}]}
		 ]}
	]}`
	const videoResp = `{"code":0,"success":true,"msg":"成功","data":[
		{"applicationId":123,"entities":[{"id":9,"round":1,"roundName":"初试","startTime":1706522400000,"intervieweeVideoUrl":"https://v.example.com/a"}]}
	]}`
	var gotVideoBody string
	mc := mokaEhrServer(t, unarchived, `{"code":200,"msg":"success","data":[]}`, videoResp, &gotVideoBody)

	cfg := &config.Config{InterviewStageID: 105021, MokaOperatorEmail: "admin@example.com"}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 现有行：A 的初试已存在（业务列为空，故走 update 且各列因与空值不同全部纳入变更），
	// 其余未命中（走 create）。
	existingRows := map[string]existingRow{
		compositeKey("123", "初试"): {recordID: "recA1", fields: lark.Row{"申请ID": "123", "面试轮次": "初试"}},
	}

	creates, updates, _, err := syncUnarchived(context.Background(), cfg, mc, existingRows, cols, keyCols, map[string]int{}, testInterviewerResolver)
	if err != nil {
		t.Fatalf("syncUnarchived: %v", err)
	}

	// 视频接口只应查 applicationId 123（B 是现场面试，A 复试未应约）。
	if !strings.Contains(gotVideoBody, `"applicationIds":[123]`) {
		t.Fatalf("interview-information 请求体应只含 123: %q", gotVideoBody)
	}
	if !strings.Contains(gotVideoBody, `"email":"admin@example.com"`) {
		t.Fatalf("interview-information 请求体应含 operatorEmail: %q", gotVideoBody)
	}

	// update：A 的初试。补齐了视频链接、已应约。
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	u := updates[0]
	if u.RecordID != "recA1" {
		t.Fatalf("update recordID: got %q want recA1", u.RecordID)
	}
	if u.Fields["是否应约"] != attendResultYes {
		t.Errorf("A初试应为已应约: %q", u.Fields["是否应约"])
	}
	if u.Fields["视频面试链接"] != "https://v.example.com/a" {
		t.Errorf("A初试视频链接应补齐: %q", u.Fields["视频面试链接"])
	}
	if u.Fields["面试方式"] != "视频面试" {
		t.Errorf("A初试面试方式: %q", u.Fields["面试方式"])
	}
	if u.Fields["姓名"] != "张三" {
		t.Errorf("A初试候选人姓名应回写: %q", u.Fields["姓名"])
	}
	// 面试官是人员列：不进 Fields，而是经 Persons 旁路携带已解析的 open_id 集合。
	if _, ok := u.Fields["面试官"]; ok {
		t.Errorf("面试官人员列不应进入 Fields: %v", u.Fields)
	}
	if ids := u.Persons["面试官"]; len(ids) != 1 || ids[0] != "ou_GZ000060" {
		t.Errorf("A初试面试官应经 Persons 旁路写入 open_id: %v", u.Persons)
	}

	// create：A 复试（未应约，带归档原因）、B 初试（已应约现场）。共 2 条。
	if len(creates) != 2 {
		t.Fatalf("expected 2 creates, got %d", len(creates))
	}
	byRound := map[string]lark.WriteRow{}
	personsByRound := map[string]map[string][]string{}
	for _, c := range creates {
		key := asString(c.Fields["申请ID"]) + "/" + asString(c.Fields["面试轮次"])
		byRound[key] = c.Fields
		personsByRound[key] = c.Persons
	}
	aFushi := byRound["123/复试"]
	if aFushi == nil {
		t.Fatalf("缺少 A 复试 create 行: %+v", creates)
	}
	if aFushi["是否应约"] != attendResultNo {
		t.Errorf("A复试应为未应约: %q", aFushi["是否应约"])
	}
	if aFushi["未应约原因"] != "候选人时间冲突" {
		t.Errorf("A复试未应约原因应取归档原因: %q", aFushi["未应约原因"])
	}
	bChushi := byRound["456/初试"]
	if bChushi == nil {
		t.Fatalf("缺少 B 初试 create 行: %+v", creates)
	}
	if bChushi["是否应约"] != attendResultYes {
		t.Errorf("B初试应为已应约: %q", bChushi["是否应约"])
	}
	if bChushi["姓名"] != "李四" {
		t.Errorf("B初试候选人姓名应回写: %q", bChushi["姓名"])
	}
	// 多面试官：两个工号解析出的 open_id 集合经 Persons 旁路写入同一多人字段。
	if ids := personsByRound["456/初试"]["面试官"]; len(ids) != 2 || ids[0] != "ou_GZ000070" || ids[1] != "ou_GZ000071" {
		t.Errorf("B初试多面试官应经 Persons 旁路写入 open_id 集合: %v", personsByRound["456/初试"])
	}
}

// TestSyncUnarchived_NonVideoDoesNotCallVideoAPI 验证已应约但非视频面试时不调用视频接口。
func TestSyncUnarchived_NonVideoDoesNotCallVideoAPI(t *testing.T) {
	const unarchived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":789,"name":"王五","archived":false},
		 "interviewInfo":[{"round":1,"roundName":"初试","interviewType":"电话面试","status":"已结束","startTime":1706522400000,"intervieweeVideoUrl":""}]}
	]}`
	videoCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api-platform/v1/interview/interview-information" {
			videoCalled = true
		}
		if r.URL.Path == "/api-platform/v2/data/ehrApplications" {
			_, _ = w.Write([]byte(unarchived))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	mc := mokabackend.NewClient(srv.URL, mokabackend.NewBasicAuth(mokabackend.BasicCredential{APIKey: "k"}))

	cfg := &config.Config{InterviewStageID: 105021, MokaOperatorEmail: "admin@example.com"}
	cols := defaultTestColumns()
	creates, _, _, err := syncUnarchived(context.Background(), cfg, mc, map[string]existingRow{}, cols, []string{cols.applicationID, cols.interviewRound}, map[string]int{}, testInterviewerResolver)
	if err != nil {
		t.Fatalf("syncUnarchived: %v", err)
	}
	if videoCalled {
		t.Fatal("电话面试不应调用 interview-information")
	}
	if len(creates) != 1 || creates[0].Fields["视频面试链接"] != "" {
		t.Fatalf("电话面试视频链接应为空: %+v", creates)
	}
}

// TestSyncArchived_OnlyUpdatesAttendAndReason 验证路径二：已归档候选人仅更新是否应约+未应约原因，
// 命中组合键才更新，无匹配行跳过，且不产生新建行。
func TestSyncArchived_OnlyUpdatesAttendAndReason(t *testing.T) {
	const archived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":123,"name":"张三","archived":true,"archiveReasons":{"name":"候选人放弃"}},
		 "interviewInfo":[
			{"round":1,"roundName":"初试","interviewType":"视频面试","status":"已取消","startTime":1706522400000,"intervieweeVideoUrl":""},
			{"round":2,"roundName":"终试","interviewType":"现场面试","status":"已结束","startTime":1706608800000,"intervieweeVideoUrl":""}
		 ]}
	]}`
	mc := mokaEhrServer(t, `{"code":200,"msg":"success","data":[]}`, archived, `{"code":0,"data":[]}`, nil)

	cfg := &config.Config{InterviewStageID: 105021}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 只有初试有匹配行（业务列为空，故变更列纳入更新）；终试无匹配行应跳过、不新建。
	existingRows := map[string]existingRow{
		compositeKey("123", "初试"): {recordID: "recArch1", fields: lark.Row{"申请ID": "123", "面试轮次": "初试"}},
	}

	updates, _, err := syncArchived(context.Background(), cfg, mc, existingRows, cols, keyCols, map[string]int{})
	if err != nil {
		t.Fatalf("syncArchived: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update (只有初试匹配), got %d", len(updates))
	}
	u := updates[0]
	if u.RecordID != "recArch1" {
		t.Fatalf("recordID: got %q want recArch1", u.RecordID)
	}
	if u.Fields["是否应约"] != attendResultNo {
		t.Errorf("初试已取消应为未应约: %q", u.Fields["是否应约"])
	}
	if u.Fields["未应约原因"] != "候选人放弃" {
		t.Errorf("未应约原因应取归档原因对象的 name: %q", u.Fields["未应约原因"])
	}
	// 路径二不应写入面试方式/时间/视频链接列。
	if _, ok := u.Fields["面试方式"]; ok {
		t.Errorf("路径二不应更新面试方式列: %+v", u.Fields)
	}
	if _, ok := u.Fields["视频面试链接"]; ok {
		t.Errorf("路径二不应更新视频面试链接列: %+v", u.Fields)
	}
}

// TestSyncUnarchived_NoChangeFreezes 验证路径一命中现有行且全部列（含日期、姓名、是否应约、
// 视频链接、未应约原因）经比对均相等时冻结，不产出更新。
func TestSyncUnarchived_NoChangeFreezes(t *testing.T) {
	const unarchived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":456,"name":"李四","archived":false},
		 "interviewInfo":[{"round":1,"roundName":"初试","interviewType":"现场面试","status":"已结束","startTime":1706522400000,"intervieweeVideoUrl":""}]}
	]}`
	mc := mokaEhrServer(t, unarchived, `{"code":200,"msg":"success","data":[]}`, `{"code":0,"data":[]}`, nil)

	cfg := &config.Config{InterviewStageID: 105021, MokaOperatorEmail: "admin@example.com"}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 现有行与本轮 desired 完全一致：面试时间以科学计数法回读（飞书数字型日期）。
	existingRows := map[string]existingRow{
		compositeKey("456", "初试"): {recordID: "recB1", fields: lark.Row{
			"申请ID": "456", "面试轮次": "初试", "面试方式": "现场面试",
			"面试时间": "1.7065224e+12", "是否应约": attendResultYes, "姓名": "李四",
			"视频面试链接": "", "未应约原因": "",
		}},
	}
	fieldTypes := map[string]int{"面试时间": larkbitablesdk.TypeDateTime}

	creates, updates, frozen, err := syncUnarchived(context.Background(), cfg, mc, existingRows, cols, keyCols, fieldTypes, testInterviewerResolver)
	if err != nil {
		t.Fatalf("syncUnarchived: %v", err)
	}
	if len(creates) != 0 || len(updates) != 0 || frozen != 1 {
		t.Fatalf("全列相等（含日期科学计数法等价）应冻结, got creates=%d updates=%d frozen=%d", len(creates), len(updates), frozen)
	}
}

// TestSyncUnarchived_ClearsUnattendReasonOnAttend 验证轮次由未应约转已应约时未应约原因被
// 无条件写为空串（显式清列），作为差异列纳入更新。
func TestSyncUnarchived_ClearsUnattendReasonOnAttend(t *testing.T) {
	const unarchived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":456,"name":"李四","archived":false},
		 "interviewInfo":[{"round":1,"roundName":"初试","interviewType":"现场面试","status":"已结束","startTime":1706522400000,"intervieweeVideoUrl":""}]}
	]}`
	mc := mokaEhrServer(t, unarchived, `{"code":200,"msg":"success","data":[]}`, `{"code":0,"data":[]}`, nil)

	cfg := &config.Config{InterviewStageID: 105021, MokaOperatorEmail: "admin@example.com"}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 现有行除未应约原因残留旧值外其余均一致 → 唯一差异列应为未应约原因（清空）。
	existingRows := map[string]existingRow{
		compositeKey("456", "初试"): {recordID: "recB1", fields: lark.Row{
			"申请ID": "456", "面试轮次": "初试", "面试方式": "现场面试",
			"面试时间": "1706522400000", "是否应约": attendResultYes, "姓名": "李四",
			"视频面试链接": "", "未应约原因": "候选人放弃",
		}},
	}
	fieldTypes := map[string]int{"面试时间": larkbitablesdk.TypeDateTime}

	_, updates, _, err := syncUnarchived(context.Background(), cfg, mc, existingRows, cols, keyCols, fieldTypes, testInterviewerResolver)
	if err != nil {
		t.Fatalf("syncUnarchived: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("应产出 1 条更新（清空未应约原因）, got %d", len(updates))
	}
	if len(updates[0].Fields) != 1 {
		t.Fatalf("更新应只含未应约原因一列, got=%v", updates[0].Fields)
	}
	if v, ok := updates[0].Fields["未应约原因"]; !ok || v != "" {
		t.Fatalf("未应约原因应被清空为空串, got=%v ok=%v", v, ok)
	}
}

// TestSyncUnarchived_InterviewerSetEquivalentFreezes 验证面试官人员列 open_id 集合等价
// （顺序无关、去重）时冻结，不产出人员列更新。
func TestSyncUnarchived_InterviewerSetEquivalentFreezes(t *testing.T) {
	const unarchived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":456,"name":"李四","archived":false},
		 "interviewInfo":[{"round":1,"roundName":"初试","interviewType":"现场面试","status":"已结束","startTime":1706522400000,"intervieweeVideoUrl":"","interviewerFeedbacks":[{"interviewer":{"name":"甲","employeeId":"GZ70"}},{"interviewer":{"name":"乙","employeeId":"GZ71"}}]}]}
	]}`
	mc := mokaEhrServer(t, unarchived, `{"code":200,"msg":"success","data":[]}`, `{"code":0,"data":[]}`, nil)

	cfg := &config.Config{InterviewStageID: 105021, MokaOperatorEmail: "admin@example.com"}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 现有行业务列全一致，人员列回读 open_id 集合与本轮期望等价（顺序相反）。
	existingRows := map[string]existingRow{
		compositeKey("456", "初试"): {
			recordID: "recB1",
			fields: lark.Row{
				"申请ID": "456", "面试轮次": "初试", "面试方式": "现场面试",
				"面试时间": "1706522400000", "是否应约": attendResultYes, "姓名": "李四",
				"视频面试链接": "", "未应约原因": "",
			},
			personIDs: map[string][]string{"面试官": {"ou_GZ71", "ou_GZ70"}},
		},
	}
	fieldTypes := map[string]int{"面试时间": larkbitablesdk.TypeDateTime}

	creates, updates, frozen, err := syncUnarchived(context.Background(), cfg, mc, existingRows, cols, keyCols, fieldTypes, testInterviewerResolver)
	if err != nil {
		t.Fatalf("syncUnarchived: %v", err)
	}
	if len(creates) != 0 || len(updates) != 0 || frozen != 1 {
		t.Fatalf("面试官 open_id 集合等价应冻结, got creates=%d updates=%d frozen=%d", len(creates), len(updates), frozen)
	}
}

// TestSyncArchived_FreezesWhenNoChange 验证路径二命中行两列均无变化时冻结、不产出更新。
func TestSyncArchived_FreezesWhenNoChange(t *testing.T) {
	const archived = `{"code":200,"msg":"success","data":[
		{"basicInfo":{"applicationId":123,"name":"张三","archived":true,"archiveReasons":{"name":"候选人放弃"}},
		 "interviewInfo":[{"round":1,"roundName":"初试","interviewType":"视频面试","status":"已取消","startTime":1706522400000,"intervieweeVideoUrl":""}]}
	]}`
	mc := mokaEhrServer(t, `{"code":200,"msg":"success","data":[]}`, archived, `{"code":0,"data":[]}`, nil)

	cfg := &config.Config{InterviewStageID: 105021}
	cols := defaultTestColumns()
	keyCols := []string{cols.applicationID, cols.interviewRound}
	// 现有行是否应约/未应约原因已与本轮一致 → 冻结。
	existingRows := map[string]existingRow{
		compositeKey("123", "初试"): {recordID: "recArch1", fields: lark.Row{
			"申请ID": "123", "面试轮次": "初试", "是否应约": attendResultNo, "未应约原因": "候选人放弃",
		}},
	}

	updates, frozen, err := syncArchived(context.Background(), cfg, mc, existingRows, cols, keyCols, map[string]int{})
	if err != nil {
		t.Fatalf("syncArchived: %v", err)
	}
	if len(updates) != 0 || frozen != 1 {
		t.Fatalf("两列均无变化应冻结, got updates=%d frozen=%d", len(updates), frozen)
	}
}

// TestRunInterviewSync_MissingKeyColumns 验证 field_mapping 缺少 applicationId/轮次列时报错。
func TestRunInterviewSync_MissingKeyColumns(t *testing.T) {
	cfg := &config.Config{
		InterviewStageID: 105021,
		FieldMapping:     map[string]string{"name": "候选人"}, // 缺 applicationId / interview_round
	}
	err := RunInterviewSync(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("缺少组合键列应返回 error")
	}
	if !strings.Contains(err.Error(), "applicationId") {
		t.Fatalf("error 应提示缺少组合键列: %v", err)
	}
}

// TestBuildInterviewerPersons 验证面试官人员列旁路的构造：多工号经 empcap 复数版解析为
// 去重 open_id 集合后写入 Persons 旁路（人员列不进 Row）；未配置列/无工号/解析器缺失/
// 解析不到 open_id 时返回 nil（该列本轮跳过，不阻断其余列写入）。
func TestBuildInterviewerPersons(t *testing.T) {
	// 替身解析器：GZ000070→[o70]，GZ000071→[o71a,o71b]，重复工号与同人多身份应被去重。
	resolve := func(employeeNos []string) []string {
		seen := make(map[string]struct{})
		var ids []string
		for _, no := range employeeNos {
			var got []string
			switch no {
			case "GZ000070":
				got = []string{"o70"}
			case "GZ000071":
				got = []string{"o71a", "o71b"}
			case "GZ000060":
				got = []string{"o71a"} // 与 GZ000071 命中同一 open_id，应被去重
			}
			for _, id := range got {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	}

	// 多面试官：合并去重后写入 Persons 旁路。
	got := buildInterviewerPersons("面试官", []string{"GZ000070", "GZ000071", "GZ000060"}, resolve)
	want := []string{"o70", "o71a", "o71b"}
	ids, ok := got["面试官"]
	if !ok || len(ids) != len(want) {
		t.Fatalf("面试官列应写入去重后的 open_id 集合, got %v want %v", got, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("open_id 顺序/去重: got %v want %v", ids, want)
		}
	}

	// 降级路径：列未配置、无工号、解析器缺失、解析不到 open_id 均返回 nil（跳过该列）。
	if p := buildInterviewerPersons("", []string{"GZ000070"}, resolve); p != nil {
		t.Errorf("列未配置应返回 nil: %v", p)
	}
	if p := buildInterviewerPersons("面试官", nil, resolve); p != nil {
		t.Errorf("无工号应返回 nil: %v", p)
	}
	if p := buildInterviewerPersons("面试官", []string{"GZ000070"}, nil); p != nil {
		t.Errorf("解析器缺失应返回 nil: %v", p)
	}
	if p := buildInterviewerPersons("面试官", []string{"GZ000999"}, resolve); p != nil {
		t.Errorf("解析不到 open_id 应返回 nil: %v", p)
	}
}
