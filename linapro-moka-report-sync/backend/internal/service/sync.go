// Package service 编排一次报表→多维表格同步周期：加载配置，
// 然后针对每个报表映射拉取 Moka 报表（根据映射来源选择 HCM 或 Recruit 客户端），
// 结合交集列门闩对目标多维表格进行规划，并应用新增/更新。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gogf/gf/v2/frame/g"

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
		g.Log().Info(ctx, logTag, "no report mappings configured; nothing to sync")
		return nil, nil
	}

	hcmClient := hcm.NewClient(cfg.MokaBase, cfg.MokaCred)
	// 人员字段（如报表中的人员列）编码需把人名解析为 open_id：周期开始时向 employee-core
	// 按写表格的 lark_app_id 作用域批量拉取一次「姓名 → 全部 open_id」映射构成解析器
	// （后续按行走内存查找，避免 N+1）。open_id 按应用作用域签发，故必须传入 cfg.LarkApp
	// 过滤；own+assoc、双租户同人多个 open_id 全返回全写入。Bitable 请求随之以
	// user_id_type=open_id 识别人员字段。服务未绑定或查询失败时 resolver 为 nil ——
	// 人员列会被跳过，不阻断本轮同步。
	resolver, err := empcap.LarkOpenIDResolver(ctx, cfg.LarkApp)
	if err != nil {
		g.Log().Warningf(ctx, "%s 加载姓名→open_id 映射失败，人员列本轮跳过: %v", logTag, err)
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
			g.Log().Infof(ctx, "%s %s skipped: disabled by config", logTag, mappingLabel(m))
			continue
		}
		res := r.syncMapping(ctx, hcmClient, recruitClient, larkClient, m)
		if res.Err != nil {
			g.Log().Errorf(ctx, "%s %s sync failed: %v", logTag, mappingLabel(m), res.Err)
		} else {
			g.Log().Infof(ctx, "%s %s synced: created=%d updated=%d frozen=%d skipped=%d",
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
	m config.ReportMapping,
) MappingResult {
	res := MappingResult{ReportID: m.ReportID}

	// 从对应数据源拉取报表数据。
	var data *syncer.ReportData
	switch m.Source {
	case config.SourceRecruit:
		if recruitClient == nil {
			res.Err = fmt.Errorf("moka-report-sync: recruit credentials not configured for mapping %s", mappingLabel(m))
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

	// Moka 可能为同一个 uniqueField 值返回多行（例如一行完整数据
	// 加上一行全是 "-" 占位符的副本）。在规划前合并它们，避免生成重复记录；
	// 字段冲突会在下方上报，而不是被静默丢弃。
	rows, conflicts := syncer.CoalesceRows(rows, m.UniqueField)
	for _, c := range conflicts {
		g.Log().Warningf(ctx, "%s %s conflict on %s: %s 保留 %q, 丢弃 %q", logTag, mappingLabel(m), c.Key, c.Field, c.Kept, c.Dropped)
	}

	if raw, mErr := json.MarshalIndent(data, "", "  "); mErr == nil {
		g.Log().Debugf(ctx, "%s %s raw ReportData:\n%s", logTag, mappingLabel(m), string(raw))
	}
	if raw, mErr := json.MarshalIndent(map[string]any{"cols": cols, "rows": rows}, "", "  "); mErr == nil {
		g.Log().Debugf(ctx, "%s %s flattened cols/rows:\n%s", logTag, mappingLabel(m), string(raw))
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
	existing, err := larkClient.ListRecords(ctx, table, m.UniqueField, fieldTypes)
	if err != nil {
		res.Err = err
		return res
	}

	plan := syncer.Plan(syncer.PlanInput{
		UniqueField: m.UniqueField,
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
