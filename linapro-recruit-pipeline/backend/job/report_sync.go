// Package job 实现 linapro-recruit-pipeline 插件的报表评分回写定时任务。
// 拉取 Moka 招聘报表，按 applicationId（Moka 报表「申请」列的值）匹配 Bitable 记录，
// 将目标评分列回写到对应行：命中则更新，未命中则新建。
// 列映射来自 config.ReportFieldMapping（sys_config report_field_mapping）：key 为 Moka 报表列
// 标题（源）、value 为飞书目标列名，两侧列名可不同（如 Moka「最终总分」→ 飞书「人才画像评分」）。
package job

import (
	"context"
	"fmt"
	"lina-core/pkg/logger"
	"maps"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	lark "linapro-lark-sdk/larkbitable"

	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
)

// reportColumns 保存从 report_field_mapping 解析出的报表评分表列映射。
// report_field_mapping 的 key 为 Moka 报表列标题（源），value 为飞书目标列名，
// 修正此前「Moka 报表列标题 == 飞书列名」的错误假设：按 key 定位 Moka 列 dataIndex、按 value 回写飞书列。
type reportColumns struct {
	// appLarkCol 是 applicationId 唯一键对应的飞书目标列名（report_field_mapping 中「申请」的 value）。
	appLarkCol string
	// values 是值列映射：Moka 报表列标题（源）→ 飞书目标列名，不含「申请」唯一键列。
	values map[string]string
}

// resolveReportColumns 从 report_field_mapping 解析报表评分表列映射。
// 「申请」的 value 作为唯一键飞书列名（未配置回退默认「applicationId」），其余键作为值列。
// value 为空的项跳过（未映射目标列）。
func resolveReportColumns(m map[string]string) reportColumns {
	cols := reportColumns{
		appLarkCol: m[config.ReportSourceApplicationTitle],
		values:     make(map[string]string, len(m)),
	}
	if cols.appLarkCol == "" {
		cols.appLarkCol = "applicationId"
	}
	for mokaTitle, larkCol := range m {
		if mokaTitle == config.ReportSourceApplicationTitle || larkCol == "" {
			continue
		}
		cols.values[mokaTitle] = larkCol
	}
	return cols
}

// RunReportSync 拉取已配置的多个招聘报表，按 applicationId 匹配 Bitable 记录，将指定评分列回写。
// 多个报表共用同一组目标列（姓名/人才画像评分/匹配度等级），Bitable 现有记录与字段类型只加载一次，
// 各报表产生的新建按 applicationId 合并、更新按 recordID 合并后统一一次 BatchCreate + BatchUpdate。
// - RecruitReportIDs 为空 → 记录 warn 日志并跳过本轮
// - 单个报表 GetReportData 失败或缺少「申请」列 → 记录 warn 日志并继续其余报表
// - applicationId 在 Bitable 中不存在 → 新建行（写入 applicationId 列 + 目标列）
// - 目标列在报表 headers 中不存在 → 记录 warn 日志并跳过该列
func RunReportSync(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client) error {
	if len(cfg.RecruitReportIDs) == 0 {
		logger.Warningf(ctx, "report-sync: recruit_report_ids 未配置, 跳过本轮")
		return nil
	}
	// 报表评分表不回退候选人决策表：未配置 report_bitable_table_id 时直接跳过，
	// 避免把报表评分误写进候选人决策表。
	if cfg.ReportBitableTableID == "" {
		logger.Warningf(ctx, "report-sync: report_bitable_table_id 未配置, 跳过本轮")
		return nil
	}

	cols := resolveReportColumns(cfg.ReportFieldMapping)

	logger.Infof(ctx, "report-sync: 开始同步, report_ids=%v, application_id_col=%q, value_columns=%v",
		cfg.RecruitReportIDs, cols.appLarkCol, cols.values)

	// Bitable 现有记录与字段类型只加载一次，供所有报表共用，避免逐表重复拉取。
	// 需求3 回写报表评分表（ReportBitableTableID），与候选人决策表分离；
	// 未单独配置时回退候选人表 table id。
	lc := lark.NewClient(cfg.LarkAppID, cfg.LarkAppSecret)
	table := lark.Table{AppToken: cfg.BitableAppToken, TableID: cfg.ReportBitableTableID}
	// 先取字段类型再读记录：cellToString 按列类型解码（人员列回读姓名、富文本列拼接），
	// 必须把 ListFields 的结果传给读取函数，否则人员单元格会被按键名嗅探误判。
	fieldTypes, err := lc.ListFields(ctx, table)
	if err != nil {
		return fmt.Errorf("report-sync: 读取字段类型失败: %w", err)
	}
	existing, err := lc.ListRecordsByID(ctx, table, fieldTypes)
	if err != nil {
		return fmt.Errorf("report-sync: 读取 Bitable 记录失败: %w", err)
	}
	// 按 applicationId 归一化为唯一键，兼容 Bitable 数字列回读的科学计数法，投影为规划器输入。
	// 报表无人员列，personIDs 旁路自然为空。
	existingRows := buildExistingRows(existing, func(f lark.Row) string { return normalizeID(f[cols.appLarkCol]) })
	logger.Infof(ctx, "report-sync: Bitable 已有 %d 条记录, 唯一键索引 %d 项", len(existing), len(existingRows))

	// 逐表拉取，经差异规划器做字段级比对：命中仅更新变更列、无变更冻结、未命中新建。
	// 单表失败记录 warn 后继续，不阻断整轮同步。
	creates, updates, frozen := collectReportOps(ctx, cfg, mokaClient, existingRows, cols, fieldTypes)
	if len(creates) == 0 && len(updates) == 0 {
		logger.Infof(ctx, "report-sync: 无可匹配的 Bitable 行或全部冻结（frozen=%d）, 本轮无需回写", frozen)
		return nil
	}

	// 同一 applicationId 可能出现在多个报表中，新建按 applicationId 合并、更新按 recordID 合并，
	// 避免重复新建或重复更新项。先建后更。
	creates = mergeReportCreates(creates, cols.appLarkCol)
	updates = mergeReportUpdates(updates)

	// 写入前把各行 Fields 按飞书列类型转成强类型（数字评分列 → float64、日期列 → int64 毫秒，
	// 其余保持文本），共享库只序列化、不再解析。
	typeCreateFields(creates, fieldTypes)
	typeUpdateFields(updates, fieldTypes)

	if len(creates) > 0 {
		if _, err := lc.BatchCreate(ctx, table, creates, fieldTypes); err != nil {
			return fmt.Errorf("report-sync: 批量新建失败: %w", err)
		}
	}
	if len(updates) > 0 {
		if err := lc.BatchUpdate(ctx, table, updates, fieldTypes); err != nil {
			return fmt.Errorf("report-sync: 批量更新失败: %w", err)
		}
	}
	logger.Infof(ctx, "report-sync: 新建 %d 条, 更新 %d 条, 冻结 %d 条报表评分成功", len(creates), len(updates), frozen)
	return nil
}

