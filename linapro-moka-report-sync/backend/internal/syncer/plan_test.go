package syncer

import (
	"testing"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

func fieldSet(cols ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		m[c] = struct{}{}
	}
	return m
}

// key1 构造单列 KeySpec，等价于原 uniqueField。
func key1(field string) KeySpec {
	return KeySpec{Fields: []string{field}, Sep: ""}
}

func TestPlan_CreateWhenAbsent(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if len(p.Creates) != 1 || len(p.Updates) != 0 || p.Frozen != 0 {
		t.Fatalf("expected 1 create, got %+v", p)
	}
	if p.Creates[0].Name != "张伟" || p.Creates[0].Fields["性别"] != "男性" {
		t.Fatalf("unexpected create op: %+v", p.Creates[0])
	}
}

func TestPlan_RewriteWhenIntersectionColumnEmpty(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "民族": ""}},
		},
	}
	p := Plan(in)
	if len(p.Updates) != 1 || len(p.Creates) != 0 || p.Frozen != 0 {
		t.Fatalf("expected 1 update, got %+v", p)
	}
	u := p.Updates[0]
	if u.RecordID != "rec1" || u.Fields["民族"] != "汉族" {
		t.Fatalf("unexpected update op: %+v", u)
	}
}

func TestPlan_FreezeWhenAllIntersectionColumnsFilled(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "民族"},
		TableFields: fieldSet("姓名", "性别", "民族"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "民族": "汉族"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "民族": "汉族"}},
		},
	}
	p := Plan(in)
	if p.Frozen != 1 || len(p.Updates) != 0 || len(p.Creates) != 0 {
		t.Fatalf("expected 1 frozen, got %+v", p)
	}
}

func TestPlan_IgnoresTableOnlyColumns(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别"},
		TableFields: fieldSet("姓名", "性别", "备注"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "性别": "男性", "备注": ""}},
		},
	}
	p := Plan(in)
	if p.Frozen != 1 || len(p.Updates) != 0 {
		t.Fatalf("manual empty column must not force rewrite; got %+v", p)
	}
	for _, c := range p.Intersection {
		if c == "备注" {
			t.Fatal("备注 must not be in the intersection")
		}
	}
}

func TestPlan_SkipsRowsWithoutName(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别"},
		TableFields: fieldSet("姓名", "性别"),
		Rows: []Row{
			{"姓名": "", "性别": "男性"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if p.SkippedNoName != 1 || len(p.Creates) != 0 {
		t.Fatalf("expected 1 skipped-no-name, got %+v", p)
	}
}

func TestPlan_OnlyWritesIntersectionColumns(t *testing.T) {
	in := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "性别", "内部编码"},
		TableFields: fieldSet("姓名", "性别"),
		Rows: []Row{
			{"姓名": "张伟", "性别": "男性", "内部编码": "X1"},
		},
		Existing: map[string]ExistingRecord{},
	}
	p := Plan(in)
	if len(p.Creates) != 1 {
		t.Fatalf("expected 1 create, got %+v", p)
	}
	if _, ok := p.Creates[0].Fields["内部编码"]; ok {
		t.Fatal("create must not carry a column absent from the table")
	}
}

// TestPlan_NumberStronglyTypedFreeze 锁定强类型数字比对：Moka 侧 "90" 与 Bitable 回读侧
// "90.0" 在数值上相等。旧的纯字符串比对会把它误判为变更、每轮空转重写；接入 FieldTypes 后
// 数字列两侧 ParseFloat 判等，应冻结。不给 FieldTypes（退化为文本比对）时仍复现旧误判。
func TestPlan_NumberStronglyTypedFreeze(t *testing.T) {
	base := PlanInput{
		Key:         key1("工号"),
		ReportCols:  []string{"工号", "出勤天数"},
		TableFields: fieldSet("工号", "出勤天数"),
		Rows: []Row{
			{"工号": "A001", "出勤天数": "90"},
		},
		Existing: map[string]ExistingRecord{
			"A001": {RecordID: "rec1", Fields: Row{"工号": "A001", "出勤天数": "90.0"}},
		},
	}

	// 文本比对（无 FieldTypes）：复现旧误判为 1 条更新。
	if got := Plan(base); len(got.Updates) != 1 {
		t.Fatalf("文本比对下 \"90\" vs \"90.0\" 应复现误判为 1 条更新，got updates=%d frozen=%d", len(got.Updates), got.Frozen)
	}

	// 强类型比对：声明数字列后应冻结。
	base.FieldTypes = map[string]int{"出勤天数": larkbitablesdk.TypeNumber}
	got := Plan(base)
	if got.Frozen != 1 || len(got.Updates) != 0 {
		t.Fatalf("数字列 \"90\" 与 \"90.0\" 应判等而冻结，got frozen=%d updates=%d", got.Frozen, len(got.Updates))
	}
}

