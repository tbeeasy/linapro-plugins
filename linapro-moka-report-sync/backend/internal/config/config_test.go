package config

import (
	"encoding/json"
	"testing"
)

// TestLoadMappings_Defaults 验证 loadMappings 对缺省字段的归一逻辑。
// 直接测试 applyMappingDefaults（从 loadMappings 中提取的纯函数），
// 避免依赖 SysConfig 基础设施。
func TestApplyMappingDefaults_UniqueFieldsEmpty(t *testing.T) {
	m := ReportMapping{}
	applyMappingDefaults(&m)
	if len(m.UniqueFields) != 1 || m.UniqueFields[0] != defaultUniqueField {
		t.Errorf("uniqueFields 为空时应归一为 [%q], got %v", defaultUniqueField, m.UniqueFields)
	}
}

func TestApplyMappingDefaults_KeySeparatorEmpty(t *testing.T) {
	m := ReportMapping{}
	applyMappingDefaults(&m)
	if m.KeySeparator != defaultKeySeparator {
		t.Errorf("keySeparator 为空时应归一为 %q, got %q", defaultKeySeparator, m.KeySeparator)
	}
}

func TestApplyMappingDefaults_SourceEmpty(t *testing.T) {
	m := ReportMapping{}
	applyMappingDefaults(&m)
	if m.Source != SourceHCM {
		t.Errorf("source 为空时应归一为 %q, got %q", SourceHCM, m.Source)
	}
}

func TestApplyMappingDefaults_SingleColumnPreserved(t *testing.T) {
	m := ReportMapping{UniqueFields: []string{"员工编号"}, KeySeparator: "_"}
	applyMappingDefaults(&m)
	if len(m.UniqueFields) != 1 || m.UniqueFields[0] != "员工编号" {
		t.Errorf("已有 uniqueFields 不应被覆盖, got %v", m.UniqueFields)
	}
	if m.KeySeparator != "_" {
		t.Errorf("已有 keySeparator 不应被覆盖, got %q", m.KeySeparator)
	}
}

func TestApplyMappingDefaults_MultiColumnPreserved(t *testing.T) {
	m := ReportMapping{UniqueFields: []string{"工号", "考勤月"}, KeySeparator: "|"}
	applyMappingDefaults(&m)
	if len(m.UniqueFields) != 2 {
		t.Errorf("多列 uniqueFields 不应被修改, got %v", m.UniqueFields)
	}
}

func TestApplyMappingDefaults_WithDerivedColumns(t *testing.T) {
	m := ReportMapping{
		DerivedColumns: []DerivedColumn{
			{Kind: "date", Sources: []string{"入职年月"}, Targets: map[string]string{"年": "2006", "月": "01"}},
		},
	}
	applyMappingDefaults(&m)
	// 归一后 uniqueFields/keySeparator 应填充默认值，derivedColumns 保持不变。
	if len(m.UniqueFields) != 1 || m.UniqueFields[0] != defaultUniqueField {
		t.Errorf("含 derivedColumns 时仍应归一 uniqueFields, got %v", m.UniqueFields)
	}
	if len(m.DerivedColumns) != 1 {
		t.Errorf("derivedColumns 不应被修改, got %v", m.DerivedColumns)
	}
}

// TestApplyMappingDefaults_ShapeEmpty 验证 shape 为空时归一为 "flat"。
func TestApplyMappingDefaults_ShapeEmpty(t *testing.T) {
	m := ReportMapping{}
	applyMappingDefaults(&m)
	if m.Shape != ShapeFlat {
		t.Errorf("shape 为空时应归一为 %q, got %q", ShapeFlat, m.Shape)
	}
}

// TestApplyMappingDefaults_ShapeFlatPreserved 验证已声明 "flat" 时不被覆盖。
func TestApplyMappingDefaults_ShapeFlatPreserved(t *testing.T) {
	m := ReportMapping{Shape: ShapeFlat}
	applyMappingDefaults(&m)
	if m.Shape != ShapeFlat {
		t.Errorf("shape=flat 不应被覆盖, got %q", m.Shape)
	}
}