// mapReportColumns 按 report_field_mapping 将报表 headers 映射为回写用的列索引。
// 唯一键列按固定 Moka 标题「申请」匹配取得其 dataIndex；值列按各自的 Moka 报表列标题（源）
// 匹配报表 headers 的 title 取得 dataIndex，映射到飞书目标列名（value）。返回值：
//   - appDataIndex：「申请」列在报表 rows 中的 dataIndex（读 applicationId 值用）
//   - colIndex：飞书目标列名 → 报表 dataIndex（回写值列用）
//
// 「申请」列缺失时返回 error。值列按**飞书目标列分组**判断缺失：同一飞书目标列可由多个 Moka
// 源列供给（如匹配度等级-初/中/高 → 匹配度等级），任一源列在报表中命中即视为该目标列成功；
// 只有当该目标列的**全部**候选源列都缺失时，才记录一条 warn 并跳过该列，不阻断整轮同步。
// 这样每个报表只含三选一的匹配度源列时，不会因另两个源列缺失而重复告警。
func mapReportColumns(ctx context.Context, headers []mokabackend.ReportHeader, cols reportColumns) (string, map[string]string, error) {
	byTitle := make(map[string]string, len(headers))
	for _, h := range headers {
		byTitle[h.Title] = h.DataIndex
	}

	appDataIndex, ok := byTitle[config.ReportSourceApplicationTitle]
	if !ok {
		return "", nil, fmt.Errorf("report-sync: 报表 headers 中缺少「申请」列 %q", config.ReportSourceApplicationTitle)
	}

	// candidates 把 cols.values（Moka 源列标题 → 飞书目标列名）按飞书目标列（value）反向聚合为
	// 「飞书目标列 → 候选 Moka 源列列表」。分组信息完全来自配置（value 相同的 key 归为一组），
	// 不写死任何列名；运营在 report_field_mapping 里让多个源列填同一个飞书列名，即构成一组多源候选。
	// 以此实现「同一目标列的全部候选源列都缺失才告警一条」，避免多源列缺失时逐条刷屏。
	candidates := make(map[string][]string, len(cols.values))
	for mokaTitle, larkCol := range cols.values {
		candidates[larkCol] = append(candidates[larkCol], mokaTitle)
	}

	colIndex := make(map[string]string, len(candidates))
	for larkCol, sources := range candidates {
		matched := false
		for _, mokaTitle := range sources {
			if di, ok := byTitle[mokaTitle]; ok {
				colIndex[larkCol] = di
				matched = true
				break // 任一候选源列命中即可；多源同目标时取首个命中的源列。
			}
		}
		if !matched {
			logger.Warningf(ctx, "report-sync: 目标列 %q 在报表 headers 中无匹配源列（候选源列: %v）", larkCol, sources)
		}
	}
	return appDataIndex, colIndex, nil
}