// TestPlan_DateScientificNotationFreeze 锁定强类型日期比对消化科学计数法：飞书日期列回读为
// 数字，大整数经字符串化后呈科学计数法（如 "1.75968e+12"），与 Moka 侧归一后的毫秒整数串
// "1759680000000" 纯文本比对不相等。接入 FieldTypes 后两侧归一到 int64 毫秒判等，应冻结。
func TestPlan_DateScientificNotationFreeze(t *testing.T) {
	base := PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "转正日期"},
		TableFields: fieldSet("姓名", "转正日期"),
		Rows: []Row{
			// normalizeColumns 已把日期文本归一为毫秒整数串。
			{"姓名": "张伟", "转正日期": "1759680000000"},
		},
		Existing: map[string]ExistingRecord{
			"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "转正日期": "1.75968e+12"}},
		},
	}

	// 文本比对（无 FieldTypes）：科学计数法回读与毫秒整数串不等，复现误判为 1 条更新。
	if got := Plan(base); len(got.Updates) != 1 {
		t.Fatalf("文本比对下科学计数法回读应复现误判为 1 条更新，got updates=%d frozen=%d", len(got.Updates), got.Frozen)
	}

	// 强类型比对：声明日期列后两侧归一到毫秒判等，应冻结。
	base.FieldTypes = map[string]int{"转正日期": larkbitablesdk.TypeDateTime}
	got := Plan(base)
	if got.Frozen != 1 || len(got.Updates) != 0 {
		t.Fatalf("日期列毫秒串与科学计数法回读应判等而冻结，got frozen=%d updates=%d", got.Frozen, len(got.Updates))
	}
}

func TestFlattenReport_MapsTitlesToValues(t *testing.T) {
	data := &ReportData{
		Headers: []ReportHeader{
			{DataIndex: "c_1", Title: "姓名", Type: "HEADER"},
			{DataIndex: "c_2", Title: "性别", Type: "HEADER"},
		},
		Rows: []map[string]any{
			{"c_1": "张伟", "c_2": "男性"},
		},
	}
	cols, rows := FlattenReport(data)
	if len(cols) != 2 || cols[0] != "姓名" || cols[1] != "性别" {
		t.Fatalf("unexpected cols: %v", cols)
	}
	if len(rows) != 1 || rows[0]["姓名"] != "张伟" || rows[0]["性别"] != "男性" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestStringify_NumberStaysInteger(t *testing.T) {
	if got := stringify(float64(83475647)); got != "83475647" {
		t.Fatalf("expected integer formatting, got %q", got)
	}
	if got := stringify(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}
}

// TestStringify_RichTextSegments 验证 stringify 对 Bitable 读取方抽取出的富文本分段切片
// （[]string）逐段归一后拼接：每段 trim、"-" 归零。
func TestStringify_RichTextSegments(t *testing.T) {
	if got := stringify([]string{"第一段", "-", "第二段"}); got != "第一段第二段" {
		t.Fatalf("expected per-segment normalize then join, got %q", got)
	}
}

// TestPlan_DateNormalizedFreeze 复现「日期列反复更新」缺陷并锁定修复：
// Bitable 回读值为写侧编码的毫秒字符串（如 "1759680000000"），而 Moka 侧是原始日期文本
// （如 "2025-10-06"）。若 Moka 侧保留文本，Plan 会把它与毫秒字符串比较判为差异而每轮重写；
// 修复后 Moka 侧经 NormalizeColumns 归一成同一毫秒字符串，Plan 应冻结该记录。
func TestPlan_DateNormalizedFreeze(t *testing.T) {
	rows := []Row{
		{"姓名": "张伟", "转正日期": "2025-10-06"},
	}
	// Bitable 回读：写侧把日期写成毫秒（东八区零点），读回收 Stringify 的毫秒数字串。
	existing := map[string]ExistingRecord{
		"张伟": {RecordID: "rec1", Fields: Row{"姓名": "张伟", "转正日期": "1759680000000"}},
	}
	// 归一前：原始日期文本与毫秒字符串不等，被误判为更新。
	raw := Plan(PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "转正日期"},
		TableFields: fieldSet("姓名", "转正日期"),
		Rows:        rows,
		Existing:    existing,
	})
	if len(raw.Updates) != 1 {
		t.Fatalf("归一前应被误判为 1 条更新，got updates=%d", len(raw.Updates))
	}
	// 归一后：dates 列解析为同一毫秒字符串，diff 为空，冻结。
	NormalizeColumns(rows, []string{"转正日期"}, fakeParseMillis)
	frozen := Plan(PlanInput{
		Key:         key1("姓名"),
		ReportCols:  []string{"姓名", "转正日期"},
		TableFields: fieldSet("姓名", "转正日期"),
		Rows:        rows,
		Existing:    existing,
	})
	if frozen.Frozen != 1 || len(frozen.Updates) != 0 {
		t.Fatalf("归一后应冻结 1 条且无更新，got frozen=%d updates=%d", frozen.Frozen, len(frozen.Updates))
	}
}

