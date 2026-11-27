// 本文件用 httptest 打桩飞书开放平台，覆盖考勤客户端的请求构造、投影、退避重试与分批。
//
// 打桩方式：newClient 的 baseURL 参数把资源请求与换 token 请求都指向桩地址，
// 桩同时响应 /open-apis/auth/v3/tenant_access_token/internal 与各业务接口，
// 因此「退避重试 → 重新发请求」的整条链路都是真实 HTTP 行为，而不是假的调用计数器。
package larkattend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// okToken 是桩返回的 tenant_access_token 响应体。
const okToken = `{"code":0,"msg":"ok","tenant_access_token":"t-stub","expire":7200}`

// newStubServer 启动一个飞书桩，handler 负责除换 token 外的所有请求。
// 返回的 Client 已注入「不真正等待」的 sleep，使退避重试在测试里瞬间完成。
func newStubServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/open-apis/auth/v3/tenant_access_token/internal") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(okToken))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	client := newClient("cli_stub", "secret_stub", server.URL)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

// writeJSON 写一个 JSON 响应。
func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("写入桩响应失败：%v", err)
	}
}

// TestListGroupsProjectsAndPaginates 校验列表接口的投影与自动翻页。
func TestListGroupsProjectsAndPaginates(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/attendance/v1/groups" {
			t.Errorf("路径不符合预期：%s", r.URL.Path)
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"group_list":[{"group_id":"7647","group_name":"客服组"}],"has_more":true,"page_token":"p2"}}`)
			return
		}
		if r.URL.Query().Get("page_token") != "p2" {
			t.Errorf("第二页未带 page_token，实际=%q", r.URL.Query().Get("page_token"))
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"group_list":[{"group_id":"7648","group_name":"综合部"}],"has_more":false}}`)
	})

	groups, err := client.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups 失败：%v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("期望 2 个考勤组，实际 %d：%+v", len(groups), groups)
	}
	if groups[0].GroupID != "7647" || groups[0].GroupName != "客服组" {
		t.Errorf("第一条投影不符合预期：%+v", groups[0])
	}
	if groups[1].GroupID != "7648" {
		t.Errorf("第二条投影不符合预期：%+v", groups[1])
	}
}

