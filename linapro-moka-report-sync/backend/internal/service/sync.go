// Package service 编排一次报表→多维表格同步周期：加载配置，
// 然后针对每个报表映射拉取 Moka 报表（根据映射来源选择 HCM 或 Recruit 客户端），
// 结合交集列门闩对目标多维表格进行规划，并应用新增/更新。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"lina-core/pkg/logger"
	"strconv"
	"strings"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"lina-core/pkg/plugin/capability"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-moka-hcm/backend/hcm"
	recruitclient "lina-plugin-linapro-moka-recruit/backend/moka"

	lark "linapro-lark-sdk/larkbitable"

	"lina-plugin-linapro-moka-report-sync/backend/internal/config"
	"lina-plugin-linapro-moka-report-sync/backend/internal/syncer"
)

const logTag = "[linapro-moka-report-sync]"

// Runner 使用宿主机服务提供的配置执行同步周期。
type Runner struct {
	services capability.Services
}

// NewRunner 构建一个绑定到宿主机服务的 Runner。
func NewRunner(services capability.Services) *Runner {
	return &Runner{services: services}
}

// MappingResult 汇总单个报表映射的同步结果。
type MappingResult struct {
	ReportID int64
	Created  int
	Updated  int
	Frozen   int
	Skipped  int
	Filtered int // 被 skipIf 行过滤丢弃的行数（仅 flat 形态）
	Err      error
}

// RunOnce 加载配置并同步每一个已配置的报表映射。单个映射失败会被记录，
// 但不会中止其他映射的同步。
func (r *Runner) RunOnce(ctx context.Context) ([]MappingResult, error) {
	cfg, err := config.Load(ctx, r.services)
	if err != nil {
		return nil, err
	}
	if len(cfg.Mappings) == 0 {
		logger.Info(ctx, logTag, "未配置报表映射，本轮无需同步")
		return nil, nil
	}

	hcmClient := hcm.NewClient(cfg.MokaBase, cfg.MokaCred)
	// 人员字段写入需把工号解析为 open_id：周期开始时向 employee-core 按写表格的 lark_app_id
	// 作用域批量拉取一次「工号 → 全部 open_id」映射构成解析器（后续按行走内存查找，避免 N+1）。
	// open_id 按应用作用域签发，故必须传入 cfg.LarkApp 过滤；own+assoc、双租户同人多个
	// open_id 全返回全写入。用多工号版：一个人员单元格可能由多个工号表达（逗号拼接），
	// 合并与去重在 empcap（数据 owner）侧完成，本插件只做逗号拆分这一层输入格式归一。
	// 服务未绑定或查询失败时 resolver 为 nil —— 人员列会被跳过，不阻断本轮同步。
	resolver, err := empcap.LarkOpenIDResolverByEmployeeNos(ctx, cfg.LarkApp)
	if err != nil {
		logger.Warningf(ctx, "%s 加载工号→open_id 映射失败，人员列本轮跳过: %v", logTag, err)
	}
	// 注入 report-sync 的差异化读侧语义：把 Moka "-" 占位符归零的单元格兜底（回读对称），
	// 以及人员字段的 user_id_type（open_id 集合由人员列旁路传入，不经 Encoder）。
	// 日期/数字的写侧类型化不再经 Encoder：桥接层 toLarkCreates/toLarkUpdates 按 fieldTypes 把
	// 日期列的毫秒整数串转 int64、数字列经 ParseNumberLoose 转 float64 后直接放进 lark.Row，
	// 共享库只序列化。日期语义解析仍只在 normalizeColumns（Plan 前）对全部 TypeDateTime 列做一次
	// （文本/epoch → 东八区毫秒），写侧此时拿到的必是毫秒整数串，只需 strconv.ParseInt 转回 int64，
	// 无需二次日期解析，从而保证整条链路对同一日期只解析一次。
	larkClient := lark.NewClient(cfg.LarkApp, cfg.LarkAppKey).WithEncoder(lark.Encoder{
		CellFallback: syncer.Stringify,
		UserIDType:   lark.UserIDTypeOpenID,
	})

	// 仅当至少存在一个招聘类映射时才按需构建 recruit 客户端。
	// 招聘端点主力鉴权为 BasicAuth（apiKey），与 HCM 共用同一把组织级 moka apiKey
	// 及同一 Moka 主机地址，无需单独的 OAuth2 clientId/clientSecret。
	var recruitClient *recruitclient.Client
	for _, m := range cfg.Mappings {
		if m.Enable && m.Source == config.SourceRecruit {
			recruitClient = recruitclient.NewClient(cfg.MokaBase,
				recruitclient.NewBasicAuth(recruitclient.BasicCredential{
					APIKey: cfg.MokaCred.APIKey,
				}))
			break
		}
	}

	results := make([]MappingResult, 0, len(cfg.Mappings))
	for _, m := range cfg.Mappings {
		if !m.Enable {
			logger.Infof(ctx, "%s %s 跳过：已被配置禁用", logTag, mappingLabel(m))
			continue
		}
		res := r.syncMapping(ctx, hcmClient, recruitClient, larkClient, resolver, m)
		if res.Err != nil {
			logger.Errorf(ctx, "%s %s 同步失败: %v", logTag, mappingLabel(m), res.Err)
		} else {
			logger.Infof(ctx, "%s %s 同步完成：新增=%d 更新=%d 冻结=%d 跳过=%d 过滤=%d",
				logTag, mappingLabel(m), res.Created, res.Updated, res.Frozen, res.Skipped, res.Filtered)
		}
		results = append(results, res)
	}
	return results, nil
}