// TestPlan_CompositeKeySkipsKeyFieldsInDiff 验证复合键组成列在 diffFields 中被跳过。
func TestPlan_CompositeKeySkipsKeyFieldsInDiff(t *testing.T) {
	spec := KeySpec{Fields: []string{"工号", "考勤月"}, Sep: "-"}
	rows := []Row{
		{"工号": "A001", "考勤月": "2026-09", "出勤天数": "22"},
	}
	existing := map[string]ExistingRecord{
		"A001-2026-09": {
			RecordID: "rec1",
			// 键列值相同，出勤天数为空（触发重写）
			Fields: Row{"工号": "A001", "考勤月": "2026-09", "出勤天数": ""},
		},
	}
	p := Plan(PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "考勤月", "出勤天数"},
		TableFields: fieldSet("工号", "考勤月", "出勤天数"),
		Rows:        rows,
		Existing:    existing,
	})
	if len(p.Updates) != 1 {
		t.Fatalf("出勤天数为空应触发 1 条更新，got updates=%d", len(p.Updates))
	}
	diff := p.Updates[0].Fields
	// 键组成列不应出现在 diff 中。
	if _, ok := diff["工号"]; ok {
		t.Error("键列 工号 不应出现在 diff 中")
	}
	if _, ok := diff["考勤月"]; ok {
		t.Error("键列 考勤月 不应出现在 diff 中")
	}
	if diff["出勤天数"] != "22" {
		t.Errorf("出勤天数应在 diff 中为 22，got %q", diff["出勤天数"])
	}
}

// TestPlan_CompositeKeyEmptyFieldSkipped 验证复合键任一列为空时行被跳过。
func TestPlan_CompositeKeyEmptyFieldSkipped(t *testing.T) {
	spec := KeySpec{Fields: []string{"工号", "考勤月"}, Sep: "-"}
	rows := []Row{
		{"工号": "A001", "考勤月": "", "出勤天数": "22"}, // 考勤月空 → 键为空 → 跳过
	}
	p := Plan(PlanInput{
		Key:         spec,
		ReportCols:  []string{"工号", "考勤月", "出勤天数"},
		TableFields: fieldSet("工号", "考勤月", "出勤天数"),
		Rows:        rows,
		Existing:    map[string]ExistingRecord{},
	})
	if p.SkippedNoName != 1 || len(p.Creates) != 0 {
		t.Fatalf("复合键含空列应跳过，got skipped=%d creates=%d", p.SkippedNoName, len(p.Creates))
	}
}

// fakeResolveOpenIDs 是测试替身：模拟「多工号 → 去重 open_id 集合」解析器（empcap 复数版）。
// GZ000040 对应双租户同人两个 open_id，用于验证多 id 与顺序无关判等。
func fakeResolveOpenIDs(empNos []string) []string {
	var ids []string
	for _, no := range empNos {
		switch no {
		case "GZ000040":
			ids = append(ids, "ou_huang", "ou_huang_alt")
		case "GZ000500":
			ids = append(ids, "ou_lixi")
		}
	}
	return ids
}

// personPlanInput 构造一个「人员列已声明 + 已注入解析器」的 PlanInput 基座。
func personPlanInput() PlanInput {
	return PlanInput{
		Key:                           key1("工号"),
		ReportCols:                    []string{"工号", "直接上级"},
		TableFields:                   fieldSet("工号", "直接上级"),
		PersonCols:                    fieldSet("直接上级"),
		ResolvePersonIDsByEmployeeNos: fakeResolveOpenIDs,
	}
}