// TestGetGroupUsesOpenIDDeptType 校验考勤组详情固定传 dept_type=open_id。
// 传 od_id 时飞书返回 99992402，这里把它固化成契约。
func TestGetGroupUsesOpenIDDeptType(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("dept_type"); got != "open_id" {
			t.Errorf("dept_type 必须是 open_id，实际 %q", got)
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"group_id":"7647","group_name":"客服组","group_type":2,"punch_day_shift_ids":["11","12"],"allow_remedy":true,"remedy_limit_count":3,"remedy_date_num":5,"allow_out_punch":true,"out_punch_need_post_approval":true,"need_punch_members":[{"user_id":"u1"},{"user_id":"u2"}]}}`)
	})

	detail, err := client.GetGroup(context.Background(), "7647")
	if err != nil {
		t.Fatalf("GetGroup 失败：%v", err)
	}
	if detail.GroupType != 2 || !detail.AllowRemedy || detail.RemedyLimitCount != 3 {
		t.Errorf("考勤组投影不符合预期：%+v", detail)
	}
	if detail.NeedPunchMemberCount != 2 {
		t.Errorf("需打卡成员数应为 2，实际 %d", detail.NeedPunchMemberCount)
	}
}

// TestListShiftsParsesCrossDayAndRestRules 校验班次投影保留 27:00 原值、休息段与晚走晚到。
func TestListShiftsParsesCrossDayAndRestRules(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"shift_list":[{"shift_id":"9","shift_name":"夜班","is_flexible":true,"flexible_minutes":30,
			"punch_time_rule":[{"on_time":"19:30","off_time":"27:00","late_minutes_as_late":5,"late_minutes_as_lack":120,"late_minutes_as_serious_late":60,"early_minutes_as_early":5,"early_minutes_as_lack":120,"on_advance_minutes":60,"off_delay_minutes":120}],
			"rest_time_rule":[{"rest_begin_time":"23:00","rest_end_time":"24:00"}],
			"late_off_late_on_rule":[{"late_off_minutes":60,"late_on_minutes":30}]}],"has_more":false}}`)
	})

	shifts, err := client.ListShifts(context.Background())
	if err != nil {
		t.Fatalf("ListShifts 失败：%v", err)
	}
	if len(shifts) != 1 {
		t.Fatalf("期望 1 个班次，实际 %d", len(shifts))
	}
	shift := shifts[0]
	if shift.ShiftID != "9" || shift.ShiftName != "夜班" {
		t.Errorf("班次标识不符合预期：%+v", shift)
	}
	if !shift.IsFlexible || shift.FlexibleMinutes != 30 {
		t.Errorf("弹性字段不符合预期：%+v", shift)
	}
	if len(shift.PunchTimeRules) != 1 {
		t.Fatalf("期望 1 段打卡规则，实际 %d", len(shift.PunchTimeRules))
	}
	rule := shift.PunchTimeRules[0]
	if rule.OnTime != "19:30" || rule.OffTime != "27:00" {
		t.Errorf("跨天时间原值应原样保留，实际 on=%q off=%q", rule.OnTime, rule.OffTime)
	}
	if rule.OnAdvanceMinutes != 60 || rule.OffDelayMinutes != 120 {
		t.Errorf("打卡窗口偏置不符合预期：%+v", rule)
	}
	if len(shift.RestRules) != 1 || shift.RestRules[0].BeginTime != "23:00" || shift.RestRules[0].EndTime != "24:00" {
		t.Errorf("休息段不符合预期：%+v", shift.RestRules)
	}
	if len(shift.LateOffLateOnRules) != 1 || shift.LateOffLateOnRules[0].LateOffMinutes != 60 {
		t.Errorf("晚走晚到规则不符合预期：%+v", shift.LateOffLateOnRules)
	}
}

// TestUserTaskQuerySplitsBatchesAt50 校验 51 人被拆成 2 批，且每批都带 employee_type 与 ignore_invalid_users，
// 并把两批返回的无效 / 无权限人员合并到结果中。
func TestUserTaskQuerySplitsBatchesAt50(t *testing.T) {
	var batches []int
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("employee_type"); got != userEmployeeType {
			t.Errorf("employee_type 应为 %s，实际 %q", userEmployeeType, got)
		}
		if got := r.URL.Query().Get("ignore_invalid_users"); got != "true" {
			t.Errorf("ignore_invalid_users 应为 true，实际 %q", got)
		}
		var body struct {
			UserIds       []string `json:"user_ids"`
			CheckDateFrom int      `json:"check_date_from"`
			CheckDateTo   int      `json:"check_date_to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("解析请求体失败：%v", err)
		}
		batches = append(batches, len(body.UserIds))
		if body.CheckDateFrom != 20261101 || body.CheckDateTo != 20261130 {
			t.Errorf("日期参数不符合预期：%+v", body)
		}
		task := `{"user_id":"` + body.UserIds[0] + `","day":20261118,"group_id":"7647","shift_id":"9","records":[{"check_in_record_id":"r1","check_out_record_id":"r2","task_shift_type":0}]}`
		// 每批各报一个无效人员与一个无权限人员，校验结果按批合并。
		extra := `"invalid_user_ids":["bad` + strconv.Itoa(len(batches)) + `"],"unauthorized_user_ids":["deny` + strconv.Itoa(len(batches)) + `"]`
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"user_task_results":[`+task+`],`+extra+`}}`)
	})

	userIDs := make([]string, 51)
	for i := range userIDs {
		userIDs[i] = "u" + strings.Repeat("x", i%3) + string(rune('a'+i%26))
	}
	result, err := client.QueryUserTasks(context.Background(), userIDs, 20261101, 20261130)
	if err != nil {
		t.Fatalf("QueryUserTasks 失败：%v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("51 人应拆成 2 批，实际批次=%v", batches)
	}
	if batches[0] != 50 || batches[1] != 1 {
		t.Errorf("分批大小应为 50/1，实际 %v", batches)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("两批各返回 1 条，期望 2 条，实际 %d", len(result.Tasks))
	}
	if result.Tasks[0].Day != 20261118 || len(result.Tasks[0].Tasks) != 1 || result.Tasks[0].Tasks[0].CheckInRecordID != "r1" {
		t.Errorf("打卡结果投影不符合预期：%+v", result.Tasks[0])
	}
	if strings.Join(result.InvalidUserIDs, ",") != "bad1,bad2" {
		t.Errorf("无效人员应按批合并为 bad1,bad2，实际 %v", result.InvalidUserIDs)
	}
	if strings.Join(result.UnauthorizedUserIDs, ",") != "deny1,deny2" {
		t.Errorf("无权限人员应按批合并为 deny1,deny2，实际 %v", result.UnauthorizedUserIDs)
	}
}

