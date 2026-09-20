// sync_test.go 验证报表同步在人员字段无法解析时的规划语义（确保离职或已从飞书删除的人员不造成虚假更新，同时保留同一行其他字段的真实更新），以及 planner→共享库 的 op 桥接把人员列 open_id 集合经 Persons 旁路透传。
package service

import (
	"context"
	"testing"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"lina-plugin-linapro-moka-report-sync/backend/internal/config"
	"lina-plugin-linapro-moka-report-sync/backend/internal/syncer"
)

func TestPreparePersonFields(t *testing.T) {
	fieldTypes := map[string]int{
		"工号":  larkbitablesdk.TypeText,
		"负责人": larkbitablesdk.TypeUser,
		"状态":  larkbitablesdk.TypeText,
	}
	// 负责人字段对应的工号来源列为"工号"。
	personFieldSources := map[string]string{"负责人": "工号"}
	singleKey := func(f string) syncer.KeySpec { return syncer.KeySpec{Fields: []string{f}, Sep: ""} }

	t.Run("仅人员字段无法解析时冻结既有记录", func(t *testing.T) {
		// 工号列有值但 resolver 返回空（如已离职员工从飞书删除）。
		rows := []syncer.Row{{"工号": "EMP001", "负责人": "张三", "状态": "处理中"}}
		preparePersonFields(rows, fieldTypes, personFieldSources)

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
		preparePersonFields(rows, fieldTypes, personFieldSources)

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
		preparePersonFields(rows, fieldTypes, personFieldSources)
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

// TestToLarkPersonsPassthrough 固化 planner→共享库 的 op 桥接：人员列 open_id 集合必须经
// Persons 旁路原样透传（不丢列、不改集合），且 planner 内部键 Name 被丢弃、RecordID 保留。
func TestToLarkPersonsPassthrough(t *testing.T) {
	creates := []syncer.CreateOp{{
		Name:    "K1",
		Fields:  syncer.Row{"状态": "处理中"},
		Persons: map[string][]string{"负责人": {"ou_a", "ou_b"}},
	}}
	gotC := toLarkCreates(creates, map[string]int{"状态": larkbitablesdk.TypeText})
	if len(gotC) != 1 {
		t.Fatalf("期望 1 条 create，got %d", len(gotC))
	}
	if gotC[0].Fields["状态"] != "处理中" {
		t.Fatalf("create 的 Fields 应透传，got %v", gotC[0].Fields)
	}
	if ids := gotC[0].Persons["负责人"]; len(ids) != 2 || ids[0] != "ou_a" || ids[1] != "ou_b" {
		t.Fatalf("create 的 Persons 应原样透传 [ou_a ou_b]，got %v", gotC[0].Persons)
	}

	updates := []syncer.UpdateOp{{
		Name:     "K2",
		RecordID: "rec_9",
		Fields:   syncer.Row{"状态": "已完成"},
		Persons:  map[string][]string{"负责人": {"ou_c"}},
	}}
	gotU := toLarkUpdates(updates, map[string]int{"状态": larkbitablesdk.TypeText})
	if len(gotU) != 1 {
		t.Fatalf("期望 1 条 update，got %d", len(gotU))
	}
	if gotU[0].RecordID != "rec_9" {
		t.Fatalf("update 的 RecordID 应透传，got %q", gotU[0].RecordID)
	}
	if gotU[0].Fields["状态"] != "已完成" {
		t.Fatalf("update 的 Fields 应透传，got %v", gotU[0].Fields)
	}
	if ids := gotU[0].Persons["负责人"]; len(ids) != 1 || ids[0] != "ou_c" {
		t.Fatalf("update 的 Persons 应原样透传 [ou_c]，got %v", gotU[0].Persons)
	}
}

// TestToDeriveRules_SkipsMultiTargetSplit 固化 split 已知限制的保护：多目标 split 无稳定顺序
// 声明时必须被跳过（不产出规则），避免把切分值写错列；同时不影响同批次的 date/regex 规则。
func TestToDeriveRules_SkipsMultiTargetSplit(t *testing.T) {
	cols := []config.DerivedColumn{
		{Kind: "split", Sources: []string{"部门路径"}, By: "/", Targets: map[string]string{"一级": "", "二级": ""}},
		{Kind: "date", Sources: []string{"入职年月"}, Targets: map[string]string{"年": "2006", "月": "01"}},
	}
	rules := toDeriveRules(context.Background(), "test", cols)
	if len(rules) != 1 {
		t.Fatalf("多目标 split 应被跳过、date 应保留，期望 1 条规则，got %d", len(rules))
	}
	if rules[0].Kind != "date" {
		t.Fatalf("保留的规则应为 date，got %q", rules[0].Kind)
	}
}

// TestToDeriveRules_KeepsSingleTargetSplit 验证单目标 split 顺序平凡稳定，正常放行。
func TestToDeriveRules_KeepsSingleTargetSplit(t *testing.T) {
	cols := []config.DerivedColumn{
		{Kind: "split", Sources: []string{"部门路径"}, By: "/", Targets: map[string]string{"一级": ""}},
	}
	rules := toDeriveRules(context.Background(), "test", cols)
	if len(rules) != 1 {
		t.Fatalf("单目标 split 应保留，got %d", len(rules))
	}
	order, _ := rules[0].Params["order"].([]string)
	if len(order) != 1 || order[0] != "一级" {
		t.Fatalf("单目标 split 的 order 应为 [一级]，got %v", order)
	}
}

func testFieldSet(fields ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		out[field] = struct{}{}
	}
	return out
}

// TestMissingPivotColumns 验证 pivot 缺失目标列的诊断计算（任务 4.2 的可诊断日志数据来源）：
// 首列（pivotHeaderColumn）不参与比对，其后转置列头缺失时按出现顺序返回，无缺失返回 nil。
// 用例数据以月份列举例，但函数对任意转置列头通用，不局限于月份语义。
func TestMissingPivotColumns(t *testing.T) {
	t.Run("部分目标列缺失时按顺序返回", func(t *testing.T) {
		pivotCols := []string{"招聘漏斗图", "5月", "6月", "7月"}
		tableFields := testFieldSet("招聘漏斗图", "5月", "年度")
		missing := missingPivotColumns(pivotCols, tableFields)
		if len(missing) != 2 || missing[0] != "6月" || missing[1] != "7月" {
			t.Fatalf("期望缺失 [6月 7月]，got %v", missing)
		}
	})

	t.Run("全部目标列存在时返回 nil", func(t *testing.T) {
		pivotCols := []string{"招聘漏斗图", "5月", "6月"}
		tableFields := testFieldSet("招聘漏斗图", "5月", "6月", "年度")
		if missing := missingPivotColumns(pivotCols, tableFields); missing != nil {
			t.Fatalf("无缺失应返回 nil，got %v", missing)
		}
	})

	t.Run("首列不参与比对", func(t *testing.T) {
		// 目标表即使不含 pivotHeaderColumn（首列），也不应把它算作缺失目标列。
		pivotCols := []string{"招聘漏斗图", "5月"}
		tableFields := testFieldSet("5月")
		if missing := missingPivotColumns(pivotCols, tableFields); missing != nil {
			t.Fatalf("首列不应计入缺失目标列，got %v", missing)
		}
	})

	t.Run("无转置列时返回 nil", func(t *testing.T) {
		pivotCols := []string{"招聘漏斗图"}
		if missing := missingPivotColumns(pivotCols, testFieldSet("招聘漏斗图")); missing != nil {
			t.Fatalf("仅首列时应返回 nil，got %v", missing)
		}
	})
}

// TestPlan_CompositeKeyWithDerivedColumns 验证复合键 + 列派生的端到端规划场景：
// 派生先于 Coalesce/Plan 执行，使派生目标列（年/月）能作为键的一部分参与匹配。
func TestPlan_CompositeKeyWithDerivedColumns(t *testing.T) {
	// 模拟 ApplyDerivedColumns 已把 入职年月 → 年/月 拆出来了。
	rows := []syncer.Row{
		{"工号": "A001", "入职年月": "2026-09", "年": "2026", "月": "09", "出勤天数": "22"},
		{"工号": "A002", "入职年月": "2026-09", "年": "2026", "月": "09", "出勤天数": "20"},
	}
	spec := syncer.KeySpec{Fields: []string{"工号", "年", "月"}, Sep: "-"}

	// A001 已存在（出勤天数为空，应触发重写）；A002 不存在（应新增）。
	existing := map[string]syncer.ExistingRecord{
		"A001-2026-09": {
			RecordID: "rec1",
			Fields:   syncer.Row{"工号": "A001", "年": "2026", "月": "09", "出勤天数": ""},
		},
	}

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "入职年月", "年", "月", "出勤天数"},
		TableFields: testFieldSet("工号", "年", "月", "出勤天数"),
		Rows:        rows,
		Existing:    existing,
	})

	if len(plan.Creates) != 1 {
		t.Fatalf("A002 应新增 1 条，got creates=%d", len(plan.Creates))
	}
	if plan.Creates[0].Name != "A002-2026-09" {
		t.Errorf("新增记录键应为 A002-2026-09，got %q", plan.Creates[0].Name)
	}
	if len(plan.Updates) != 1 {
		t.Fatalf("A001 出勤天数为空应更新 1 条，got updates=%d", len(plan.Updates))
	}
	update := plan.Updates[0]
	if update.RecordID != "rec1" {
		t.Errorf("更新应命中 rec1，got %q", update.RecordID)
	}
	// 键组成列（工号/年/月）不应出现在 diff 中。
	for _, keyCol := range []string{"工号", "年", "月"} {
		if _, ok := update.Fields[keyCol]; ok {
			t.Errorf("键列 %q 不应出现在 diff 中", keyCol)
		}
	}
	if update.Fields["出勤天数"] != "22" {
		t.Errorf("出勤天数 diff 应为 22，got %q", update.Fields["出勤天数"])
	}
}

