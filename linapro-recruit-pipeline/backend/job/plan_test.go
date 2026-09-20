package job

import (
	"fmt"
	"testing"

	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// fieldTypesFixture 是规划器测试共用的飞书列类型：applicationId 文本键、评分数字、
// 面试时间日期、面试官人员、其余文本。
func fieldTypesFixture() map[string]int {
	return map[string]int{
		"applicationId": larkbitablesdk.TypeText,
		"评分":            larkbitablesdk.TypeNumber,
		"面试时间":          larkbitablesdk.TypeDateTime,
		"面试官":           larkbitablesdk.TypeUser,
		"面试方式":          larkbitablesdk.TypeText,
		"未应约原因":         larkbitablesdk.TypeText,
	}
}

// TestPlanDiff_UnchangedFreezes 验证全列相等时冻结、不产出更新。
func TestPlanDiff_UnchangedFreezes(t *testing.T) {
	existing := map[string]existingRow{
		"101": {recordID: "r1", fields: lark.Row{"applicationId": "101", "面试方式": "视频面试"}},
	}
	desired := []desiredRow{
		{key: "101", fields: lark.WriteRow{"applicationId": "101", "面试方式": "视频面试"}},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if len(got.creates) != 0 || len(got.updates) != 0 || got.frozen != 1 {
		t.Fatalf("全列相等应冻结, got creates=%d updates=%d frozen=%d", len(got.creates), len(got.updates), got.frozen)
	}
}

// TestPlanDiff_SingleColumnOnlyWritesThatColumn 验证仅一列变更时更新只含该列。
func TestPlanDiff_SingleColumnOnlyWritesThatColumn(t *testing.T) {
	existing := map[string]existingRow{
		"101": {recordID: "r1", fields: lark.Row{"applicationId": "101", "面试方式": "现场面试", "未应约原因": ""}},
	}
	desired := []desiredRow{
		{key: "101", fields: lark.WriteRow{"applicationId": "101", "面试方式": "视频面试", "未应约原因": ""}},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if len(got.updates) != 1 {
		t.Fatalf("应产出 1 条更新, got=%d", len(got.updates))
	}
	u := got.updates[0]
	if u.RecordID != "r1" {
		t.Errorf("RecordID 应为 r1, got=%q", u.RecordID)
	}
	if len(u.Fields) != 1 || u.Fields["面试方式"] != "视频面试" {
		t.Errorf("更新应只含面试方式一列, got=%v", u.Fields)
	}
}

// TestPlanDiff_PersonSetEquivalentFreezes 验证人员列 open_id 集合等价（去重、顺序无关）时冻结。
func TestPlanDiff_PersonSetEquivalentFreezes(t *testing.T) {
	existing := map[string]existingRow{
		"101": {
			recordID:  "r1",
			fields:    lark.Row{"applicationId": "101"},
			personIDs: map[string][]string{"面试官": {"ou_a", "ou_b"}},
		},
	}
	desired := []desiredRow{
		{
			key:     "101",
			fields:  lark.WriteRow{"applicationId": "101"},
			persons: map[string][]string{"面试官": {"ou_b", "ou_a", "ou_a"}}, // 顺序不同 + 重复
		},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if got.frozen != 1 || len(got.updates) != 0 {
		t.Fatalf("open_id 集合等价应冻结, got updates=%d frozen=%d", len(got.updates), got.frozen)
	}
}

// TestPlanDiff_PersonSetChangedUpdates 验证人员列集合变化时纳入人员差异更新。
func TestPlanDiff_PersonSetChangedUpdates(t *testing.T) {
	existing := map[string]existingRow{
		"101": {
			recordID:  "r1",
			fields:    lark.Row{"applicationId": "101"},
			personIDs: map[string][]string{"面试官": {"ou_a"}},
		},
	}
	desired := []desiredRow{
		{
			key:     "101",
			fields:  lark.WriteRow{"applicationId": "101"},
			persons: map[string][]string{"面试官": {"ou_a", "ou_c"}},
		},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if len(got.updates) != 1 {
		t.Fatalf("人员集合变化应产出更新, got=%d", len(got.updates))
	}
	if ids := got.updates[0].Persons["面试官"]; len(ids) != 2 {
		t.Errorf("更新应含新的 open_id 集合, got=%v", ids)
	}
}

// TestPlanDiff_DateEquivalentFreezes 验证日期列按 int64 毫秒等价时冻结，且消化回读侧
// 科学计数法：飞书数字型日期回读经 %v 字符串化为 "1.7065224e+12" 形态，desired 为 ISO 文本。
func TestPlanDiff_DateEquivalentFreezes(t *testing.T) {
	ms, _ := lark.ParseEpochMillisUTC("2026-09-20T10:00:00Z")
	// 回读形态：飞书数字日期经 %v 字符串化，大整数呈科学计数法。
	readback := fmtSprintfV(float64(ms))
	existing := map[string]existingRow{
		"101": {recordID: "r1", fields: lark.Row{"applicationId": "101", "面试时间": readback}},
	}
	desired := []desiredRow{
		{key: "101", fields: lark.WriteRow{"applicationId": "101", "面试时间": "2026-09-20T10:00:00Z"}},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if got.frozen != 1 || len(got.updates) != 0 {
		t.Fatalf("日期毫秒等价（含科学计数法回读）应冻结, got updates=%d frozen=%d", len(got.updates), got.frozen)
	}
}

// TestPlanDiff_NumberEquivalentFreezes 验证数字列按 float64 等价（回读 "90" vs 报表 "90.0"）时冻结。
func TestPlanDiff_NumberEquivalentFreezes(t *testing.T) {
	existing := map[string]existingRow{
		"101": {recordID: "r1", fields: lark.Row{"applicationId": "101", "评分": "90"}},
	}
	desired := []desiredRow{
		{key: "101", fields: lark.WriteRow{"applicationId": "101", "评分": "90.0"}},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if got.frozen != 1 || len(got.updates) != 0 {
		t.Fatalf("数字 float64 等价应冻结, got updates=%d frozen=%d", len(got.updates), got.frozen)
	}
}

// TestPlanDiff_EmptyStringClearsColumn 验证空串=显式清列产生更新（不做 v=="" 空值过滤）。
func TestPlanDiff_EmptyStringClearsColumn(t *testing.T) {
	existing := map[string]existingRow{
		"101": {recordID: "r1", fields: lark.Row{"applicationId": "101", "未应约原因": "候选人放弃"}},
	}
	desired := []desiredRow{
		{key: "101", fields: lark.WriteRow{"applicationId": "101", "未应约原因": ""}}, // 转已应约 → 清列
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if len(got.updates) != 1 {
		t.Fatalf("空串清列应产出更新, got=%d", len(got.updates))
	}
	if v, ok := got.updates[0].Fields["未应约原因"]; !ok || v != "" {
		t.Errorf("更新应把未应约原因写为空串, got=%v ok=%v", v, ok)
	}
}

// TestPlanDiff_MissingKeyCreates 验证未命中唯一键时新建（全列 + 人员旁路）。
func TestPlanDiff_MissingKeyCreates(t *testing.T) {
	existing := map[string]existingRow{}
	desired := []desiredRow{
		{
			key:     "999",
			fields:  lark.WriteRow{"applicationId": "999", "面试方式": "视频面试"},
			persons: map[string][]string{"面试官": {"ou_x"}},
		},
	}
	got := planDiff(desired, existing, []string{"applicationId"}, fieldTypesFixture())
	if len(got.creates) != 1 {
		t.Fatalf("未命中应新建, got=%d", len(got.creates))
	}
	c := got.creates[0]
	if c.Fields["applicationId"] != "999" || c.Fields["面试方式"] != "视频面试" {
		t.Errorf("新建应含全列, got=%v", c.Fields)
	}
	if len(c.Persons["面试官"]) != 1 {
		t.Errorf("新建应含人员旁路, got=%v", c.Persons)
	}
}

// TestPersonSetEqual 验证集合判等：去重、顺序无关、忽略空 id、双方空集相等。
func TestPersonSetEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"双方空集", nil, nil, true},
		{"空集 vs 仅空串", nil, []string{"", ""}, true},
		{"顺序无关去重", []string{"a", "b", "a"}, []string{"b", "a"}, true},
		{"成员不同", []string{"a"}, []string{"a", "c"}, false},
		{"忽略空串", []string{"a", ""}, []string{"a"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := personSet(tc.a).equal(tc.b); got != tc.want {
				t.Errorf("equal(%v,%v)=%v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestPersonIDsOf 验证从结构化人员值投影 open_id 集合：丢空 id、无有效 id 的列不入结果。
func TestPersonIDsOf(t *testing.T) {
	persons := map[string][]*larkbitablesdk.Person{
		"面试官": {
			larkbitablesdk.NewPersonBuilder().Id("ou_a").Name("张三").Build(),
			larkbitablesdk.NewPersonBuilder().Name("无id").Build(), // 无 id → 丢弃
		},
		"空列": {larkbitablesdk.NewPersonBuilder().Name("仅姓名").Build()}, // 无有效 id → 整列丢弃
	}
	got := personIDsOf(persons)
	if len(got["面试官"]) != 1 || got["面试官"][0] != "ou_a" {
		t.Errorf("面试官应仅含 ou_a, got=%v", got["面试官"])
	}
	if _, ok := got["空列"]; ok {
		t.Errorf("无有效 id 的列不应出现, got=%v", got)
	}
}

// fmtSprintfV 模拟共享库回读侧对数字/日期单元格的 %v 字符串化（大整数呈科学计数法）。
func fmtSprintfV(v float64) string {
	return fmt.Sprintf("%v", v)
}