// TestUserDailyShiftQuerySendsEmployeeType 校验排班查询带 employee_type=employee_id（飞书必填、SDK 无默认值），
// 且不带 ignore_invalid_users（SDK v3.11.0 该接口不支持）。
func TestUserDailyShiftQuerySendsEmployeeType(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/attendance/v1/user_daily_shifts/query" {
			t.Errorf("路径不符合预期：%s", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("employee_type"); got != userEmployeeType {
			t.Errorf("employee_type 应为 %s，实际 %q", userEmployeeType, got)
		}
		if query.Has("ignore_invalid_users") {
			t.Errorf("排班查询不应带 ignore_invalid_users，实际 query=%s", r.URL.RawQuery)
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"user_daily_shifts":[{"user_id":"u1","group_id":"7647","shift_id":"9","month":202611,"day_no":18}]}}`)
	})

	shifts, err := client.QueryUserDailyShifts(context.Background(), []string{"u1"}, 20261118, 20261118)
	if err != nil {
		t.Fatalf("QueryUserDailyShifts 失败：%v", err)
	}
	if len(shifts) != 1 || shifts[0].UserID != "u1" || shifts[0].ShiftID != "9" || shifts[0].DayNo != 18 {
		t.Errorf("排班投影不符合预期：%+v", shifts)
	}
}

// TestUserFlowQuerySendsEmployeeType 校验打卡流水查询带 employee_type=employee_id（飞书必填、SDK 无默认值），
// 且不带 ignore_invalid_users（SDK v3.11.0 该接口不支持）。
func TestUserFlowQuerySendsEmployeeType(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/attendance/v1/user_flows/query" {
			t.Errorf("路径不符合预期：%s", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("employee_type"); got != userEmployeeType {
			t.Errorf("employee_type 应为 %s，实际 %q", userEmployeeType, got)
		}
		if query.Has("ignore_invalid_users") {
			t.Errorf("打卡流水查询不应带 ignore_invalid_users，实际 query=%s", r.URL.RawQuery)
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"user_flow_results":[{"user_id":"u1","record_id":"r1","check_time":"1763427900","type":0,"is_field":true}]}}`)
	})

	var (
		from = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
		to   = from.Add(24 * time.Hour)
	)
	flows, err := client.QueryUserFlows(context.Background(), []string{"u1"}, from, to)
	if err != nil {
		t.Fatalf("QueryUserFlows 失败：%v", err)
	}
	if len(flows) != 1 || flows[0].RecordID != "r1" || !flows[0].IsField {
		t.Errorf("打卡流水投影不符合预期：%+v", flows)
	}
}