func mappingLabel(m config.ReportMapping) string {
	if m.Remark != "" {
		return fmt.Sprintf("%s(reportId=%d)", m.Remark, m.ReportID)
	}
	return fmt.Sprintf("reportId=%d", m.ReportID)
}

func (r *Runner) syncMapping(
	ctx context.Context,
	hcmClient *hcm.Client,
	recruitClient *recruitclient.Client,
	larkClient *lark.Client,
	resolver func(employeeNos []string) []string,
	m config.ReportMapping,
) MappingResult {
	res := MappingResult{ReportID: m.ReportID}

	// 从对应数据源拉取报表数据。
	var data *syncer.ReportData
	switch m.Source {
	case config.SourceRecruit:
		if recruitClient == nil {
			res.Err = fmt.Errorf("linapro-moka-report-sync: 映射 %s 未配置招聘凭据", mappingLabel(m))
			return res
		}
		raw, err := recruitClient.GetReportData(ctx, m.ReportID)
		if err != nil {
			res.Err = err
			return res
		}
		data = convertRecruitData(raw)
	default: // 来源为 HCM
		raw, err := hcmClient.GetReportData(ctx, m.ReportID)
		if err != nil {
			res.Err = err
			return res
		}
		data = convertHCMData(raw)
	}

	cols, rows := syncer.FlattenReport(data)

	// 按 shape 分派：每种形态封装为独立函数，新增形态时只需在此增加分支。
	switch m.Shape {
	case config.ShapePivot:
		return r.syncMappingPivot(ctx, larkClient, m, cols, rows, res)
	default: // ShapeFlat（缺省）
		return r.syncMappingFlat(ctx, larkClient, resolver, data, m, cols, rows, res)
	}
}

