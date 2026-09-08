package moka

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer 返回一个指向测试服务器的客户端，该服务器按给定的状态码与
// 响应体应答，并捕获最近一次请求的路径与请求体。
func newTestServer(t *testing.T, status int, respBody string, capture func(path string, reqBody []byte)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if capture != nil {
			capture(r.URL.Path, body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))
}

func TestGetResumeContent_Success(t *testing.T) {
	// 来自 Moka 文档的真实示例（已裁剪）。
	const resp = `{"code":200,"msg":"success","data":{"resumeKey":"xingjia/2714f81f.pdf","resumeContent":"教育背景\n电子科技大学"}}`
	var gotPath string
	var gotBody []byte
	c := newTestServer(t, http.StatusOK, resp, func(p string, b []byte) { gotPath = p; gotBody = b })

	rc, err := c.GetResumeContent(context.Background(), 779645471)
	if err != nil {
		t.Fatalf("GetResumeContent: %v", err)
	}
	if gotPath != "/api-platform/application/resumeContent/get" {
		t.Fatalf("path: got %q", gotPath)
	}
	if string(gotBody) != `{"applicationId":779645471}` {
		t.Fatalf("request body: got %q", string(gotBody))
	}
	if rc.ResumeKey != "xingjia/2714f81f.pdf" {
		t.Fatalf("resumeKey: got %q", rc.ResumeKey)
	}
	if rc.ResumeContent == "" {
		t.Fatal("resumeContent should not be empty")
	}
}

func TestGetResumeContent_BusinessError(t *testing.T) {
	const resp = `{"code":400001,"msg":"application not found","data":null}`
	c := newTestServer(t, http.StatusOK, resp, nil)
	if _, err := c.GetResumeContent(context.Background(), 1); err == nil {
		t.Fatal("expected error on non-200 business code")
	}
}

func TestEhrApplications_Success(t *testing.T) {
	// 文档中 ehrApplications 响应的最小形态。
	const resp = `{"code":200,"msg":"success","data":[{"basicInfo":{"applicationId":425436988,"candidateId":425830771,"name":"张三","stage":{"id":410012144,"name":"待入职","type":102}}}]}`
	var gotBody []byte
	c := newTestServer(t, http.StatusOK, resp, func(_ string, b []byte) { gotBody = b })

	apps, err := c.EhrApplications(context.Background(), EhrApplicationsQuery{StageIDs: []int64{105019}})
	if err != nil {
		t.Fatalf("EhrApplications: %v", err)
	}
	if string(gotBody) != `{"stageIds":[105019]}` {
		t.Fatalf("request body: got %q", string(gotBody))
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 application, got %d", len(apps))
	}
	bi := apps[0].BasicInfo
	if bi.ApplicationID != 425436988 || bi.CandidateID != 425830771 || bi.Name != "张三" {
		t.Fatalf("basicInfo mismatch: %+v", bi)
	}
	if bi.Stage.ID != 410012144 || bi.Stage.Type != 102 {
		t.Fatalf("stage mismatch: %+v", bi.Stage)
	}
}

func TestEhrApplications_EmptyResult(t *testing.T) {
	const resp = `{"code":200,"msg":"success","data":[]}`
	c := newTestServer(t, http.StatusOK, resp, nil)
	apps, err := c.EhrApplications(context.Background(), EhrApplicationsQuery{StageIDs: []int64{999}})
	if err != nil {
		t.Fatalf("EhrApplications: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected empty slice, got %d", len(apps))
	}
}