// TestUserTaskParsesEmbeddedFlowRecords 校验打卡结果内嵌的 check_in_record / check_out_record
// 按 UserFlow 原样投影；只有 id、没有对象的一端为 nil。
func TestUserTaskParsesEmbeddedFlowRecords(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/attendance/v1/user_tasks/query" {
			t.Errorf("路径不符合预期：%s", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"user_task_results":[{"user_id":"u1","day":20261118,"group_id":"7647","shift_id":"11",
			"records":[
				{"check_in_record_id":"r-in","check_in_record":{"user_id":"u1","creator_id":"u1","location_name":"上海市徐汇区","check_time":"1763427900","comment":"早到","record_id":"r-in","ssid":"office","bssid":"aa:bb","is_field":false,"is_wifi":true,"type":0,"photo_urls":["https://p/1.jpg"],"check_result":"Normal","latitude":31.18,"longitude":121.43},
				 "check_out_record_id":"r-out","check_out_record":{"user_id":"u1","record_id":"r-out","check_time":"1763460000","location_name":"客户现场","is_field":true,"type":2,"latitude":0,"longitude":0},
				 "check_in_result":"Normal","check_out_result":"Normal","task_shift_type":0},
				{"check_in_record_id":"r-in2","check_out_record_id":"","task_shift_type":1}
			]}]}}`)
	})

	result, err := client.QueryUserTasks(context.Background(), []string{"u1"}, 20261118, 20261118)
	if err != nil {
		t.Fatalf("QueryUserTasks 失败：%v", err)
	}
	if len(result.Tasks) != 1 || len(result.Tasks[0].Tasks) != 2 {
		t.Fatalf("期望 1 人 2 段，实际 %+v", result.Tasks)
	}

	first := result.Tasks[0].Tasks[0]
	in := first.CheckInRecord
	if in == nil {
		t.Fatal("上班卡完整流水不应为 nil")
	}
	wantIn := UserFlow{
		UserID: "u1", RecordID: "r-in", CheckTime: "1763427900", Type: 0,
		IsField: false, IsWifi: true, Ssid: "office", Bssid: "aa:bb",
		LocationName: "上海市徐汇区", Latitude: 31.18, Longitude: 121.43,
		PhotoUrls: []string{"https://p/1.jpg"}, Comment: "早到", CheckResult: "Normal",
	}
	if !reflect.DeepEqual(*in, wantIn) {
		t.Errorf("上班卡流水投影不符合预期：\n实际 %+v\n期望 %+v", *in, wantIn)
	}
	if in.RecordID != first.CheckInRecordID {
		t.Errorf("上班卡流水 record_id 应与 check_in_record_id 一致，实际 %q / %q", in.RecordID, first.CheckInRecordID)
	}

	out := first.CheckOutRecord
	if out == nil {
		t.Fatal("下班卡完整流水不应为 nil")
	}
	if out.RecordID != "r-out" || out.CheckTime != "1763460000" || !out.IsField || out.Type != 2 || out.LocationName != "客户现场" {
		t.Errorf("下班卡流水投影不符合预期：%+v", *out)
	}

	second := result.Tasks[0].Tasks[1]
	if second.CheckInRecordID != "r-in2" || second.TaskShiftType != 1 {
		t.Errorf("第二段标识不符合预期：%+v", second)
	}
	if second.CheckInRecord != nil || second.CheckOutRecord != nil {
		t.Errorf("响应缺少 check_in_record / check_out_record 时应为 nil，实际 in=%+v out=%+v", second.CheckInRecord, second.CheckOutRecord)
	}
}

// TestListGroupMembersQueryAndPaginates 校验考勤组成员查询的路径、固定参数、打卡类型筛选、
// 自动翻页与空 user_id 过滤。
func TestListGroupMembersQueryAndPaginates(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("应为 GET，实际 %s", r.Method)
		}
		if r.URL.Path != "/open-apis/attendance/v1/groups/7647/list_user" {
			t.Errorf("路径不符合预期：%s", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("employee_type"); got != "employee_id" {
			t.Errorf("employee_type 应为 employee_id，实际 %q", got)
		}
		if got := query.Get("dept_type"); got != "open_id" {
			t.Errorf("dept_type 应为 open_id，实际 %q", got)
		}
		if got := query.Get("member_clock_type"); got != "1" {
			t.Errorf("member_clock_type 应为 1，实际 %q", got)
		}
		if got := query.Get("page_size"); got != strconv.Itoa(groupMemberPageSize) {
			t.Errorf("page_size 应为 %d，实际 %q", groupMemberPageSize, got)
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			if query.Has("page_token") {
				t.Errorf("第一页不应带 page_token，实际 %q", query.Get("page_token"))
			}
			writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"users":[{"user_id":"u1","department_ids":["od-1"]},{"user_id":""},{"user_id":"u2"}],"has_more":true,"page_token":"p2"}}`)
			return
		}
		if got := query.Get("page_token"); got != "p2" {
			t.Errorf("第二页应带 page_token=p2，实际 %q", got)
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"users":[{"user_id":"u3"}],"has_more":false}}`)
	})

	userIDs, err := client.ListGroupMembers(context.Background(), "7647", MemberClockTypeNeedPunch)
	if err != nil {
		t.Fatalf("ListGroupMembers 失败：%v", err)
	}
	if strings.Join(userIDs, ",") != "u1,u2,u3" {
		t.Errorf("成员应跨页合并并过滤空 user_id，实际 %v", userIDs)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("期望翻 2 页，实际 %d 次请求", got)
	}
}

// TestListGroupMembersRetriesRateLimit 校验成员查询同样走 callWithBackoff：业务码限流后重试成功。
func TestListGroupMembersRetriesRateLimit(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(t, w, http.StatusOK, `{"code":99991400,"msg":"触发限流"}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"users":[{"user_id":"u1"}],"has_more":false}}`)
	})

	userIDs, err := client.ListGroupMembers(context.Background(), "7647", MemberClockTypeAll)
	if err != nil {
		t.Fatalf("限流后应重试成功，实际失败：%v", err)
	}
	if len(userIDs) != 1 || userIDs[0] != "u1" {
		t.Errorf("成员不符合预期：%v", userIDs)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("期望 2 次请求（1 次限流 + 1 次成功），实际 %d", got)
	}
}

// TestListGroupMembersErrorOnLaterPage 校验第二页返回不可重试错误码时整次失败、不返回半份名单，
// 且错误保留 action / code / req_id。
func TestListGroupMembersErrorOnLaterPage(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"users":[{"user_id":"u1"}],"has_more":true,"page_token":"p2"}}`)
			return
		}
		w.Header().Set("X-Tt-Logid", "log-456")
		writeJSON(t, w, http.StatusOK, `{"code":1220001,"msg":"group not found"}`)
	})

	userIDs, err := client.ListGroupMembers(context.Background(), "7647", MemberClockTypeNeedPunch)
	if err == nil {
		t.Fatalf("第二页失败应整次失败，实际返回 %v", userIDs)
	}
	if userIDs != nil {
		t.Errorf("失败时不应返回半份名单，实际 %v", userIDs)
	}
	var feishuErr *FeishuError
	if !errors.As(err, &feishuErr) {
		t.Fatalf("错误应为 *FeishuError，实际 %T：%v", err, err)
	}
	if feishuErr.Action != "attendance.groups.list_user" || feishuErr.Code != 1220001 || feishuErr.ReqID != "log-456" {
		t.Errorf("错误字段不符合预期：%+v", feishuErr)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("不可重试错误不应重试，期望 2 次请求，实际 %d", got)
	}
}