// TestApplyMappingDefaults_ShapePivotPreserved 验证已声明 "pivot" 时不被覆盖。
func TestApplyMappingDefaults_ShapePivotPreserved(t *testing.T) {
	m := ReportMapping{Shape: ShapePivot, PivotHeaderColumn: "招聘漏斗图"}
	applyMappingDefaults(&m)
	if m.Shape != ShapePivot {
		t.Errorf("shape=pivot 不应被覆盖, got %q", m.Shape)
	}
	if m.PivotHeaderColumn != "招聘漏斗图" {
		t.Errorf("pivotHeaderColumn 不应被修改, got %q", m.PivotHeaderColumn)
	}
}

// TestApplyMappingDefaults_PivotFieldsParsed 验证 pivot 相关字段可正确解析并通过缺省归一。
// 源列名（pivotHeaderColumn=公共日期）与输出索引列名（pivotIndexColumn=招聘漏斗图）解耦，
// 二者均按原样保留、不被缺省归一改写；pivotIndexColumn 不回退到 pivotHeaderColumn。
func TestApplyMappingDefaults_PivotFieldsParsed(t *testing.T) {
	m := ReportMapping{
		Shape:             ShapePivot,
		PivotHeaderColumn: "公共日期",
		PivotIndexColumn:  "招聘漏斗图",
		UniqueFields:      []string{"招聘漏斗图"},
	}
	applyMappingDefaults(&m)
	if m.Shape != ShapePivot {
		t.Errorf("shape 应保持 pivot, got %q", m.Shape)
	}
	if m.PivotHeaderColumn != "公共日期" {
		t.Errorf("pivotHeaderColumn 应保持 公共日期, got %q", m.PivotHeaderColumn)
	}
	if m.PivotIndexColumn != "招聘漏斗图" {
		t.Errorf("pivotIndexColumn 应保持 招聘漏斗图, got %q", m.PivotIndexColumn)
	}
	// 已声明 uniqueFields 时不应被覆盖。
	if len(m.UniqueFields) != 1 || m.UniqueFields[0] != "招聘漏斗图" {
		t.Errorf("uniqueFields 不应被覆盖, got %v", m.UniqueFields)
	}
}

// TestReportMapping_SkipIfUnmarshal 验证 skipIf 的 JSON 反序列化：多条规则各字段就位，
// 且字段名拼写正确（format 而非 formate）。
func TestReportMapping_SkipIfUnmarshal(t *testing.T) {
	raw := `{
		"reportId": 100,
		"appToken": "tok",
		"tableId": "tbl",
		"skipIf": [
			{"kind": "before", "source": "申请时间", "format": "Y-m", "value": "2026-01"},
			{"kind": "before", "source": "招聘月份", "format": "Y-m", "value": "2025-06"}
		]
	}`
	var m ReportMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("反序列化 skipIf 失败: %v", err)
	}
	if len(m.SkipIf) != 2 {
		t.Fatalf("应解析出 2 条 skipIf 规则, got %d", len(m.SkipIf))
	}
	first := m.SkipIf[0]
	if first.Kind != "before" || first.Source != "申请时间" || first.Format != "Y-m" || first.Value != "2026-01" {
		t.Errorf("首条规则字段解析有误: %+v", first)
	}
	if m.SkipIf[1].Source != "招聘月份" || m.SkipIf[1].Value != "2025-06" {
		t.Errorf("次条规则字段解析有误: %+v", m.SkipIf[1])
	}
}

// TestReportMapping_SkipIfSpellingSensitive 验证字段名拼写敏感：错写成 formate 时
// Format 为空（JSON 反序列化对未知字段静默忽略），据此提醒配置侧必须写 format。
func TestReportMapping_SkipIfSpellingSensitive(t *testing.T) {
	raw := `{"skipIf": [{"kind": "before", "source": "申请时间", "formate": "Y-m", "value": "2026-01"}]}`
	var m ReportMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(m.SkipIf) != 1 {
		t.Fatalf("应解析出 1 条规则, got %d", len(m.SkipIf))
	}
	if m.SkipIf[0].Format != "" {
		t.Errorf("错写成 formate 时 Format 应为空, got %q", m.SkipIf[0].Format)
	}
}

// TestReportMapping_SkipIfAbsent 验证未提供 skipIf 时为空切片（nil），不做行过滤。
func TestReportMapping_SkipIfAbsent(t *testing.T) {
	raw := `{"reportId": 1, "appToken": "t", "tableId": "b"}`
	var m ReportMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(m.SkipIf) != 0 {
		t.Errorf("未提供 skipIf 时应为空, got %v", m.SkipIf)
	}
}
