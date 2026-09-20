// config_test.go 验证配置层的 sys_config 解析行为，重点覆盖 FB-6 三表分离：
//   - 面试表 / 报表表 table id 单独配置时各自生效；
//   - 二者未配置时回退到候选人决策表 table id（candidate_bitable_table_id），保持单表兼容；
//   - 候选人决策表 table id 始终来自 candidate_bitable_table_id。
//
// 测试自包含且顺序无关：每个测试自行构造 sys_config 替身，不依赖共享状态或网络。
package config

import (
	"context"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/capmodel"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// fakeSysConfig 是 hostconfigcap.SysConfigService 的最小替身，
// 仅按预置 values 返回 Get 结果，其余写操作在配置读取路径中不会被触达。
type fakeSysConfig struct {
	values map[hostconfigcap.SysConfigKey]string
}

func (f *fakeSysConfig) Get(_ context.Context, key hostconfigcap.SysConfigKey) (*hostconfigcap.SysConfigInfo, error) {
	if v, ok := f.values[key]; ok {
		return &hostconfigcap.SysConfigInfo{Key: key, Value: v}, nil
	}
	return nil, nil
}

func (f *fakeSysConfig) BatchGet(_ context.Context, keys []hostconfigcap.SysConfigKey) (*capmodel.BatchResult[*hostconfigcap.SysConfigInfo, hostconfigcap.SysConfigKey], error) {
	out := &capmodel.BatchResult[*hostconfigcap.SysConfigInfo, hostconfigcap.SysConfigKey]{
		Items: map[hostconfigcap.SysConfigKey]*hostconfigcap.SysConfigInfo{},
	}
	for _, key := range keys {
		if v, ok := f.values[key]; ok {
			out.Items[key] = &hostconfigcap.SysConfigInfo{Key: key, Value: v}
		} else {
			out.MissingIDs = append(out.MissingIDs, key)
		}
	}
	return out, nil
}

func (f *fakeSysConfig) List(context.Context, hostconfigcap.ListSysConfigInput) (*capmodel.PageResult[*hostconfigcap.SysConfigInfo], error) {
	return &capmodel.PageResult[*hostconfigcap.SysConfigInfo]{}, nil
}

func (f *fakeSysConfig) SetValue(_ context.Context, key hostconfigcap.SysConfigKey, value string, _ *hostconfigcap.SetSysConfigValueOptions) error {
	f.values[key] = value
	return nil
}

func (f *fakeSysConfig) BatchSetValue(_ context.Context, items []hostconfigcap.SetSysConfigValueItem, _ *hostconfigcap.SetSysConfigValueOptions) error {
	for _, item := range items {
		f.values[item.Key] = item.Value
	}
	return nil
}

func (f *fakeSysConfig) Reset(context.Context, hostconfigcap.SysConfigKey) error { return nil }

func (f *fakeSysConfig) EnsureVisible(context.Context, []hostconfigcap.SysConfigKey) error {
	return nil
}

// fakeHostConfig 是 hostconfigcap.Service 的最小替身，静态读取返回默认值，
// SysConfig() 返回预置的 fakeSysConfig，供 loadSysConfig 消费。
type fakeHostConfig struct {
	sc *fakeSysConfig
}

func (f *fakeHostConfig) Get(_ context.Context, _ string, defaultValue any) (*gvar.Var, error) {
	return gvar.New(defaultValue), nil
}
func (f *fakeHostConfig) Exists(context.Context, string) (bool, error) { return false, nil }
func (f *fakeHostConfig) String(_ context.Context, _ string, defaultValue string) (string, error) {
	return defaultValue, nil
}
func (f *fakeHostConfig) Bool(_ context.Context, _ string, defaultValue bool) (bool, error) {
	return defaultValue, nil
}
func (f *fakeHostConfig) Int(_ context.Context, _ string, defaultValue int) (int, error) {
	return defaultValue, nil
}
func (f *fakeHostConfig) Duration(_ context.Context, _ string, defaultValue time.Duration) (time.Duration, error) {
	return defaultValue, nil
}
func (f *fakeHostConfig) SysConfig() hostconfigcap.SysConfigService { return f.sc }

// newHostConfig 用给定的 sys_config 键值对构造 fakeHostConfig。
func newHostConfig(values map[hostconfigcap.SysConfigKey]string) *fakeHostConfig {
	return &fakeHostConfig{sc: &fakeSysConfig{values: values}}
}

// TestLoadSysConfig_SeparateTableIDs 验证三张表分别配置时各自生效（FB-6）。
func TestLoadSysConfig_SeparateTableIDs(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyCandidateBitableTableID: "tblCandidate",
		keyInterviewBitableTableID: "tblInterview",
		keyReportBitableTableID:    "tblReport",
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if cfg.CandidateBitableTableID != "tblCandidate" {
		t.Fatalf("候选人决策表 table id: got=%q want=tblCandidate", cfg.CandidateBitableTableID)
	}
	if cfg.InterviewBitableTableID != "tblInterview" {
		t.Fatalf("面试状态表 table id: got=%q want=tblInterview", cfg.InterviewBitableTableID)
	}
	if cfg.ReportBitableTableID != "tblReport" {
		t.Fatalf("报表评分表 table id: got=%q want=tblReport", cfg.ReportBitableTableID)
	}
}

// TestLoadSysConfig_TableIDFallback 验证面试表未配置时回退候选人决策表（保持单表部署兼容），
// 而报表评分表未配置时不回退、保持空串，由 RunReportSync 跳过本轮以避免误写候选人决策表。
func TestLoadSysConfig_TableIDFallback(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyCandidateBitableTableID: "tblCandidate",
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if cfg.InterviewBitableTableID != "tblCandidate" {
		t.Fatalf("面试状态表未配置应回退候选人表: got=%q want=tblCandidate", cfg.InterviewBitableTableID)
	}
	if cfg.ReportBitableTableID != "" {
		t.Fatalf("报表评分表未配置应保持空串(不回退): got=%q want=\"\"", cfg.ReportBitableTableID)
	}
}

// TestLoadSysConfig_PartialTableIDOverride 验证仅覆盖其中一张表时，
// 被覆盖的表用自身配置、未覆盖的表回退候选人表（FB-6）。
func TestLoadSysConfig_PartialTableIDOverride(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyCandidateBitableTableID: "tblCandidate",
		keyReportBitableTableID:    "tblReport",
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if cfg.InterviewBitableTableID != "tblCandidate" {
		t.Fatalf("面试状态表未配置应回退候选人表: got=%q want=tblCandidate", cfg.InterviewBitableTableID)
	}
	if cfg.ReportBitableTableID != "tblReport" {
		t.Fatalf("报表评分表已配置应生效: got=%q want=tblReport", cfg.ReportBitableTableID)
	}
}

// TestLoadSysConfig_FieldMappingMerge 验证 sys_config field_mapping 是覆盖式合并而非整体替换：
// 运营只配了需求1 的候选人列（覆盖 name、新增自定义键）时，需求2 的组合键列
// （applicationId/interview_round）仍保留 defaultFieldMapping 的默认值，
// 避免面试同步因缺列在守卫处提前返回。
func TestLoadSysConfig_FieldMappingMerge(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyFieldMapping: `{"name":"姓名列改名","custom":"自定义列"}`,
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	// 运营覆盖的键生效。
	if got := cfg.FieldMapping[FieldKeyName]; got != "姓名列改名" {
		t.Fatalf("name 列应被 sys_config 覆盖: got=%q want=姓名列改名", got)
	}
	// 运营新增的键并入。
	if got := cfg.FieldMapping["custom"]; got != "自定义列" {
		t.Fatalf("自定义键应并入: got=%q want=自定义列", got)
	}
	// 未覆盖的需求2 组合键列保留默认值（回归本次修复的核心）。
	if got := cfg.FieldMapping[FieldKeyApplicationID]; got != defaultFieldMapping[FieldKeyApplicationID] {
		t.Fatalf("applicationId 列应保留默认值: got=%q want=%q", got, defaultFieldMapping[FieldKeyApplicationID])
	}
	if got := cfg.FieldMapping[FieldKeyInterviewRound]; got != defaultFieldMapping[FieldKeyInterviewRound] {
		t.Fatalf("interview_round 列应保留默认值: got=%q want=%q", got, defaultFieldMapping[FieldKeyInterviewRound])
	}
}

// TestDefaultFieldMapping_IncludesResumeOwner 守护 FB-17：defaultFieldMapping 含
// resume_owner 键（默认「简历归属者」），供候选人轮询任务与 Bitable 人员列写入。
func TestDefaultFieldMapping_IncludesResumeOwner(t *testing.T) {
	if got, ok := defaultFieldMapping[FieldKeyResumeOwner]; !ok {
		t.Fatalf("defaultFieldMapping 缺少 %q 键", FieldKeyResumeOwner)
	} else if got != "简历归属者" {
		t.Fatalf("resume_owner 默认列名: got=%q want=简历归属者", got)
	}
}

// TestLoadSysConfig_FieldMappingDefaultNotMutated 验证合并不会污染包级 defaultFieldMapping 全局变量：
// 一次带 field_mapping 的加载后，defaultFieldMapping 中被覆盖的键仍为原始默认值，
// 保证后续加载（含未配置 field_mapping 的场景）拿到干净的默认底座。
func TestLoadSysConfig_FieldMappingDefaultNotMutated(t *testing.T) {
	original := defaultFieldMapping[FieldKeyName]
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyFieldMapping: `{"name":"临时改名"}`,
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if got := defaultFieldMapping[FieldKeyName]; got != original {
		t.Fatalf("defaultFieldMapping 全局不应被合并污染: got=%q want=%q", got, original)
	}
}

// TestLoadSysConfig_ReportFieldMappingMerge 验证 FB-12：需求3 报表列映射从独立 sys_config 键
// report_field_mapping 覆盖式合并。key 为 Moka 报表列标题（源）、value 为飞书目标列名；
// 运营只改一列（覆盖「最终总分」的目标列名）时，其余列（申请/候选人/匹配度定级备份）保留默认映射。
func TestLoadSysConfig_ReportFieldMappingMerge(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyReportFieldMapping: `{"最终总分":"综合评分","自定义源列":"自定义目标列"}`,
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	// 运营覆盖的键生效。
	if got := cfg.ReportFieldMapping["最终总分"]; got != "综合评分" {
		t.Fatalf("最终总分 目标列应被覆盖: got=%q want=综合评分", got)
	}
	// 运营新增的键并入。
	if got := cfg.ReportFieldMapping["自定义源列"]; got != "自定义目标列" {
		t.Fatalf("自定义源列应并入: got=%q want=自定义目标列", got)
	}
	// 未覆盖的键保留默认映射。
	if got := cfg.ReportFieldMapping[ReportSourceApplicationTitle]; got != defaultReportFieldMapping[ReportSourceApplicationTitle] {
		t.Fatalf("「申请」列应保留默认值: got=%q want=%q", got, defaultReportFieldMapping[ReportSourceApplicationTitle])
	}
	if got := cfg.ReportFieldMapping["候选人"]; got != defaultReportFieldMapping["候选人"] {
		t.Fatalf("「候选人」列应保留默认值: got=%q want=%q", got, defaultReportFieldMapping["候选人"])
	}
}

// TestLoadSysConfig_ReportFieldMappingDefaultNotMutated 验证合并不污染包级 defaultReportFieldMapping。
func TestLoadSysConfig_ReportFieldMappingDefaultNotMutated(t *testing.T) {
	original := defaultReportFieldMapping["候选人"]
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyReportFieldMapping: `{"候选人":"临时目标列"}`,
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if got := defaultReportFieldMapping["候选人"]; got != original {
		t.Fatalf("defaultReportFieldMapping 全局不应被合并污染: got=%q want=%q", got, original)
	}
}

// TestLoadSysConfig_OwnerKeys 验证 FB-18 新增的三个归属者 sys_config 键（需求1.4）：
// owner_report_id 解析为 int64、owner_email_column 覆盖默认值、
// owner_email_mapping 的 key（HR 邮箱）在存入时做 TrimSpace+ToLower 归一化。
func TestLoadSysConfig_OwnerKeys(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{
		keyOwnerReportID:     "2001",
		keyOwnerEmailColumn:  "HR邮箱",
		keyOwnerEmailMapping: `{"  HR1@X.CoM  ":"GZ001","hr2@x.com":"GZ002"}`,
	})
	cfg := &Config{}
	loadSysConfig(context.Background(), hc, cfg)

	if cfg.OwnerReportID != 2001 {
		t.Fatalf("owner_report_id: got=%d want=2001", cfg.OwnerReportID)
	}
	if cfg.OwnerEmailColumn != "HR邮箱" {
		t.Fatalf("owner_email_column 应被覆盖: got=%q want=HR邮箱", cfg.OwnerEmailColumn)
	}
	// key 归一化：带大小写与前后空格的邮箱应以小写去空格形式存入。
	if got := cfg.OwnerEmailMapping["hr1@x.com"]; got != "GZ001" {
		t.Fatalf("owner_email_mapping key 应归一化为 hr1@x.com: got=%q want=GZ001 (map=%v)", got, cfg.OwnerEmailMapping)
	}
	if got := cfg.OwnerEmailMapping["hr2@x.com"]; got != "GZ002" {
		t.Fatalf("owner_email_mapping hr2@x.com: got=%q want=GZ002", got)
	}
	if _, ok := cfg.OwnerEmailMapping["  HR1@X.CoM  "]; ok {
		t.Fatalf("原始未归一化 key 不应保留: %v", cfg.OwnerEmailMapping)
	}
}