// TestListGroupMembersRejectsInvalidArgs 校验空考勤组 ID 与越界打卡类型在本地就被拒绝，不发起 HTTP 请求。
func TestListGroupMembersRejectsInvalidArgs(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("非法参数不应发起 HTTP 请求")
		writeJSON(t, w, http.StatusOK, `{"code":0}`)
	})
	if _, err := client.ListGroupMembers(context.Background(), "", MemberClockTypeNeedPunch); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("空考勤组 ID 期望 ErrInvalidArgument，实际 %v", err)
	}
	if _, err := client.ListGroupMembers(context.Background(), "7647", MemberClockType(3)); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("越界打卡类型期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestBusinessRateLimitRetriesThenSucceeds 校验 HTTP 200 + code=99991400 会退避重试并最终成功。
func TestBusinessRateLimitRetriesThenSucceeds(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(t, w, http.StatusOK, `{"code":99991400,"msg":"触发限流"}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"group_list":[{"group_id":"1","group_name":"A"}],"has_more":false}}`)
	})

	groups, err := client.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("限流后应重试成功，实际失败：%v", err)
	}
	if len(groups) != 1 {
		t.Errorf("期望 1 个考勤组，实际 %d", len(groups))
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("期望 2 次请求（1 次限流 + 1 次成功），实际 %d", got)
	}
}