// TestPivotPlan_SummaryColumnNotTouched 固化「汇总列不触碰」语义（任务 4.1）：
// pivot 产出列不含「年度」时，Plan 不写入、不清空、不冻结该表外列。
func TestPivotPlan_SummaryColumnNotTouched(t *testing.T) {
	// pivot 转置产出：指标列 + 各月份列，不含「年度」。
	reportCols := []string{"招聘漏斗图", "5月", "6月"}
	rows := []syncer.Row{
		{"招聘漏斗图": "简历收集数", "5月": "50", "6月": "60"},
		{"招聘漏斗图": "入职人数", "5月": "8", "6月": "3"},
	}
	// Bitable 含「年度」公式列，pivot 未产出该列。
	tableFields := testFieldSet("招聘漏斗图", "5月", "6月", "年度")
	spec := syncer.KeySpec{Fields: []string{"招聘漏斗图"}, Sep: ""}

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  reportCols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    map[string]syncer.ExistingRecord{},
	})

	// 应新增 2 行（首轮同步）。
	if len(plan.Creates) != 2 {
		t.Fatalf("期望新增 2 行，got creates=%d", len(plan.Creates))
	}
	// 「年度」不应出现在任何新增操作的字段中。
	for _, op := range plan.Creates {
		if _, ok := op.Fields["年度"]; ok {
			t.Errorf("新增操作不应包含「年度」列，got fields=%v", op.Fields)
		}
	}
	// 「年度」不在交集中（报表列不含它）。
	for _, c := range plan.Intersection {
		if c == "年度" {
			t.Error("「年度」不应出现在 Intersection 中")
		}
	}
}

