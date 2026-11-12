// sync_person_test.go 验证人员字段的来源解析：工号来源与姓名来源如何把人员字段换成工号、姓名按在职与
// 去重口径判定（唯一、跨租户同人、离职、重名、无工号），以及重名告警正文的汇总；同时固化人员字段
// 无法解析时的规划语义（不造成虚假更新，同时保留同一行其他字段的真实更新）。

package service

import (
	"strings"
	"testing"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-moka-report-sync/backend/internal/syncer"
)

func TestPreparePersonFields(t *testing.T) {
	fieldTypes := map[string]int{
		"工号":  larkbitablesdk.TypeText,
		"负责人": larkbitablesdk.TypeUser,
		"状态":  larkbitablesdk.TypeText,
	}
	// 负责人字段对应的工号来源列为"工号"。
	personFieldEmpNoSources := map[string]string{"负责人": "工号"}
	singleKey := func(f string) syncer.KeySpec { return syncer.KeySpec{Fields: []string{f}, Sep: ""} }

	t.Run("仅人员字段无法解析时冻结既有记录", func(t *testing.T) {
		// 工号列有值但 resolver 返回空（如已离职员工从飞书删除）。
		rows := []syncer.Row{{"工号": "EMP001", "负责人": "张三", "状态": "处理中"}}
		preparePersonFields(rows, fieldTypes, personFieldEmpNoSources, nil, nil)

		plan := syncer.Plan(syncer.PlanInput{
			Key:                           singleKey("工号"),
			ReportCols:                    []string{"工号", "负责人", "状态"},
			TableFields:                   testFieldSet("工号", "负责人", "状态"),
			PersonCols:                    testFieldSet("负责人"),
			ResolvePersonIDsByEmployeeNos: func([]string) []string { return nil },
			Rows:                          rows,
			Existing: map[string]syncer.ExistingRecord{
				"EMP001": {RecordID: "rec_1", Fields: syncer.Row{"工号": "EMP001", "负责人": "历史人员", "状态": "处理中"}},
			},
		})
		if plan.Frozen != 1 || len(plan.Updates) != 0 {
			t.Fatalf("无法解析的人员字段不应触发更新，got frozen=%d updates=%d", plan.Frozen, len(plan.Updates))
		}
	})

	t.Run("同一行其他字段变更仍更新", func(t *testing.T) {
		rows := []syncer.Row{{"工号": "EMP002", "负责人": "李四", "状态": "已完成"}}
		preparePersonFields(rows, fieldTypes, personFieldEmpNoSources, nil, nil)

		plan := syncer.Plan(syncer.PlanInput{
			Key:                           singleKey("工号"),
			ReportCols:                    []string{"工号", "负责人", "状态"},
			TableFields:                   testFieldSet("工号", "负责人", "状态"),
			PersonCols:                    testFieldSet("负责人"),
			ResolvePersonIDsByEmployeeNos: func([]string) []string { return nil },
			Rows:                          rows,
			Existing: map[string]syncer.ExistingRecord{
				"EMP002": {RecordID: "rec_2", Fields: syncer.Row{"工号": "EMP002", "负责人": "历史人员", "状态": "处理中"}},
			},
		})
		if len(plan.Updates) != 1 {
			t.Fatalf("其他字段变更应保留更新，got updates=%d", len(plan.Updates))
		}
		update := plan.Updates[0]
		if len(update.Fields) != 1 || update.Fields["状态"] != "已完成" {
			t.Fatalf("更新应只包含状态字段，got fields=%v", update.Fields)
		}
	})

	t.Run("人员字段值被替换为工号后 resolver 能命中", func(t *testing.T) {
		rows := []syncer.Row{{"工号": "EMP003", "负责人": "王五", "状态": "处理中"}}
		resolver := func(empNos []string) []string {
			for _, no := range empNos {
				if no == "EMP003" {
					return []string{"ou_abc123"}
				}
			}
			return nil
		}
		preparePersonFields(rows, fieldTypes, personFieldEmpNoSources, nil, nil)
		// 人员字段值应被替换为工号，供 Plan 解析为 open_id 集合。
		if rows[0]["负责人"] != "EMP003" {
			t.Fatalf("人员字段值应替换为工号 EMP003，got %q", rows[0]["负责人"])
		}

		// 解析命中且回读集合不同 → 人员列经 Persons 旁路携带已解析的 open_id 集合。
		plan := syncer.Plan(syncer.PlanInput{
			Key:                           singleKey("工号"),
			ReportCols:                    []string{"工号", "负责人", "状态"},
			TableFields:                   testFieldSet("工号", "负责人", "状态"),
			PersonCols:                    testFieldSet("负责人"),
			ResolvePersonIDsByEmployeeNos: resolver,
			Rows:                          rows,
			Existing: map[string]syncer.ExistingRecord{
				"EMP003": {RecordID: "rec_3", Fields: syncer.Row{"工号": "EMP003", "负责人": "王五", "状态": "处理中"}},
			},
		})
		if len(plan.Updates) != 1 {
			t.Fatalf("人员列集合不等价应产生 1 条更新，got updates=%d", len(plan.Updates))
		}
		if ids := plan.Updates[0].Persons["负责人"]; len(ids) != 1 || ids[0] != "ou_abc123" {
			t.Fatalf("人员列应经 Persons 旁路写入已解析 open_id，got %v", plan.Updates[0].Persons)
		}
		if _, ok := plan.Updates[0].Fields["负责人"]; ok {
			t.Fatalf("人员列不应进入 Fields，got %v", plan.Updates[0].Fields)
		}
	})
}