// TestPlan_PersonColumnFreezesWhenOpenIDSetEqual 复现「人员列每轮空转重写」缺陷并锁定修复。
// Moka 侧 `直接上级` 值是工号 GZ000040，Bitable 回读侧 Fields 中是姓名「黄春越」——
// 二者不在同一空间，按文本比对永远不等，人员列会每轮被误判为变更并重写。
// 修复后按 open_id 集合比对：回读 PersonIDs 与工号解析结果等价，应冻结。
func TestPlan_PersonColumnFreezesWhenOpenIDSetEqual(t *testing.T) {
	rows := []Row{
		{"工号": "GZ000500", "直接上级": "GZ000040"},
	}
	existing := map[string]ExistingRecord{
		"GZ000500": {
			RecordID: "rec1",
			// 回读侧人员列是姓名文本，与 Moka 侧工号必然不等。
			Fields: Row{"工号": "GZ000500", "直接上级": "黄春越"},
			// 结构化旁路给出该单元格真实的 open_id 集合（顺序与解析器返回不同）。
			PersonIDs: map[string][]string{"直接上级": {"ou_huang_alt", "ou_huang"}},
		},
	}
	base := personPlanInput()
	base.Rows = rows
	base.Existing = existing

	// 未声明人员列时退化为文本比对：复现缺陷，工号 vs 姓名被误判为 1 条更新。
	raw := base
	raw.PersonCols = nil
	raw.ResolvePersonIDsByEmployeeNos = nil
	if got := Plan(raw); len(got.Updates) != 1 {
		t.Fatalf("文本比对下应复现误判为 1 条更新，got updates=%d frozen=%d", len(got.Updates), got.Frozen)
	}

	// 声明人员列并注入解析器后：open_id 集合等价（顺序不同不影响），应冻结。
	got := Plan(base)
	if got.Frozen != 1 || len(got.Updates) != 0 {
		t.Fatalf("open_id 集合等价应冻结，got frozen=%d updates=%d", got.Frozen, len(got.Updates))
	}
}

// TestPlan_PersonColumnUpdatesWhenOpenIDSetDiffers 验证 open_id 集合不等价时仍会更新
// （换人、离职复入导致 id 漂移），且写入的是**已解析的 open_id 集合**：
// 人员列不进 Fields（工号文本无法写进飞书人员字段），而是产出到 op.Persons 旁路。
func TestPlan_PersonColumnUpdatesWhenOpenIDSetDiffers(t *testing.T) {
	in := personPlanInput()
	in.Rows = []Row{
		{"工号": "GZ000500", "直接上级": "GZ000040"},
	}
	in.Existing = map[string]ExistingRecord{
		"GZ000500": {
			RecordID:  "rec1",
			Fields:    Row{"工号": "GZ000500", "直接上级": "旧上级"},
			PersonIDs: map[string][]string{"直接上级": {"ou_someone_else"}},
		},
	}
	p := Plan(in)
	if len(p.Updates) != 1 {
		t.Fatalf("open_id 集合不等价应产生 1 条更新，got updates=%d frozen=%d", len(p.Updates), p.Frozen)
	}
	u := p.Updates[0]
	if _, ok := u.Fields["直接上级"]; ok {
		t.Fatalf("人员列不应进入 Fields（工号文本写不进人员字段），got %v", u.Fields)
	}
	got := u.Persons["直接上级"]
	want := []string{"ou_huang", "ou_huang_alt"}
	if len(got) != len(want) {
		t.Fatalf("Persons 应携带已解析的 open_id 集合，got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Persons open_id 顺序/内容: got %v want %v", got, want)
		}
	}
}

// TestPlan_PersonColumnSkippedWhenEmpty 验证工号不可解析/未声明来源列时该列被清空，
// 不参与 diff，也不因人员列触发更新（preparePersonFields 会把这类列置空）。
func TestPlan_PersonColumnSkippedWhenEmpty(t *testing.T) {
	in := personPlanInput()
	in.Rows = []Row{
		{"工号": "GZ000500", "直接上级": ""}, // 解析不到 open_id → 置空
	}
	in.Existing = map[string]ExistingRecord{
		"GZ000500": {
			RecordID:  "rec1",
			Fields:    Row{"工号": "GZ000500", "直接上级": "黄春越"},
			PersonIDs: map[string][]string{"直接上级": {"ou_huang"}},
		},
	}
	p := Plan(in)
	if p.Frozen != 1 || len(p.Updates) != 0 {
		t.Fatalf("空人员列应跳过且冻结，got frozen=%d updates=%d", p.Frozen, len(p.Updates))
	}
}

// TestPlan_PersonColumnCreatesWithResolvedIDs 验证新增行的人员列同样经 op.Persons 旁路
// 携带已解析 open_id 集合（而非工号文本），且多工号拼接值按逗号拆分后合并去重。
func TestPlan_PersonColumnCreatesWithResolvedIDs(t *testing.T) {
	in := personPlanInput()
	in.Rows = []Row{
		{"工号": "GZ000600", "直接上级": "GZ000040, GZ000500"},
	}
	in.Existing = map[string]ExistingRecord{}
	p := Plan(in)
	if len(p.Creates) != 1 {
		t.Fatalf("应产生 1 条新增，got %+v", p)
	}
	if _, ok := p.Creates[0].Fields["直接上级"]; ok {
		t.Fatalf("人员列不应进入 Fields，got %v", p.Creates[0].Fields)
	}
	got := p.Creates[0].Persons["直接上级"]
	want := []string{"ou_huang", "ou_huang_alt", "ou_lixi"}
	if len(got) != len(want) {
		t.Fatalf("多工号应合并为 3 个 open_id，got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("open_id 顺序/内容: got %v want %v", got, want)
		}
	}
}
