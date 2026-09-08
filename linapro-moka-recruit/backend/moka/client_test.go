package moka

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuthHeader_TrailingColon(t *testing.T) {
	// Basic base64("mykey:") —— 密码为空，冒号是必须的。
	got := BasicAuthHeader("mykey")
	const want = "Basic bXlrZXk6"
	if got != want {
		t.Fatalf("basic auth header mismatch: got=%q want=%q", got, want)
	}
}

func TestBasicAuth_SetsHeaderNoQuery(t *testing.T) {
	auth := NewBasicAuth(BasicCredential{APIKey: "mykey"})
	req, err := http.NewRequest(http.MethodGet, "https://api.mokahr.com/api-platform/v2/stage/getStagesList", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.ApplyAuth(context.Background(), req); err != nil {
		t.Fatalf("ApplyAuth: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Basic bXlrZXk6" {
		t.Fatalf("Authorization header mismatch: got=%q", got)
	}
	// Basic Auth 绝不能追加任何签名查询参数。
	if req.URL.RawQuery != "" {
		t.Fatalf("BasicAuth must not add query params, got RawQuery=%q", req.URL.RawQuery)
	}
}

func TestPutQuery_EncodesParamsAndSucceeds(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))
	params := map[string][]string{
		"applicationId": {"96"},
		"stageId":       {"4"},
	}
	if err := c.PutQuery(context.Background(), "/api-platform/v1/applications/move_application_stage", params); err != nil {
		t.Fatalf("PutQuery: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method: got %q want PUT", gotMethod)
	}
	if gotPath != "/api-platform/v1/applications/move_application_stage" {
		t.Fatalf("path: got %q", gotPath)
	}
	// url.Values.Encode 会对 key 排序：applicationId 在 stageId 之前。
	if gotQuery != "applicationId=96&stageId=4" {
		t.Fatalf("query: got %q want applicationId=96&stageId=4", gotQuery)
	}
	if gotAuth != "Basic azo=" {
		t.Fatalf("auth: got %q", gotAuth)
	}
}

func TestPutQuery_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))
	err := c.PutQuery(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGetJSON_ReturnsBodyOn2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))
	body, err := c.GetJSON(context.Background(), "/x")
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body mismatch: got %q", string(body))
	}
}

func TestGetJSON_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, NewBasicAuth(BasicCredential{APIKey: "k"}))
	if _, err := c.GetJSON(context.Background(), "/x"); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestNewClient_DefaultBaseURL(t *testing.T) {
	c := NewClient("", NewBasicAuth(BasicCredential{APIKey: "k"}))
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("empty baseURL should fall back to %q, got %q", DefaultBaseURL, c.baseURL)
	}
}