// TestPreparePersonFields_NameSources 固化姓名来源：同名字段自引用、来源冲突工号优先、未声明清空、
// 解析不到的姓名留空。
func TestPreparePersonFields_NameSources(t *testing.T) {
	fieldTypes := map[string]int{
		"工号":    larkbitablesdk.TypeText,
		"面谈人 1": larkbitablesdk.TypeUser,
		"面谈人 2": larkbitablesdk.TypeUser,
		"负责人":   larkbitablesdk.TypeUser,
		"其他人":   larkbitablesdk.TypeUser,
	}
	nameToEmpNo := map[string]string{"张三": "A001", "李四": "A002"}

	t.Run("姓名来源换成工号，字段名与来源列同名也成立", func(t *testing.T) {
		rows := []syncer.Row{{"工号": "X", "面谈人 1": " 张三 ", "面谈人 2": "李四"}}
		nameSources := map[string]string{"面谈人 1": "面谈人 1", "面谈人 2": "面谈人 2"}
		preparePersonFields(rows, fieldTypes, nil, nameSources, nameToEmpNo)
		if rows[0]["面谈人 1"] != "A001" || rows[0]["面谈人 2"] != "A002" {
			t.Fatalf("姓名应换成工号（去首尾空白后匹配），got %v", rows[0])
		}
	})

	t.Run("来源列是另一人员字段时不受遍历顺序影响", func(t *testing.T) {
		// 面谈人 2 取面谈人 1 的姓名：必须读到原值「张三」，而不是面谈人 1 已被替换后的工号。
		for range 20 {
			rows := []syncer.Row{{"面谈人 1": "张三", "面谈人 2": "李四"}}
			nameSources := map[string]string{"面谈人 1": "面谈人 1", "面谈人 2": "面谈人 1"}
			preparePersonFields(rows, fieldTypes, nil, nameSources, nameToEmpNo)
			if rows[0]["面谈人 1"] != "A001" || rows[0]["面谈人 2"] != "A001" {
				t.Fatalf("两个字段都应按原值「张三」解析为 A001，got %v", rows[0])
			}
		}
	})

	t.Run("同一字段两边都声明时工号来源优先", func(t *testing.T) {
		rows := []syncer.Row{{"工号": "B009", "负责人": "张三"}}
		preparePersonFields(rows, fieldTypes,
			map[string]string{"负责人": "工号"},
			map[string]string{"负责人": "负责人"},
			nameToEmpNo)
		if rows[0]["负责人"] != "B009" {
			t.Fatalf("工号来源应优先，got %q", rows[0]["负责人"])
		}
	})

	t.Run("姓名解析不到时留空、未声明来源的人员字段清空", func(t *testing.T) {
		rows := []syncer.Row{{"面谈人 1": "王五", "其他人": "赵六", "工号": "X"}}
		preparePersonFields(rows, fieldTypes, nil, map[string]string{"面谈人 1": "面谈人 1"}, nameToEmpNo)
		if rows[0]["面谈人 1"] != "" {
			t.Fatalf("解析不到的姓名应留空，got %q", rows[0]["面谈人 1"])
		}
		if rows[0]["其他人"] != "" {
			t.Fatalf("未声明来源的人员字段应清空，got %q", rows[0]["其他人"])
		}
		if rows[0]["工号"] != "X" {
			t.Fatalf("非人员字段不应被改动，got %q", rows[0]["工号"])
		}
	})
}