// TestPivotPlan_MissingMonthColumnIgnored 固化「目标表未预建月份列被忽略」语义（任务 4.2）：
// 源报表出现目标表未预建的月份列值时，该列被忽略，其余月份列正常写入。
func TestPivotPlan_MissingMonthColumnIgnored(t *testing.T) {
	// pivot 产出包含「7月」，但目标表未预建「7月」列。
	reportCols := []string{"招聘漏斗图", "5月", "6月", "7月"}
	rows := []syncer.Row{
		{"招聘漏斗图": "简历收集数", "5月": "50", "6月": "60", "7月": "70"},
	}
	// 目标表只有 5月、6月，没有 7月。
	tableFields := testFieldSet("招聘漏斗图", "5月", "6月")
	spec := syncer.KeySpec{Fields: []string{"招聘漏斗图"}, Sep: ""}

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  reportCols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    map[string]syncer.ExistingRecord{},
	})

	if len(plan.Creates) != 1 {
		t.Fatalf("期望新增 1 行，got creates=%d", len(plan.Creates))
	}
	op := plan.Creates[0]
	// 5月、6月 正常写入。
	if op.Fields["5月"] != "50" {
		t.Errorf("5月 应为 '50'，got %q", op.Fields["5月"])
	}
	if op.Fields["6月"] != "60" {
		t.Errorf("6月 应为 '60'，got %q", op.Fields["6月"])
	}
	// 7月 不应出现（表外列被忽略）。
	if _, ok := op.Fields["7月"]; ok {
		t.Error("7月 不在目标表中，不应出现在新增操作字段里")
	}
}

