package hcm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient 使用虚拟凭证构建一个指向给定测试服务器 URL 的 Client。
// batchData 的 apiCode 已预先填充。
func newTestClient(baseURL string) *Client {
	cred := HCMCredential{
		APIKey:   "testkey",
		EntCode:  "testent",
		APICodes: map[string]string{APICodeKeyBatchData: "testcode"},
	}
	// 使用空实现的 Author，使测试无需真实 RSA 密钥。
	c := NewClient(baseURL, cred)
	c.auth = &noopAuthor{}
	return c
}

// noopAuthor 在单元测试中跳过所有认证头/查询参数的注入。
type noopAuthor struct{}

func (noopAuthor) ApplyAuth(_ context.Context, _ *http.Request, _ string, _ map[string]string) error {
	return nil
}

// batchResponse 构建一个与 Moka batch/data 结构相符的最小 JSON 信封。
func batchResponse(total int, records []map[string]any) []byte {
	env := map[string]any{
		"code": 200,
		"msg":  "操作成功",
		"data": map[string]any{
			"total":    total,
			"size":     len(records),
			"pageSize": 200,
			"pageNum":  1,
			"list":     records,
		},
	}
	b, _ := json.Marshal(env)
	return b
}

func TestListEmployees_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 字段与 Moka batch/data 实际响应一致（员工任职数据接口）。
		w.Write(batchResponse(1, []map[string]any{
			{
				"employee_no":        "ae000344",
				"realname":           "摩小卡",
				"department":         "IM同步测试测试测试",
				"department_id":      "30774",
				"employee_type":      "正式",
				"employee_type_id":   1,
				"employee_status":    "在职",
				"employee_status_id": 1,
				"on_boarding_date":   "2011-03-30",
				"end_probation_date": "2022-11-01",
				"leave_date":         "",
			},
		}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	list, total, err := c.ListEmployees(context.Background(), 1, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total: got %d want 1", total)
	}
	if len(list) != 1 {
		t.Fatalf("list len: got %d want 1", len(list))
	}
	e := list[0]
	if e.EmployeeNo != "ae000344" {
		t.Errorf("EmployeeNo: got %q want %q", e.EmployeeNo, "ae000344")
	}
	if e.Realname != "摩小卡" {
		t.Errorf("Realname: got %q want %q", e.Realname, "摩小卡")
	}
	if e.Department != "IM同步测试测试测试" {
		t.Errorf("Department: got %q want %q", e.Department, "IM同步测试测试测试")
	}
	if e.DepartmentID != "30774" {
		t.Errorf("DepartmentID: got %q want %q", e.DepartmentID, "30774")
	}
	if e.EmployeeType != "正式" {
		t.Errorf("EmployeeType: got %q want %q", e.EmployeeType, "正式")
	}
	if e.EmployeeTypeID != 1 {
		t.Errorf("EmployeeTypeID: got %d want 1", e.EmployeeTypeID)
	}
	if e.EmployeeStatus != "在职" {
		t.Errorf("EmployeeStatus: got %q want %q", e.EmployeeStatus, "在职")
	}
	if e.EmployeeStatusID != 1 {
		t.Errorf("EmployeeStatusID: got %d want 1", e.EmployeeStatusID)
	}
	if e.Status != 1 {
		t.Errorf("Status: got %d want 1", e.Status)
	}
	if e.OnBoardingDate != "2011-03-30" {
		t.Errorf("OnBoardingDate: got %q want %q", e.OnBoardingDate, "2011-03-30")
	}
	if e.EndProbationDate != "2022-11-01" {
		t.Errorf("EndProbationDate: got %q want %q", e.EndProbationDate, "2022-11-01")
	}
	if e.LeaveDate != "" {
		t.Errorf("LeaveDate: got %q want empty (active employee)", e.LeaveDate)
	}
}

func TestListAllEmployees_MultiPageTermination(t *testing.T) {
	// 总数为 3，pageSize 为 200 —— 服务器第 1 页返回 2 条、第 2 页返回 1 条。
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		var records []map[string]any
		switch page {
		case 1:
			records = []map[string]any{
				{"employee_no": "E001", "realname": "Alice", "department_id": "1", "employee_status_id": 1},
				{"employee_no": "E002", "realname": "Bob", "department_id": "1", "employee_status_id": 1},
			}
		case 2:
			records = []map[string]any{
				{"employee_no": "E003", "realname": "Carol", "department_id": "2", "employee_status_id": 0, "employee_status": "离职"},
			}
		default:
			// 不应到达此处。
			t.Errorf("unexpected page request: %d", page)
			records = nil
		}
		w.Write(batchResponse(3, records))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	all, err := c.ListAllEmployees(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 employees, got %d", len(all))
	}
	if page != 2 {
		t.Errorf("expected exactly 2 page requests, got %d", page)
	}
}

func TestEmployeeStatus_IDPriorityOverText(t *testing.T) {
	cases := []struct {
		name           string
		statusID       int
		statusText     string
		expectedStatus int
	}{
		// id=1 → 在职，即使文本为 离职（id 优先）
		{"id=1 text=离职 → active", 1, "离职", 1},
		// id=0（缺失）→ 回退到文本 "在职" → 在职
		{"id=0 text=在职 → active", 0, "在职", 1},
		// id=2（非 1）→ 离职，忽略文本
		{"id=2 text=在职 → inactive", 2, "在职", 0},
		// id=0 文本=离职 → 离职
		{"id=0 text=离职 → inactive", 0, "离职", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write(batchResponse(1, []map[string]any{
					{
						"employee_no":        "X001",
						"realname":           "Test",
						"department_id":      "1",
						"employee_status_id": tc.statusID,
						"employee_status":    tc.statusText,
					},
				}))
			}))
			defer srv.Close()

			c := newTestClient(srv.URL)
			list, _, err := c.ListEmployees(context.Background(), 1, 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(list) == 0 {
				t.Fatal("expected 1 employee")
			}
			if list[0].Status != tc.expectedStatus {
				t.Errorf("Status: got %d want %d", list[0].Status, tc.expectedStatus)
			}
		})
	}
}

func TestListEmployees_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":400,"msg":"invalid apiCode","data":null}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, _, err := c.ListEmployees(context.Background(), 1, 200)
	if err == nil {
		t.Fatal("expected error for non-200 API code, got nil")
	}
}