// buildReportDesired 将报表行转换为规划器输入的 desired 行：每行含唯一键列 applicationId 与
// 命中的目标值列（值均为字符串，typeFields 尚未执行）。applicationId 为空的行跳过（debug 日志）。
// 报表无人员列，故 desired 行不含 persons 旁路。
func buildReportDesired(ctx context.Context, rows []map[string]any, colIndex map[string]string, appDataIndex, appCol string) []desiredRow {
	var desired []desiredRow
	for _, rowData := range rows {
		appVal := ""
		if v, ok := rowData[appDataIndex]; ok {
			appVal = normalizeID(fmt.Sprintf("%v", v))
		}
		if appVal == "" {
			logger.Debugf(ctx, "report-sync: 报表行缺少 applicationId, 跳过")
			continue
		}

		row := make(lark.WriteRow, len(colIndex)+1)
		for larkCol, dataIndex := range colIndex {
			if v, ok := rowData[dataIndex]; ok {
				row[larkCol] = fmt.Sprintf("%v", v)
			}
		}
		// 唯一键列恒定出现：更新时被 planDiff 的 keyCols 跳过、新建时作为键列写入。
		row[appCol] = appVal
		desired = append(desired, desiredRow{key: appVal, fields: row})
	}
	return desired
}

// collectReportOps 遍历 cfg.RecruitReportIDs 中的多个报表 ID，逐表拉取报表数据并经差异规划器
// 与已有 Bitable 记录做字段级比对：命中仅更新变更列、无变更冻结、未命中新建。单个报表
// GetReportData 失败、无行数据或缺少「申请」列时记录 warn 日志并继续其余报表，不阻断整轮同步。
// 返回累计的新建、更新与冻结计数。
func collectReportOps(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client, existingRows map[string]existingRow, cols reportColumns, fieldTypes map[string]int) (creates []lark.CreateOp, updates []lark.UpdateOp, frozen int) {
	for _, reportID := range cfg.RecruitReportIDs {
		report, err := mokaClient.GetReportData(ctx, reportID)
		if err != nil {
			logger.Warningf(ctx, "report-sync: GetReportData reportId=%d 失败, 跳过该表: %v", reportID, err)
			continue
		}
		if report == nil || len(report.Rows) == 0 {
			logger.Infof(ctx, "report-sync: reportId=%d 无报表行数据", reportID)
			continue
		}
		logger.Infof(ctx, "report-sync: reportId=%d, rows: %v, headers: %v", reportID, report.Rows, report.Headers)

		appDataIndex, colIndex, err := mapReportColumns(ctx, report.Headers, cols)
		if err != nil {
			logger.Warningf(ctx, "report-sync: reportId=%d 列解析失败, 跳过该表: %v", reportID, err)
			continue
		}
		if len(colIndex) == 0 {
			logger.Warningf(ctx, "report-sync: reportId=%d 无待同步的目标列", reportID)
			continue
		}
		desired := buildReportDesired(ctx, report.Rows, colIndex, appDataIndex, cols.appLarkCol)
		res := planDiff(desired, existingRows, []string{cols.appLarkCol}, fieldTypes)
		creates = append(creates, res.creates...)
		updates = append(updates, res.updates...)
		frozen += res.frozen
	}
	return creates, updates, frozen
}

// mergeReportUpdates 将多个报表产生的更新按 recordID 合并为一条，避免同一候选人在多个
// 报表中命中时出现重复的 recordID 更新项。同列冲突时后出现的报表覆盖先出现的值（按
// cfg.RecruitReportIDs 配置顺序）。
func mergeReportUpdates(updates []lark.UpdateOp) []lark.UpdateOp {
	if len(updates) <= 1 {
		return updates
	}
	index := make(map[string]int, len(updates))
	merged := make([]lark.UpdateOp, 0, len(updates))
	for _, u := range updates {
		if i, ok := index[u.RecordID]; ok {
			maps.Copy(merged[i].Fields, u.Fields)
			continue
		}
		index[u.RecordID] = len(merged)
		merged = append(merged, u)
	}
	return merged
}

// mergeReportCreates 将多个报表产生的新建按 applicationId 合并为一条，避免同一 applicationId
// 在多个报表中命中时创建重复行。同列冲突时后出现的报表覆盖先出现的值（按 cfg.RecruitReportIDs
// 配置顺序）。
func mergeReportCreates(creates []lark.CreateOp, appCol string) []lark.CreateOp {
	if len(creates) <= 1 {
		return creates
	}
	index := make(map[string]int, len(creates))
	merged := make([]lark.CreateOp, 0, len(creates))
	for _, c := range creates {
		key := asString(c.Fields[appCol])
		if i, ok := index[key]; ok {
			maps.Copy(merged[i].Fields, c.Fields)
			continue
		}
		index[key] = len(merged)
		merged = append(merged, c)
	}
	return merged
}
