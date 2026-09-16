// Package service 编排一次报表→多维表格同步周期：加载配置，
// 然后针对每个报表映射拉取 Moka 报表（根据映射来源选择 HCM 或 Recruit 客户端），
// 结合交集列门闩对目标多维表格进行规划，并应用新增/更新。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"lina-core/pkg/logger"

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
	// 人员字段编码需把工号解析为 open_id：周期开始时向 employee-core
	// 按写表格的 lark_app_id 作用域批量拉取一次「工号 → 全部 open_id」映射构成解析器
	// （后续按行走内存查找，避免 N+1）。open_id 按应用作用域签发，故必须传入 cfg.LarkApp
	// 过滤；own+assoc、双租户同人多个 open_id 全返回全写入。Bitable 请求随之以
	// user_id_type=open_id 识别人员字段。服务未绑定或查询失败时 resolver 为 nil ——
	// 人员列会被跳过，不阻断本轮同步。
	resolver, err := empcap.LarkOpenIDResolverByEmployeeNo(ctx, cfg.LarkApp)
	if err != nil {
		logger.Warningf(ctx, "%s 加载工号→open_id 映射失败，人员列本轮跳过: %v", logTag, err)
	}
	// 注入 report-sync 的差异化编码语义：东八区日期、容忍百分号/千分位的数字、
	// 把 Moka "-" 占位符归零的单元格兜底，以及人员字段的 open_id 解析器。
	larkClient := lark.NewClient(cfg.LarkApp, cfg.LarkAppKey).WithEncoder(lark.Encoder{
		ParseDateMillis:  lark.ParseEpochMillisCST,
		ParseNumber:      lark.ParseNumberLoose,
		CellFallback:     syncer.Stringify,
		ResolvePersonIDs: resolver,
		UserIDType:       lark.UserIDTypeOpenID,
	})

	// 仅当至少存在一个招聘类映射时才按需构建 recruit 客户端。
	var recruitClient *recruitclient.Client
	for _, m := range cfg.Mappings {
		if m.Enable && m.Source == config.SourceRecruit {
			recruitClient = recruitclient.NewClient(cfg.RecruitBase,
				recruitclient.NewOAuth2Auth(cfg.RecruitBase, recruitclient.OAuth2Credential{
					ClientID:     cfg.RecruitClientID,
					ClientSecret: cfg.RecruitClientSecret,
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
			logger.Infof(ctx, "%s %s 同步完成：新增=%d 更新=%d 冻结=%d 跳过=%d",
				logTag, mappingLabel(m), res.Created, res.Updated, res.Frozen, res.Skipped)
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
	resolver func(name string) []string,
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

	// spec 由 m.UniqueFields + m.KeySeparator 构造，贯穿 CoalesceRows、ListRecords、Plan 三处。
	spec := syncer.KeySpec{Fields: m.UniqueFields, Sep: m.KeySeparator}

	// 列派生：在 Coalesce 之前执行，使派生列（如年/月）可作为键组成列参与后续匹配。
	// 派生顺序：Flatten → 列派生 → Coalesce → Normalize → preparePersonFields → Plan。
	if len(m.DerivedColumns) > 0 {
		rules := toDeriveRules(m.DerivedColumns)
		cols = syncer.ApplyDerivedColumns(rows, cols, rules)
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
	// 按字段类型对 Moka 侧的需规一列做归一，使规划比较与 Bitable 回读值严格对称：
	// 写侧把日期编码成毫秒、读侧把毫秒读回字符串，Moka 侧保留原始日期文本
	// 会在 diffFields 里与毫秒字符串比较永远不相等，导致同值每轮被误判为更新。
	// 当前仅日期列会归一，文本/数字/人员等列不受影响；解析失败的值保留原样。
	normalizeColumns(rows, fieldTypes)
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

	// 人员列必须在规划前把人员字段值替换为对应工号，并确认工号能解析为当前写表格
	// 应用作用域的 open_id：PersonFieldSources 声明「Bitable 人员字段名 → Moka 工号列名」；
	// 未配置或工号解析不到 open_id 时清空该源值，使 Plan 忽略该列。
	preparePersonFields(rows, fieldTypes, resolver, m.PersonFieldSources)

	plan := syncer.Plan(syncer.PlanInput{
		Key:         spec,
		ReportCols:  cols,
		Rows:        rows,
		TableFields: tableFields,
		Existing:    toSyncerExisting(existing),
	})

	if _, err := larkClient.BatchCreate(ctx, table, toLarkCreates(plan.Creates), fieldTypes); err != nil {
		res.Err = err
		return res
	}
	if err := larkClient.BatchUpdate(ctx, table, toLarkUpdates(plan.Updates), fieldTypes); err != nil {
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
func toDeriveRules(cols []config.DerivedColumn) []syncer.DeriveRule {
	rules := make([]syncer.DeriveRule, 0, len(cols))
	for _, c := range cols {
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
			params["parse"] = func(s string) (int64, bool) {
				return lark.ParseEpochMillisCST(s)
			}
		}
		// split 规则：targets 的 key 顺序不稳定，需要调用方通过 Params["order"] 声明顺序；
		// 此处把 targets 的 key 列表按 JSON 顺序传入（Go map 无序，调用方应在配置中提供 order）。
		// 为兼容缺省情况，把 targets key 顺序转为 order（不保证稳定，建议配置方显式指定）。
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

// 使 encoder 能以工号为键查询 open_id。PersonFieldSources 声明
// 「Bitable 人员字段名 → Moka 报表工号列名」；未在 PersonFieldSources
// 中声明的人员字段直接清空，不参与本轮写入。工号列为空或 resolver
// 解析不到 open_id 时同样清空，使 Plan 忽略该列。
func preparePersonFields(
	rows []syncer.Row,
	fieldTypes map[string]int,
	resolver func(empNo string) []string,
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
			empNo := row[empNoCol]
			if empNo == "" || resolver == nil || len(resolver(empNo)) == 0 {
				row[fieldName] = ""
				continue
			}
			// 把人员字段值替换为工号，encoder 以工号查 open_id。
			row[fieldName] = empNo
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

// toSyncerExisting 把共享库返回的 ExistingRecord 映射桥接为 planner 用的 syncer.ExistingRecord。
// 两者字段同构，Row 底层同为 map[string]string，做浅拷贝转换即可。
func toSyncerExisting(src map[string]lark.ExistingRecord) map[string]syncer.ExistingRecord {
	out := make(map[string]syncer.ExistingRecord, len(src))
	for k, v := range src {
		out[k] = syncer.ExistingRecord{
			RecordID: v.RecordID,
			Fields:   syncer.Row(v.Fields),
		}
	}
	return out
}

// toLarkCreates 把 planner 产出的 syncer.CreateOp（含内部键 Name）桥接为共享库 CreateOp，
// 丢弃 Name（共享库不感知 planner 内部概念），仅取写入字段。
func toLarkCreates(src []syncer.CreateOp) []lark.CreateOp {
	out := make([]lark.CreateOp, len(src))
	for i, op := range src {
		out[i] = lark.CreateOp{Fields: lark.Row(op.Fields)}
	}
	return out
}

// toLarkUpdates 把 planner 产出的 syncer.UpdateOp 桥接为共享库 UpdateOp，丢弃 Name。
func toLarkUpdates(src []syncer.UpdateOp) []lark.UpdateOp {
	out := make([]lark.UpdateOp, len(src))
	for i, op := range src {
		out[i] = lark.UpdateOp{RecordID: op.RecordID, Fields: lark.Row(op.Fields)}
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