// TestHTTP429Retries 校验 HTTP 429 由注入的 larkhttp 客户端退避重试后成功。
func TestHTTP429Retries(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("X-Ogw-Ratelimit-Reset", "0")
			writeJSON(t, w, http.StatusTooManyRequests, `{"code":99991400,"msg":"too many request"}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"code":0,"msg":"ok","data":{"shift_list":[{"shift_id":"9","shift_name":"早班"}],"has_more":false}}`)
	})

	shifts, err := client.ListShifts(context.Background())
	if err != nil {
		t.Fatalf("HTTP 429 后应重试成功，实际失败：%v", err)
	}
	if len(shifts) != 1 || shifts[0].ShiftName != "早班" {
		t.Errorf("班次投影不符合预期：%+v", shifts)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("期望 2 次请求（1 次 429 + 1 次成功），实际 %d", got)
	}
}

// TestNonRetryableErrorCodeStopsImmediately 校验不可重试的业务码立即失败并保留 code / req_id。
func TestNonRetryableErrorCodeStopsImmediately(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Tt-Logid", "log-123")
		writeJSON(t, w, http.StatusOK, `{"code":99992402,"msg":"param error"}`)
	})

	_, err := client.GetGroup(context.Background(), "7647")
	if err == nil {
		t.Fatal("业务错误码应返回错误")
	}
	var feishuErr *FeishuError
	if !errors.As(err, &feishuErr) {
		t.Fatalf("错误应为 *FeishuError，实际 %T：%v", err, err)
	}
	if feishuErr.Code != 99992402 {
		t.Errorf("错误码应为 99992402，实际 %d", feishuErr.Code)
	}
	if feishuErr.ReqID != "log-123" {
		t.Errorf("应保留 request_id，实际 %q", feishuErr.ReqID)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("不可重试错误应只请求 1 次，实际 %d", got)
	}
}

// TestRateLimitExhaustedReturnsFeishuError 校验重试次数用尽后返回最后一次的限流错误。
func TestRateLimitExhaustedReturnsFeishuError(t *testing.T) {
	var calls int32
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(t, w, http.StatusOK, `{"code":99991400,"msg":"触发限流"}`)
	})

	_, err := client.ListShifts(context.Background())
	if err == nil {
		t.Fatal("重试用尽应返回错误")
	}
	var feishuErr *FeishuError
	if !errors.As(err, &feishuErr) || feishuErr.Code != 99991400 {
		t.Fatalf("期望限流错误码，实际 %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != maxRetries+1 {
		t.Errorf("期望 %d 次请求，实际 %d", maxRetries+1, got)
	}
}

// TestEmptyGroupIDRejected 校验空考勤组 ID 在本地就被拒绝，不发起 HTTP 请求。
func TestEmptyGroupIDRejected(t *testing.T) {
	client := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("空 ID 不应发起 HTTP 请求")
		writeJSON(t, w, http.StatusOK, `{"code":0}`)
	})
	if _, err := client.GetGroup(context.Background(), ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}