// syncMappingFlat 处理 shape=flat（缺省）的映射：报表一行对应 Bitable 一行，
// 依次执行列派生、行合并、日期归一、人员字段解析，再走现有 Plan/BatchCreate/BatchUpdate
// 写入 Bitable。data 仅用于 Debug 日志打印原始报表。
func (r *Runner) syncMappingFlat(
	ctx context.Context,
	larkClient *lark.Client,
	resolver func(employeeNos []string) []string,
	data *syncer.ReportData,
	m config.ReportMapping,
	cols []string,
	rows []syncer.Row,
	res MappingResult,
) MappingResult {
	// spec 由 m.UniqueFields + m.KeySeparator 构造，贯穿 CoalesceRows、ListRecords、Plan 三处。
	spec := syncer.KeySpec{Fields: m.UniqueFields, Sep: m.KeySeparator}

	// 列派生：在 Coalesce 之前执行，使派生列（如年/月）可作为键组成列参与后续匹配。
	// 派生顺序：Flatten → 列派生 → 行过滤 → Coalesce → Normalize → preparePersonFields → Plan。
	if len(m.DerivedColumns) > 0 {
		rules := toDeriveRules(ctx, mappingLabel(m), m.DerivedColumns)
		cols = syncer.ApplyDerivedColumns(rows, cols, rules)
	}

	// 行过滤：紧跟列派生之后、Coalesce 之前执行，使规则可引用派生列，且被丢弃的行不再走
	// 后续按行开销（合并、归一、人员解析）。「无法判定」的行保留并记 warn（见 skip.go 语义）。
	if len(m.SkipIf) > 0 {
		skipRules := toSkipRules(ctx, mappingLabel(m), m.SkipIf)
		var warns []syncer.SkipWarning
		var dropped int
		rows, dropped, warns = syncer.ApplySkipRules(rows, skipRules)
		if dropped > 0 {
			logger.Infof(ctx, "%s %s 行过滤丢弃 %d 行", logTag, mappingLabel(m), dropped)
		}
		for _, w := range warns {
			logger.Warningf(ctx, "%s %s 行过滤无法判定(kind=%s source=%q format=%q value=%q)：保留 %d 行，原因：%s",
				logTag, mappingLabel(m), w.Rule.Kind, w.Rule.Source, w.Rule.Format, w.Rule.Value, w.Rows, w.Why)
		}
		res.Filtered = dropped
	}

	// Moka 可能为同一个键值返回多行（例如一行完整数据
	// 加上一行全是 "-" 占位符的副本）。在规划前合并它们，避免生成重复记录；
	// 字段冲突会在下方上报，而不是被静默丢弃。
	rows, conflicts := syncer.CoalesceRows(rows, spec)
	for _, c := range conflicts {
		logger.Warningf(ctx, "%s %s 字段 %s 存在冲突(键 %s)：保留 %q, 丢弃 %q", logTag, mappingLabel(m), c.Field, c.Key, c.Kept, c.Dropped)
	}

	if raw, mErr := json.MarshalIndent(data, "", "  "); mErr == nil {
		logger.Debugf(ctx, "%s %s 原始报表数据:\n%s", logTag, mappingLabel(m), string(raw))
	}
	if raw, mErr := json.MarshalIndent(map[string]any{"cols": cols, "rows": rows}, "", "  "); mErr == nil {
		logger.Debugf(ctx, "%s %s 拍平后的列/行:\n%s", logTag, mappingLabel(m), string(raw))
	}

	table := lark.Table{AppToken: m.AppToken, TableID: m.TableID}
	fieldTypes, err := larkClient.ListFields(ctx, table)
	if err != nil {
		res.Err = err
		return res
	}

	tableFields := make(map[string]struct{}, len(fieldTypes))
	for name := range fieldTypes {
		tableFields[name] = struct{}{}
	}
	existing, err := larkClient.ListRecords(ctx, table, func(row lark.Row) string {
		return spec.KeyOf(syncer.Row(row))
	}, fieldTypes)
	if err != nil {
		res.Err = err
		return res
	}

	// 按字段类型对 Moka 侧的需规一列做归一，使规划比较与 Bitable 回读值严格对称：
	// 写侧把日期编码成毫秒、读侧把毫秒读回字符串，Moka 侧保留原始日期文本
	// 会在 diffFields 里与毫秒字符串比较永远不相等，导致同值每轮被误判为更新。
	// 当前仅日期列会归一，文本/数字/人员等列不受影响；解析失败的值保留原样。
	normalizeColumns(rows, fieldTypes)
	// 人员列必须在规划前把人员字段值替换为对应工号（逗号拼接多工号）：PersonFieldSources 声明
	// 「Bitable 人员字段名 → Moka 工号列名」；未配置的人员字段清空，使 Plan 忽略该列。
	// 工号能否解析为当前应用作用域的 open_id 由 Plan 内 resolvePersonCols 判断，不在此预解析。
	preparePersonFields(rows, fieldTypes, m.PersonFieldSources)

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  cols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    toSyncerExisting(existing),
		// 人员列按 open_id 集合比对：Moka 侧为工号、回读侧为姓名，文本比对永不相等会每轮空转
		// 重写。Plan 用同一解析器把工号解析为期望 open_id 集合，既判等、也作为要写入的集合
		// 产出到 op.Persons —— 全链路解析只此一次，写侧不再按工号二次解析。
		PersonCols:                    personColsOf(fieldTypes),
		ResolvePersonIDsByEmployeeNos: resolver,
		// 强类型比对：按目标表列类型判等（日期比毫秒、数字比 float64、其余比文本），
		// 修复数字列 "90" vs 回读 "90.0" 被误判为变更而空转重写。
		FieldTypes: fieldTypes,
	})

	if _, err := larkClient.BatchCreate(ctx, table, toLarkCreates(plan.Creates, fieldTypes), fieldTypes); err != nil {
		res.Err = err
		return res
	}
	if err := larkClient.BatchUpdate(ctx, table, toLarkUpdates(plan.Updates, fieldTypes), fieldTypes); err != nil {
		res.Err = err
		return res
	}

	res.Created = len(plan.Creates)
	res.Updated = len(plan.Updates)
	res.Frozen = plan.Frozen
	res.Skipped = plan.SkippedNoName
	return res
}

