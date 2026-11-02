// relation_resolve_test.go 覆盖按姓名解析员工的判定规则（FB-32）：只看在职员工、
// 同工号跨租户视为同一人、去重后 0 人找不到、≥2 人重名。纯函数用例，无需数据库。

package relation

import (
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// emp 构造测试员工；active=false 表示离职。
func emp(id int64, no string, active bool) *empcap.Employee {
	status := empcap.StatusActive
	if !active {
		status = empcap.StatusDeparted
	}
	return &empcap.Employee{EmpID: id, EmployeeNo: no, Name: "张三", Status: status}
}

// TestPickActiveByName 覆盖各类同名组合的判定结果。
func TestPickActiveByName(t *testing.T) {
	cases := []struct {
		name      string
		rows      []*empcap.Employee
		want      nameOutcome
		wantEmpID int64
		wantCount int
	}{
		{name: "无同名", rows: nil, want: nameNotFound},
		{name: "仅离职", rows: []*empcap.Employee{emp(1, "E1", false)}, want: nameNotFound},
		{name: "唯一在职", rows: []*empcap.Employee{emp(1, "E1", true)}, want: nameResolved, wantEmpID: 1, wantCount: 1},
		{name: "在职两人", rows: []*empcap.Employee{emp(1, "E1", true), emp(2, "E2", true)}, want: nameDuplicate, wantCount: 2},
		{name: "在职一人加离职一人", rows: []*empcap.Employee{emp(1, "E1", false), emp(2, "E2", true)}, want: nameResolved, wantEmpID: 2, wantCount: 1},
		{name: "同工号跨租户取最小 EmpID", rows: []*empcap.Employee{emp(3, "E1", true), emp(7, "E1", true)}, want: nameResolved, wantEmpID: 3, wantCount: 1},
		{name: "工号为空按 EmpID 区分", rows: []*empcap.Employee{emp(1, "", true), emp(2, "", true)}, want: nameDuplicate, wantCount: 2},
		{name: "在职三人", rows: []*empcap.Employee{emp(1, "E1", true), emp(2, "E2", true), emp(3, "E3", true)}, want: nameDuplicate, wantCount: 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickActiveByName(c.rows)
			if got.outcome != c.want {
				t.Fatalf("判定期望 %d，实际 %d", c.want, got.outcome)
			}
			if got.activeCount != c.wantCount {
				t.Fatalf("在职同名人数期望 %d，实际 %d", c.wantCount, got.activeCount)
			}
			if c.want == nameResolved {
				if got.emp == nil || got.emp.EmpID != c.wantEmpID {
					t.Fatalf("解析员工期望 EmpID=%d，实际 %+v", c.wantEmpID, got.emp)
				}
			} else if got.emp != nil {
				t.Fatalf("未解析成功时不应返回员工，实际 %+v", got.emp)
			}
		})
	}
}

// TestNameFailReason 覆盖导入行失败原因文案（点名姓名与在职同名人数）。
func TestNameFailReason(t *testing.T) {
	if got := nameFailReason("评价者", "张三", nameResolution{outcome: nameResolved}); got != "" {
		t.Fatalf("解析成功不应有失败原因，实际 %q", got)
	}
	if got, want := nameFailReason("被评估人", "张三", nameResolution{outcome: nameDuplicate, activeCount: 2}),
		"被评估人姓名「张三」有 2 名在职同名员工，无法确定是哪一位"; got != want {
		t.Fatalf("重名原因期望 %q，实际 %q", want, got)
	}
	if got, want := nameFailReason("评价者", "李四", nameResolution{outcome: nameNotFound}),
		"评价者「李四」找不到在职员工"; got != want {
		t.Fatalf("找不到原因期望 %q，实际 %q", want, got)
	}
}