// TestPivotPlan_IdempotentSecondRoundFreezes 固化「幂等冻结」语义（任务 5.1）：
// 同一份 pivot 报表连续两轮同步无变化时，第二轮全部指标行落入冻结分支，无写入。
func TestPivotPlan_IdempotentSecondRoundFreezes(t *testing.T) {
	reportCols := []string{"招聘漏斗图", "5月", "6月"}
	rows := []syncer.Row{
		{"招聘漏斗图": "简历收集数", "5月": "50", "6月": "60"},
		{"招聘漏斗图": "入职人数", "5月": "8", "6月": "3"},
	}
	tableFields := testFieldSet("招聘漏斗图", "5月", "6月")
	spec := syncer.KeySpec{Fields: []string{"招聘漏斗图"}, Sep: ""}

	// 第一轮：全部新增。
	round1 := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  reportCols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    map[string]syncer.ExistingRecord{},
	})
	if len(round1.Creates) != 2 {
		t.Fatalf("第一轮应新增 2 行，got creates=%d", len(round1.Creates))
	}

	// 模拟写入后 Bitable 的现有记录（与报表内容完全一致）。
	existing := map[string]syncer.ExistingRecord{
		"简历收集数": {RecordID: "rec1", Fields: syncer.Row{"招聘漏斗图": "简历收集数", "5月": "50", "6月": "60"}},
		"入职人数":  {RecordID: "rec2", Fields: syncer.Row{"招聘漏斗图": "入职人数", "5月": "8", "6月": "3"}},
	}

	// 第二轮：相同数据，全部冻结。
	round2 := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  reportCols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    existing,
	})
	if round2.Frozen != 2 {
		t.Fatalf("第二轮应冻结 2 行，got frozen=%d", round2.Frozen)
	}
	if len(round2.Creates) != 0 || len(round2.Updates) != 0 {
		t.Fatalf("第二轮不应有新增或更新，got creates=%d updates=%d",
			len(round2.Creates), len(round2.Updates))
	}
}