// toDeriveRules 把 config.DerivedColumn 列表转换为 syncer.DeriveRule 列表，
// 并把 larkbitable.ParseEpochMillisCST 注入到 date 类型规则的 Params["parse"]，
// 使派生层可复用已有日期解析器而无需在 syncer 包引入 larkbitable 依赖。
//
// 已知限制：split 目标列 ≥ 2 时，「切分结果第 i 段 → 第 i 个目标列」的对应关系依赖顺序，
// 但配置结构未提供显式顺序声明（Targets 为 map，JSON 反序列化后键序不稳定）。此时无法保证
// 稳定映射，会把切分值写错列（如「一级/二级」值对调），故跳过该规则并告警，避免污染数据。
// 单目标 split 顺序平凡稳定，正常放行；date/regex 按列名显式映射，与顺序无关，不受影响。
// 后续若确有多目标 split 需求，应为 DerivedColumn 增加显式 order 字段后再启用。
func toDeriveRules(ctx context.Context, label string, cols []config.DerivedColumn) []syncer.DeriveRule {
	rules := make([]syncer.DeriveRule, 0, len(cols))
	for _, c := range cols {
		// 多目标 split 无稳定顺序声明：跳过并告警（见函数注释的已知限制）。
		if c.Kind == "split" && len(c.Targets) >= 2 {
			logger.Warningf(ctx, "%s %s split 派生含多个目标列(%d)但无稳定顺序声明，本轮跳过该规则以避免写错列",
				logTag, label, len(c.Targets))
			continue
		}
		params := make(map[string]any, len(c.Targets)+2)
		// 通用参数：targets 供 date/regex 使用，by/pattern 供 split/regex 使用。
		params["targets"] = c.Targets
		if c.By != "" {
			params["by"] = c.By
		}
		if c.Pattern != "" {
			params["pattern"] = c.Pattern
		}
		// date 规则注入 ParseEpochMillisCST，保证年月等派生值与 NormalizeColumns 使用
		// 同一解析器，回读对称。
		if c.Kind == "date" {
			params["parse"] = lark.ParseEpochMillisCST
		}
		// split 规则：到此仅剩单目标（多目标已在上方跳过），order 即该唯一目标列，顺序平凡稳定。
		if c.Kind == "split" {
			order := make([]string, 0, len(c.Targets))
			for k := range c.Targets {
				order = append(order, k)
			}
			params["order"] = order
		}
		rules = append(rules, syncer.DeriveRule{
			Kind:    c.Kind,
			Sources: c.Sources,
			Targets: c.Targets,
			Params:  params,
		})
	}
	return rules
}

// toSkipRules 把 config.SkipRule 列表转换为 syncer.SkipRule 列表（config → syncer 类型桥接，
// 与 toDeriveRules 同构，syncer 包不反向依赖 config）。并对每条规则记一条 debug 日志，
// 标明 kind/source/format/value，便于排查「规则未按预期生效」。
func toSkipRules(ctx context.Context, label string, rules []config.SkipRule) []syncer.SkipRule {
	out := make([]syncer.SkipRule, 0, len(rules))
	for _, r := range rules {
		logger.Debugf(ctx, "%s %s 行过滤规则：kind=%s source=%q format=%q value=%q",
			logTag, label, r.Kind, r.Source, r.Format, r.Value)
		out = append(out, syncer.SkipRule{
			Kind:   r.Kind,
			Source: r.Source,
			Format: r.Format,
			Value:  r.Value,
		})
	}
	return out
}

