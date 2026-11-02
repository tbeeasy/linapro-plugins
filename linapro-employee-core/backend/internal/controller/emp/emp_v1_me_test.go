// emp_v1_me_test.go 验证 Me 把员工读模型完整投影到响应：工号与部门 id 不得丢失，
// 无部门时返回空数组而非 null；缺少 EmpContext 时按未认证拒绝。

package emp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	v1 "lina-plugin-linapro-employee-core/backend/api/emp/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// fakeMeEmployees 只实现 Me 用到的 GetByID；嵌入接口满足其余方法签名，
// 若被意外调用会因 nil 接口而 panic，从而暴露多余依赖。
type fakeMeEmployees struct {
	empcap.Service
	emp *empcap.Employee
}

func (f *fakeMeEmployees) GetByID(context.Context, int64) (*empcap.Employee, error) {
	return f.emp, nil
}

// TestMeProjectsEmployeeNoAndDepartments 验证工号与飞书部门 id 被投影到响应
// （此前控制器漏填这两个字段，前端刷新后工号被空串覆盖）。
func TestMeProjectsEmployeeNoAndDepartments(t *testing.T) {
	ctrl := NewMeV1(&fakeMeEmployees{emp: &empcap.Employee{
		EmpID:      10,
		TenantID:   7,
		Name:       "张三",
		EmployeeNo: "GZ001198",
		Status:     empcap.StatusActive,
		Departments: []empcap.Department{
			{LarkDepartmentID: "od-primary", IsPrimary: true},
			{LarkDepartmentID: "od-second"},
		},
	}})
	ctx := empcap.WithEmpContext(context.Background(), empcap.EmpContext{EmpID: 10, TenantID: 7})

	res, err := ctrl.Me(ctx, &v1.MeReq{})
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if res.MokaEmployeeNo != "GZ001198" {
		t.Fatalf("want mokaEmployeeNo GZ001198, got %q", res.MokaEmployeeNo)
	}
	if len(res.DepartmentIds) != 2 || res.DepartmentIds[0] != "od-primary" || res.DepartmentIds[1] != "od-second" {
		t.Fatalf("want lark department ids in order, got %v", res.DepartmentIds)
	}
	if res.EmpId != 10 || res.TenantId != 7 || res.Name != "张三" || res.Status != empcap.StatusActive {
		t.Fatalf("unexpected identity fields: %+v", res)
	}
}

// TestMeEmptyDepartmentsSerializeAsArray 验证无部门时 departmentIds 序列化为 []
// 而不是 null，前端按数组处理无需判空。
func TestMeEmptyDepartmentsSerializeAsArray(t *testing.T) {
	ctrl := NewMeV1(&fakeMeEmployees{emp: &empcap.Employee{EmpID: 1, Status: empcap.StatusActive}})
	ctx := empcap.WithEmpContext(context.Background(), empcap.EmpContext{EmpID: 1})

	res, err := ctrl.Me(ctx, &v1.MeReq{})
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(body["departmentIds"]) != "[]" {
		t.Fatalf("want departmentIds [], got %s", body["departmentIds"])
	}
}

// TestMeWithoutEmpContextUnauthorized 验证未经 EmpAuth 注入身份时按未认证拒绝。
func TestMeWithoutEmpContextUnauthorized(t *testing.T) {
	ctrl := NewMeV1(&fakeMeEmployees{})
	if _, err := ctrl.Me(context.Background(), &v1.MeReq{}); !errors.Is(err, empcap.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}