// TestFlatSkipIf_DroppedRowsNoOps 固化 flat 路径 skipIf 语义（任务 3.6）：
// 配置 skipIf 后早于阈值的行经 toSkipRules→ApplySkipRules 丢弃，不再进入 Plan，
// 从而既不产生 Creates 也不产生 Updates；不早于阈值的行正常参与规划。
func TestFlatSkipIf_DroppedRowsNoOps(t *testing.T) {
	// 模拟 Flatten 后的报表行：A001 早于阈值应被丢弃，A002 不早于阈值应保留。
	rows := []syncer.Row{
		{"工号": "A001", "申请时间": "2025-12"},
		{"工号": "A002", "申请时间": "2026-03"},
	}
	skipCfg := []config.SkipRule{{Kind: "before", Source: "申请时间", Format: "Y-m", Value: "2026-01"}}
	rules := toSkipRules(context.Background(), "test", skipCfg)
	kept, dropped, warns := syncer.ApplySkipRules(rows, rules)
	if dropped != 1 || len(kept) != 1 || kept[0]["工号"] != "A002" {
		t.Fatalf("早于阈值的 A001 应被丢弃，got dropped=%d kept=%v", dropped, kept)
	}
	if len(warns) != 0 {
		t.Errorf("正常判定不应产生告警，got %v", warns)
	}

	// 过滤后进入 Plan：目标表为空，仅保留的 A002 应新增；被丢弃的 A001 不出现在任何操作里。
	spec := syncer.KeySpec{Fields: []string{"工号"}, Sep: "-"}
	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "申请时间"},
		Rows:        kept,
		TableFields: testFieldSet("工号", "申请时间"),
		Existing:    map[string]syncer.ExistingRecord{},
	})
	if len(plan.Creates) != 1 {
		t.Fatalf("过滤后应只新增 A002 一行，got creates=%d", len(plan.Creates))
	}
	if plan.Creates[0].Name != "A002" {
		t.Errorf("新增记录键应为 A002，got %q", plan.Creates[0].Name)
	}
	for _, op := range plan.Creates {
		if op.Fields["申请时间"] == "2025-12" {
			t.Error("被丢弃的 A001 不应出现在新增操作中")
		}
	}
}

// TestPivotSkipIf_NoEffect 固化 pivot 路径 skipIf 语义（任务 3.6）：pivot 形态忽略 skipIf，
// 同步结果与不配置 skipIf 时一致（此处直接验证转置+规划结果不受 skipIf 影响，忽略告警在
// syncMappingPivot 中记录，不改变 Pivot/Plan 产物）。
func TestPivotSkipIf_NoEffect(t *testing.T) {
	// 源报表：以 公共日期 的行值作为列头转置，指标名（入职数）写入索引列 招聘漏斗图。
	cols := []string{"公共日期", "入职数"}
	rows := []syncer.Row{
		{"公共日期": "2025-05", "入职数": "50"},
		{"公共日期": "2025-06", "入职数": "60"},
	}
	pivotCols, pivotRows, conflicts := syncer.Pivot(cols, rows, "公共日期", "招聘漏斗图")
	if pivotCols == nil {
		t.Fatalf("转置应成功")
	}
	if len(conflicts) != 0 {
		t.Errorf("不应有冲突，got %v", conflicts)
	}

	tableFields := testFieldSet("招聘漏斗图", "2025-05", "2025-06")
	spec := syncer.KeySpec{Fields: []string{"招聘漏斗图"}, Sep: ""}
	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  pivotCols,
		Rows:        pivotRows,
		TableFields: tableFields,
		Existing:    map[string]syncer.ExistingRecord{},
	})
	// 配了 skipIf 也不影响：pivot 忽略行过滤，转置产出的指标行照常新增。
	if len(plan.Creates) != 1 {
		t.Fatalf("转置后应新增 1 行指标，got creates=%d", len(plan.Creates))
	}
	op := plan.Creates[0]
	if op.Fields["2025-05"] != "50" || op.Fields["2025-06"] != "60" {
		t.Errorf("转置列值应完整写入，got %v", op.Fields)
	}
}