// TestLoadSysConfig_OwnerKeysUnconfigured 验证归属者三键未配置时的降级口径：
// owner_report_id 保持 0（loadOwnerMap 据此跳过归属者取值与回填）、owner_email_mapping 保持 nil，
// 且 loadSysConfig 不覆盖 Load 已设好的 owner_email_column 默认值（「简历接收邮箱」）。
func TestLoadSysConfig_OwnerKeysUnconfigured(t *testing.T) {
	hc := newHostConfig(map[hostconfigcap.SysConfigKey]string{})
	// 模拟 Load 已铺好默认值的初始状态。
	cfg := &Config{OwnerEmailColumn: defaultOwnerEmailColumn}
	loadSysConfig(context.Background(), hc, cfg)

	if cfg.OwnerReportID != 0 {
		t.Fatalf("owner_report_id 未配置应为 0: got=%d", cfg.OwnerReportID)
	}
	if len(cfg.OwnerEmailMapping) != 0 {
		t.Fatalf("owner_email_mapping 未配置应为空: got=%v", cfg.OwnerEmailMapping)
	}
	if cfg.OwnerEmailColumn != defaultOwnerEmailColumn {
		t.Fatalf("owner_email_column 未配置应保留默认值: got=%q want=%q", cfg.OwnerEmailColumn, defaultOwnerEmailColumn)
	}
}

// 各表字段名无法统一（匹配度等级-初/中/高），默认映射把这三个源列都指向飞书同一列「匹配度等级」，
// 且不再包含旧键「匹配度定级备份」。
func TestDefaultReportFieldMapping_MatchLevelThreeSources(t *testing.T) {
	for _, src := range []string{"匹配度等级-初", "匹配度等级-中", "匹配度等级-高"} {
		if got := defaultReportFieldMapping[src]; got != "匹配度等级" {
			t.Fatalf("默认映射 %q 应指向飞书「匹配度等级」, got=%q", src, got)
		}
	}
	if _, ok := defaultReportFieldMapping["匹配度定级备份"]; ok {
		t.Fatalf("默认映射不应再包含旧键「匹配度定级备份」")
	}
}