// preparePersonFields 把人员字段的值换成其工号来源列的值，交给 Plan 统一解析为 open_id：
// 人员列不能直接把工号文本交给共享库（飞书人员字段只接受用户 id）。PersonFieldSources 声明
// 「Bitable 人员字段名 → Moka 报表工号列名」；未在 PersonFieldSources 中声明的人员字段直接
// 清空，不参与本轮写入。
//
// 工号能否解析到 open_id 不在此判断：Plan 内 resolvePersonCols 会对每个人员列解析一次，解析
// 为空的列自然不写（project 已把人员列排除出 Fields、resolvePersonCols 跳过空集合），与在此
// 预清空等价。故这里只做「取工号来源列的值」这一步搬运，不再调用解析器预解析一次——保证全链路
// 对同一工号只解析一次（解析器虽是闭包内存查找、二次调用廉价，但仍属重复的身份解析动作）。
func preparePersonFields(
	rows []syncer.Row,
	fieldTypes map[string]int,
	personFieldSources map[string]string,
) {
	for _, row := range rows {
		for fieldName := range row {
			if fieldTypes[fieldName] != larkbitablesdk.TypeUser {
				continue
			}
			empNoCol, declared := personFieldSources[fieldName]
			if !declared {
				// 未声明工号来源列的人员字段，本轮跳过。
				row[fieldName] = ""
				continue
			}
			// 把人员字段值替换为工号来源列的值，Plan 以工号查 open_id（空工号由 Plan 跳过）。
			row[fieldName] = row[empNoCol]
		}
	}
}

// normalizeColumns 根据 fieldTypes 对需要规一的列做归一（调用 syncer.NormalizeColumns），
// 把 Moka 侧原始值转成与 Bitable 回读一致的字符串。当前仅日期/时间列需要：写侧把日期写成
// 毫秒数字、读侧把毫秒读回字符串，Moka 侧若保留原始日期文本会在 diffFields 里与毫秒字符串
// 比较永远不相等，导致同值每轮被误判为更新。其他类型列（文本/数字/人员）不处理；
// 归一失败的值保留原样。
func normalizeColumns(rows []syncer.Row, fieldTypes map[string]int) {
	var cols []string
	for name, typ := range fieldTypes {
		if typ == larkbitablesdk.TypeDateTime {
			cols = append(cols, name)
		}
	}
	syncer.NormalizeColumns(rows, cols, lark.ParseEpochMillisCST)
}

// personColsOf 从字段类型表提取人员类型列集合，供 planner 对这些列做 open_id 集合比对。
func personColsOf(fieldTypes map[string]int) map[string]struct{} {
	var cols map[string]struct{}
	for name, typ := range fieldTypes {
		if typ == larkbitablesdk.TypeUser {
			if cols == nil {
				cols = make(map[string]struct{})
			}
			cols[name] = struct{}{}
		}
	}
	return cols
}

// toSyncerExisting 把共享库返回的 ExistingRecord 映射桥接为 planner 用的 syncer.ExistingRecord。
// 共享库回读的 Fields 是读 Row（map[string]string，与 syncer.Row 底层同构），直接类型转换即可；
// 同时把共享库回读的结构化人员值 Persons 投影为「列名 → open_id 切片」写入 PersonIDs，供 planner
// 对人员列做 open_id 集合比对（syncer 包不引入 SDK 类型，投影在此完成）。
func toSyncerExisting(src map[string]lark.ExistingRecord) map[string]syncer.ExistingRecord {
	out := make(map[string]syncer.ExistingRecord, len(src))
	for k, v := range src {
		out[k] = syncer.ExistingRecord{
			RecordID:  v.RecordID,
			Fields:    syncer.Row(v.Fields),
			PersonIDs: personIDsOf(v.Persons),
		}
	}
	return out
}