// TestEhrApplications_ArchivedAndTimeRange 验证 archived 与更新时间范围过滤参数
// 透传到请求体，且 interviewInfo / archiveReasons 容错解析生效。
func TestEhrApplications_ArchivedAndTimeRange(t *testing.T) {
	const resp = `{"code":200,"msg":"success","data":[{"basicInfo":{"applicationId":123,"name":"张三","archived":true,"archiveReasons":{"name":"候选人放弃"}},"interviewInfo":[{"round":1,"roundName":"初试","interviewType":"视频面试","status":"已取消","startTime":1706522400000,"intervieweeVideoUrl":""}]}]}`
	var gotBody []byte
	c := newTestServer(t, http.StatusOK, resp, func(_ string, b []byte) { gotBody = b })

	archived := true
	apps, err := c.EhrApplications(context.Background(), EhrApplicationsQuery{
		StageIDs:          []int64{105021},
		Archived:          &archived,
		UpdateAtStartTime: "2026-08-26T00:00:00.000+08:00",
		UpdateAtEndTime:   "2026-08-27T10:00:00.000+08:00",
	})
	if err != nil {
		t.Fatalf("EhrApplications: %v", err)
	}
	body := string(gotBody)
	for _, want := range []string{`"stageIds":[105021]`, `"archived":true`, `"updateAtStartTime":"2026-08-26T00:00:00.000+08:00"`, `"updateAtEndTime":"2026-08-27T10:00:00.000+08:00"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("request body missing %q, got %q", want, body)
		}
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 application, got %d", len(apps))
	}
	if got := apps[0].BasicInfo.ArchiveReasonName(); got != "候选人放弃" {
		t.Fatalf("archiveReasons object form: got %q want 候选人放弃", got)
	}
	rounds := apps[0].Rounds()
	if len(rounds) != 1 {
		t.Fatalf("expected 1 interview round, got %d", len(rounds))
	}
	if rounds[0].InterviewType != InterviewTypeVideo || rounds[0].Status != InterviewStatusCancelled {
		t.Fatalf("round parse mismatch: %+v", rounds[0])
	}
	if rounds[0].StartTime.String() != "1706522400000" {
		t.Fatalf("startTime: got %q", rounds[0].StartTime.String())
	}
}

// TestEhrApplications_ArchiveReasonsAsString 验证 archiveReasons 为纯字符串形态时的容错解析。
func TestEhrApplications_ArchiveReasonsAsString(t *testing.T) {
	const resp = `{"code":200,"msg":"success","data":[{"basicInfo":{"applicationId":1,"name":"李四","archiveReasons":"简历不匹配"},"interviewInfo":null}]}`
	c := newTestServer(t, http.StatusOK, resp, nil)
	apps, err := c.EhrApplications(context.Background(), EhrApplicationsQuery{StageIDs: []int64{1}})
	if err != nil {
		t.Fatalf("EhrApplications: %v", err)
	}
	if got := apps[0].BasicInfo.ArchiveReasonName(); got != "简历不匹配" {
		t.Fatalf("archiveReasons string form: got %q want 简历不匹配", got)
	}
	if len(apps[0].Rounds()) != 0 {
		t.Fatalf("null interviewInfo should yield 0 rounds, got %d", len(apps[0].Rounds()))
	}
}

// TestGetInterviewInformation_SuccessCodeZero 验证 interview-information 接口
// 请求体透传 applicationIds+email，并从 entities 提取视频面试链接（code=0 为成功）。
func TestGetInterviewInformation_SuccessCodeZero(t *testing.T) {
	const resp = `{"code":0,"codeType":0,"success":true,"msg":"成功","data":[{"applicationId":123,"entities":[{"id":456,"round":16,"roundName":"初试","startTime":1706522400000,"intervieweeVideoUrl":"https://video.example.com/abc"}]}]}`
	var gotPath string
	var gotBody []byte
	c := newTestServer(t, http.StatusOK, resp, func(p string, b []byte) { gotPath = p; gotBody = b })

	infos, err := c.GetInterviewInformation(context.Background(), []int64{123}, "admin@example.com")
	if err != nil {
		t.Fatalf("GetInterviewInformation: %v", err)
	}
	if gotPath != "/api-platform/v1/interview/interview-information" {
		t.Fatalf("path: got %q", gotPath)
	}
	body := string(gotBody)
	if !strings.Contains(body, `"applicationIds":[123]`) || !strings.Contains(body, `"email":"admin@example.com"`) {
		t.Fatalf("request body mismatch: %q", body)
	}
	if len(infos) != 1 || len(infos[0].Entities) != 1 {
		t.Fatalf("expected 1 info with 1 entity, got %+v", infos)
	}
	if infos[0].Entities[0].IntervieweeVideoURL != "https://video.example.com/abc" {
		t.Fatalf("video url: got %q", infos[0].Entities[0].IntervieweeVideoURL)
	}
}

// TestGetInterviewInformation_EmptyIDsNoRequest 验证 applicationIds 为空时不发请求直接返回。
func TestGetInterviewInformation_EmptyIDsNoRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))

	infos, err := c.GetInterviewInformation(context.Background(), nil, "admin@example.com")
	if err != nil {
		t.Fatalf("GetInterviewInformation: %v", err)
	}
	if called {
		t.Fatal("expected no HTTP request for empty applicationIds")
	}
	if len(infos) != 0 {
		t.Fatalf("expected empty result, got %d", len(infos))
	}
}

func TestMoveApplicationStage_Success(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))

	if err := c.MoveApplicationStage(context.Background(), 96, 4); err != nil {
		t.Fatalf("MoveApplicationStage: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method: got %q want PUT", gotMethod)
	}
	if gotPath != "/api-platform/v1/applications/move_application_stage" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotQuery != "applicationId=96&stageId=4" {
		t.Fatalf("query: got %q", gotQuery)
	}
}

func TestGetStagesList_Success(t *testing.T) {
	// 来自 Moka 文档的真实示例（已裁剪）。
	const resp = `{"code":200,"msg":"success","data":[{"id":105019,"name":"初筛","type":100},{"id":105021,"name":"面试","type":201}]}`
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))

	stages, err := c.GetStagesList(context.Background())
	if err != nil {
		t.Fatalf("GetStagesList: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method: got %q want GET", gotMethod)
	}
	if gotPath != "/api-platform/v2/stage/getStagesList" {
		t.Fatalf("path: got %q", gotPath)
	}
	if len(stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(stages))
	}
	if stages[0].Name != "初筛" || stages[0].Type != 100 {
		t.Fatalf("stage[0] mismatch: %+v", stages[0])
	}
}

func TestGetReportData_AcceptsBothSuccessCodes(t *testing.T) {
	for _, code := range []int{200, 1000000} {
		resp := `{"code":` + itoa(code) + `,"msg":"成功","data":{"headers":[{"dataIndex":"c_1","title":"性别","type":"HEADER"}],"rows":[{"c_1":"男性"}],"size":1}}`
		c := newTestServer(t, http.StatusOK, resp, nil)
		rd, err := c.GetReportData(context.Background(), 12318)
		if err != nil {
			t.Fatalf("code=%d: %v", code, err)
		}
		if rd.Size != 1 || len(rd.Headers) != 1 || rd.Headers[0].Title != "性别" {
			t.Fatalf("code=%d: report data mismatch: %+v", code, rd)
		}
	}
}

// itoa 仅为上表避免引入 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