// TestCollectPersonNames 固化姓名收集：只看人员类型字段、跳过被工号来源覆盖的字段、去空白去重。
func TestCollectPersonNames(t *testing.T) {
	fieldTypes := map[string]int{
		"面谈人 1": larkbitablesdk.TypeUser,
		"负责人":   larkbitablesdk.TypeUser,
		"备注":    larkbitablesdk.TypeText,
	}
	rows := []syncer.Row{
		{"面谈人 1": " 张三", "负责人": "李四", "备注": "王五"},
		{"面谈人 1": "张三 ", "负责人": "", "备注": "赵六"},
		{"面谈人 1": "", "负责人": "钱七", "备注": ""},
	}
	nameSources := map[string]string{"面谈人 1": "面谈人 1", "负责人": "负责人", "备注": "备注"}
	empNoSources := map[string]string{"负责人": "工号"}

	got := collectPersonNames(rows, fieldTypes, empNoSources, nameSources)
	if len(got) != 1 || got[0] != "张三" {
		t.Fatalf("只应收集面谈人 1 中去重后的「张三」（负责人被工号来源覆盖、备注非人员字段），got %v", got)
	}
	if names := collectPersonNames(rows, fieldTypes, nil, nil); names != nil {
		t.Fatalf("未声明姓名来源时应返回 nil，got %v", names)
	}
}

// TestJudgePersonNames 固化姓名判定口径：只认在职、按工号去重算一人、重名不猜、无工号不解析。
func TestJudgePersonNames(t *testing.T) {
	active := func(id int64, no string) *empcap.Employee {
		return &empcap.Employee{EmpID: id, EmployeeNo: no, Status: empcap.StatusActive}
	}
	departed := func(id int64, no string) *empcap.Employee {
		return &empcap.Employee{EmpID: id, EmployeeNo: no, Status: empcap.StatusDeparted}
	}
	byName := map[string][]*empcap.Employee{
		"唯一":      {active(1, "A001")},
		"跨租户":     {active(2, "A002"), active(3, "A002")},
		"离职":      {departed(4, "A004")},
		"在职与离职同名": {departed(5, "A005"), active(6, "A006")},
		"重名":      {active(7, "A007"), active(8, "A008")},
		"无工号":     {active(9, "")},
		"无工号两行":   {active(10, ""), active(11, "")},
	}
	names := []string{"唯一", "跨租户", "离职", "在职与离职同名", "重名", "无工号", "无工号两行", "不存在"}

	res := judgePersonNames(names, byName)

	wantNos := map[string]string{"唯一": "A001", "跨租户": "A002", "在职与离职同名": "A006"}
	if len(res.employeeNos) != len(wantNos) {
		t.Fatalf("可解析姓名应为 %v，got %v", wantNos, res.employeeNos)
	}
	for name, no := range wantNos {
		if res.employeeNos[name] != no {
			t.Errorf("姓名 %q 应解析为 %q，got %q", name, no, res.employeeNos[name])
		}
	}
	if got := strings.Join(res.duplicates, ","); got != "重名,无工号两行" {
		t.Errorf("重名应为 [重名 无工号两行]（无工号时按 EmpID 去重仍是两人），got %v", res.duplicates)
	}
	if got := strings.Join(res.unresolved, ","); got != "离职,无工号,不存在" {
		t.Errorf("未解析应为 [离职 无工号 不存在]，got %v", res.unresolved)
	}
}

// TestBuildNameDuplicateDetail 固化重名告警正文：多个映射汇总为一条、按映射分组、无重名不发。
func TestBuildNameDuplicateDetail(t *testing.T) {
	if got := buildNameDuplicateDetail([]MappingResult{{Label: "a"}, {Label: "b"}}); got != "" {
		t.Fatalf("没有重名时应返回空串，got %q", got)
	}

	detail := buildNameDuplicateDetail([]MappingResult{
		{Label: "面谈表(reportId=1)", DuplicateNames: []string{"张三", "李四"}},
		{Label: "无重名(reportId=2)"},
		{Label: "reportId=3", DuplicateNames: []string{"王五"}},
	})
	for _, want := range []string{"面谈表(reportId=1)：张三、李四", "reportId=3：王五"} {
		if !strings.Contains(detail, want) {
			t.Errorf("告警正文应包含 %q，got %q", want, detail)
		}
	}
	if strings.Contains(detail, "无重名") {
		t.Errorf("没有重名的映射不应出现在正文中，got %q", detail)
	}

	many := make([]string, maxListedNames+5)
	for i := range many {
		many[i] = "人"
	}
	if got := joinNames(many); !strings.HasSuffix(got, "等 25 个") {
		t.Errorf("超过上限应截断并注明总数，got %q", got)
	}
}