// personIDsOf 把共享库回读的结构化人员值投影为「列名 → open_id 切片」。
// 只取每个人员的 id（open_id），丢弃姓名等展示字段：人员列判等只由 id 集合决定。
// 无人员列或某列无有效 id 时返回 nil，与「无该列」语义一致，交给集合比对判等。
func personIDsOf(persons map[string][]*larkbitablesdk.Person) map[string][]string {
	if len(persons) == 0 {
		return nil
	}
	out := make(map[string][]string, len(persons))
	for col, ps := range persons {
		ids := make([]string, 0, len(ps))
		for _, p := range ps {
			if p == nil || p.Id == nil || *p.Id == "" {
				continue
			}
			ids = append(ids, *p.Id)
		}
		if len(ids) > 0 {
			out[col] = ids
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// toLarkCreates 把 planner 产出的 syncer.CreateOp（含内部键 Name）桥接为共享库 CreateOp，
// 丢弃 Name（共享库不感知 planner 内部概念），仅取写入字段与人员列旁路。写入字段经
// syncerFieldsToLark 按 fieldTypes 转为强类型（日期→int64 毫秒、数字→float64、其余 string）。
func toLarkCreates(src []syncer.CreateOp, fieldTypes map[string]int) []lark.CreateOp {
	out := make([]lark.CreateOp, len(src))
	for i, op := range src {
		out[i] = lark.CreateOp{Fields: syncerFieldsToLark(op.Fields, fieldTypes), Persons: op.Persons}
	}
	return out
}

// toLarkUpdates 把 planner 产出的 syncer.UpdateOp 桥接为共享库 UpdateOp，丢弃 Name。
// 写入字段经 syncerFieldsToLark 按 fieldTypes 转为强类型。
func toLarkUpdates(src []syncer.UpdateOp, fieldTypes map[string]int) []lark.UpdateOp {
	out := make([]lark.UpdateOp, len(src))
	for i, op := range src {
		out[i] = lark.UpdateOp{RecordID: op.RecordID, Fields: syncerFieldsToLark(op.Fields, fieldTypes), Persons: op.Persons}
	}
	return out
}

// syncerFieldsToLark 把 planner 产出的字符串 Fields（syncer.Row）按飞书列类型转成强类型 lark.Row，
// 供共享库 verbatim 序列化：
//   - TypeDateTime：值是 normalizeColumns 已归一好的毫秒整数串，strconv.ParseInt 转回 int64
//     （纯 int 转换，非日期语义解析）；空串或非整数跳列（等价旧 SDK 的 ParseMillisPlain 跳列）。
//   - TypeNumber：经 lark.ParseNumberLoose 转 float64（容忍千分位/百分号，与旧 SDK 注入等价）；
//     解析失败跳列。
//   - 其余列：保持 string 原样（含空串，与旧 SDK 文本列写入语义一致）。
//
// 人员列不出现在 Fields（planner 的 project 已排除，只走 Persons 旁路），无需在此处理。
func syncerFieldsToLark(fields syncer.Row, fieldTypes map[string]int) lark.WriteRow {
	out := make(lark.WriteRow, len(fields))
	for k, v := range fields {
		switch fieldTypes[k] {
		case larkbitablesdk.TypeDateTime:
			s := strings.TrimSpace(v)
			if s == "" {
				continue // 空日期跳列，避免 DatetimeFieldConvFail。
			}
			ms, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				continue // 非毫秒整数串跳列（normalizeColumns 未能归一的异常值）。
			}
			out[k] = ms
		case larkbitablesdk.TypeNumber:
			if n, ok := lark.ParseNumberLoose(v); ok {
				out[k] = n
			}
			// 无法解析的数字跳列。
		default:
			out[k] = v
		}
	}
	return out
}

// convertHCMData 把 hcm.ReportData 转换为本地的 syncer.ReportData 类型。
func convertHCMData(src *hcm.ReportData) *syncer.ReportData {
	if src == nil {
		return &syncer.ReportData{}
	}
	return &syncer.ReportData{
		Headers: convertHCMHeaders(src.Headers),
		Rows:    src.Rows,
	}
}

func convertHCMHeaders(src []hcm.ReportHeader) []syncer.ReportHeader {
	out := make([]syncer.ReportHeader, len(src))
	for i, h := range src {
		out[i] = syncer.ReportHeader{
			DataIndex: h.DataIndex,
			Title:     h.Title,
			Type:      h.Type,
			Children:  convertHCMHeaders(h.Children),
		}
	}
	return out
}

// convertRecruitData 把 recruitclient.ReportData 转换为本地的 syncer.ReportData 类型。
func convertRecruitData(src *recruitclient.ReportData) *syncer.ReportData {
	if src == nil {
		return &syncer.ReportData{}
	}
	return &syncer.ReportData{
		Headers: convertRecruitHeaders(src.Headers),
		Rows:    src.Rows,
	}
}

func convertRecruitHeaders(src []recruitclient.ReportHeader) []syncer.ReportHeader {
	out := make([]syncer.ReportHeader, len(src))
	for i, h := range src {
		out[i] = syncer.ReportHeader{
			DataIndex: h.DataIndex,
			Title:     h.Title,
			Type:      h.Type,
			Children:  convertRecruitHeaders(h.Children),
		}
	}
	return out
}

// missingPivotColumns 返回 pivot 转置产出的目标列（由 pivotHeaderColumn 各行值转成的列头）
// 中在目标表 tableFields 里不存在的列。pivotCols 首元素为 pivotIndexColumn（承载指标名，
// 非转置列头），故只比对其后的各列头取值。结果保持 pivotCols 的出现顺序，供上层记一条诊断
// warn 提示运营补建列；无缺失时返回 nil。
func missingPivotColumns(pivotCols []string, tableFields map[string]struct{}) []string {
	if len(pivotCols) <= 1 {
		return nil
	}
	var missing []string
	for _, c := range pivotCols[1:] {
		if _, ok := tableFields[c]; !ok {
			missing = append(missing, c)
		}
	}
	return missing
}

// syncMappingPivot 处理 shape=pivot 的映射：在 FlattenReport 产出上调用 syncer.Pivot
// 做转置，再复用现有 Plan/BatchCreate/BatchUpdate 写入 Bitable。
//
//   - 3.2：PivotHeaderColumn / PivotIndexColumn 为空或源报表无 headerColumn 列时记 warn 并跳过本轮映射。
//   - 3.3：DerivedColumns 与 PersonFieldSources 非空时忽略并记 warn。
//   - 3.4：headerColumn 值重复冲突按 Coalesce 风格记 warn。
func (r *Runner) syncMappingPivot(
	ctx context.Context,
	larkClient *lark.Client,
	m config.ReportMapping,
	cols []string,
	rows []syncer.Row,
	res MappingResult,
) MappingResult {
	label := mappingLabel(m)

	// 3.2：pivotHeaderColumn / pivotIndexColumn 未配置时跳过本轮。
	// PivotIndexColumn 不回退到 PivotHeaderColumn（二者语义不同：前者是输出索引列名，须与
	// 目标表标签列同名；后者是源列名），未显式声明即视为配置缺失。
	if m.PivotHeaderColumn == "" {
		logger.Warningf(ctx, "%s %s pivot 映射未声明 pivotHeaderColumn，跳过本轮同步", logTag, label)
		return res
	}
	if m.PivotIndexColumn == "" {
		logger.Warningf(ctx, "%s %s pivot 映射未声明 pivotIndexColumn，跳过本轮同步", logTag, label)
		return res
	}

	// 3.3：pivot 不支持 DerivedColumns 与 PersonFieldSources，有配置时忽略并告警。
	if len(m.DerivedColumns) > 0 {
		logger.Warningf(ctx, "%s %s pivot 形态不支持 derivedColumns，已忽略", logTag, label)
	}
	if len(m.PersonFieldSources) > 0 {
		logger.Warningf(ctx, "%s %s pivot 形态不支持 personFieldSources，已忽略", logTag, label)
	}
	// pivot 形态本次不接入 skipIf（行过滤语义需作用于转置前的月份行，与 flat 不同，属独立需求）。
	// 沿用 derivedColumns / personFieldSources 的「忽略 + 告警」先例，配置了也不因此失败。
	if len(m.SkipIf) > 0 {
		logger.Warningf(ctx, "%s %s pivot 形态不支持 skipIf 行过滤，已忽略", logTag, label)
	}

	// 打印转置前的拍平列/行，参考 syncMappingFlat 的调试输出，便于排查源报表形态。
	if raw, mErr := json.MarshalIndent(map[string]any{"cols": cols, "rows": rows}, "", "  "); mErr == nil {
		logger.Debugf(ctx, "%s %s 转置前的拍平列/行:\n%s", logTag, label, string(raw))
	}

	// 执行转置变换；返回 nil 说明 pivotHeaderColumn 不在源报表列中。
	// headerColumn（源列，值→列头）与 indexColumn（输出索引列名，= 目标表标签列）解耦。
	pivotCols, pivotRows, pivotConflicts := syncer.Pivot(cols, rows, m.PivotHeaderColumn, m.PivotIndexColumn)
	if pivotCols == nil {
		// 3.2：源报表无 pivotHeaderColumn 列时记 warn 并跳过。
		logger.Warningf(ctx, "%s %s pivot 的 pivotHeaderColumn=%q 在源报表中不存在，跳过本轮同步",
			logTag, label, m.PivotHeaderColumn)
		return res
	}

	// 打印转置后的列/行，便于核对指标行、月份列与目标表字段是否对齐。
	if raw, mErr := json.MarshalIndent(map[string]any{"cols": pivotCols, "rows": pivotRows}, "", "  "); mErr == nil {
		logger.Debugf(ctx, "%s %s 转置后的列/行:\n%s", logTag, label, string(raw))
	}

	// 3.4：上报 headerColumn 值重复冲突。
	for _, c := range pivotConflicts {
		logger.Warningf(ctx, "%s %s pivot 冲突：指标 %q 在 %q=%q 出现重复，保留 %q 丢弃 %q",
			logTag, label, c.Metric, m.PivotHeaderColumn, c.HeaderValue, c.Kept, c.Dropped)
	}

	// 以 pivotIndexColumn（承载指标名、与目标表标签列同名）为唯一键进入现有
	// Plan/BatchCreate/BatchUpdate 路径。转置输出与目标表回读都以该列为键，故两侧可对齐。
	spec := syncer.KeySpec{Fields: []string{m.PivotIndexColumn}, Sep: m.KeySeparator}

	table := lark.Table{AppToken: m.AppToken, TableID: m.TableID}
	fieldTypes, err := larkClient.ListFields(ctx, table)
	if err != nil {
		res.Err = err
		return res
	}

	normalizeColumns(pivotRows, fieldTypes)
	tableFields := make(map[string]struct{}, len(fieldTypes))
	for name := range fieldTypes {
		tableFields[name] = struct{}{}
	}

	// 目标转置列由运营预建：转置产出的列头（pivotCols 除首列 pivotIndexColumn 外的各行值）
	// 若在目标表中不存在，其值会被 Plan 的交集门闩静默忽略、不阻断本轮同步。此处对缺失的列
	// 汇总记一条 warn，便于运营补建列（诊断日志，不影响写入流程）。
	if missingCols := missingPivotColumns(pivotCols, tableFields); len(missingCols) > 0 {
		logger.Warningf(ctx, "%s %s pivot 目标表缺少转置列 %v，其值本轮被忽略，请在 Bitable 预建这些列",
			logTag, label, missingCols)
	}

	existing, err := larkClient.ListRecords(ctx, table, func(row lark.Row) string {
		return spec.KeyOf(syncer.Row(row))
	}, fieldTypes)
	if err != nil {
		res.Err = err
		return res
	}

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  pivotCols,
		Rows:        pivotRows,
		TableFields: tableFields,
		Existing:    toSyncerExisting(existing),
		// 强类型比对：pivot 转置后的指标值多为数字列，按 float64 判等避免 "90"/"90.0" 空转重写。
		FieldTypes: fieldTypes,
	})

	if _, err := larkClient.BatchCreate(ctx, table, toLarkCreates(plan.Creates, fieldTypes), fieldTypes); err != nil {
		res.Err = err
		return res
	}
	if err := larkClient.BatchUpdate(ctx, table, toLarkUpdates(plan.Updates, fieldTypes), fieldTypes); err != nil {
		res.Err = err
		return res
	}

	res.Created = len(plan.Creates)
	res.Updated = len(plan.Updates)
	res.Frozen = plan.Frozen
	res.Skipped = plan.SkippedNoName
	return res
}
