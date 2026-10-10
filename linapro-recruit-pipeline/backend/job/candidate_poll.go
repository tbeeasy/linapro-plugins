// Package job 实现 linapro-recruit-pipeline 插件的候选人轮询定时任务。
// 每 5 分钟从 Moka 拉取昨日至今申请的初筛阶段候选人，写入飞书 Bitable 并入 Redis 待 AI 判定队列。
package job

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"lina-core/pkg/logger"
	"maps"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
	"lina-plugin-linapro-recruit-pipeline/backend/config"
	"lina-plugin-linapro-recruit-pipeline/backend/state"
	lark "linapro-lark-sdk/larkbitable"
)

// RunCandidatePoll 轮询 Moka 初筛阶段候选人，按 applicationId 去重后写入 Bitable 并入 Redis 队列。
// 拉取昨日 0 点至当前时间（北京时间）申请的候选人，已存在则跳过不更新。
func RunCandidatePoll(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client, store *state.Store) error {
	// 1. 计算时间窗口（昨日 0 点 → 现在，北京时间）
	now := time.Now()
	start := yesterdayStartInBeijing(now)
	startTime, endTime := formatMokaTimestampRange(start, now)

	logger.Infof(ctx, "candidate-poll: 开始轮询, stageId=%d, timeWindow=[%s, %s]",
		cfg.InitialScreeningStageID, startTime, endTime)

	// 2. 调用 Moka EhrApplications 接口
	applications, err := mokaClient.EhrApplications(ctx, mokabackend.EhrApplicationsQuery{
		StageIDs:                      []int64{cfg.InitialScreeningStageID},
		ApplicationAppliedAtStartTime: startTime,
		ApplicationAppliedAtEndTime:   endTime,
	})
	if err != nil {
		return fmt.Errorf("candidate-poll: 调用 EhrApplications 失败: %w", err)
	}

	if len(applications) == 0 {
		logger.Infof(ctx, "candidate-poll: 本轮无新候选人")
		return nil
	}

	logger.Infof(ctx, "candidate-poll: 拉取到 %d 个候选人", len(applications))

	// 3. 构建 Lark Client 与人员列解析器
	lc, table, err := buildLarkClient(cfg)
	if err != nil {
		return fmt.Errorf("candidate-poll: 构建 Lark 客户端失败: %w", err)
	}
	// 人员列（简历归属者）的 open_id 在插件边界解析一次后经 CreateOp/UpdateOp.Persons 旁路
	// 交给共享库序列化；解析器为 nil（employee-core 未绑定/查询失败）时归属者列跳过，不阻断轮询。
	resolveOwner := loadOwnerResolver(ctx, cfg)

	// 4. ListFields + ListRecords 建立索引
	fieldTypes, err := lc.ListFields(ctx, table)
	if err != nil {
		return fmt.Errorf("candidate-poll: 读取字段类型失败: %w", err)
	}

	records, err := lc.ListRecordsByID(ctx, table, fieldTypes)
	if err != nil {
		return fmt.Errorf("candidate-poll: 读取 Bitable 记录失败: %w", err)
	}

	appIDCol := cfg.FieldMapping[config.FieldKeyApplicationID]
	if appIDCol == "" {
		return fmt.Errorf("candidate-poll: field_mapping 缺少 %q", config.FieldKeyApplicationID)
	}
	index := buildCandidateIndex(records, appIDCol)

	logger.Infof(ctx, "candidate-poll: 简历评估池Bitable 现有 %d 条记录", len(index))

	// 4.1 加载 owner 报表映射（需求1.4·归属者回填）。报表非实时（5–20 分钟刷新）+ skip-once 去重，
	// 故归属者取值走「报表 + 回填」：一次报表拉取同时喂插入路径与回填路径。
	// owner_report_id 未配置、报表失败等降级情况返回空 map，不阻断轮询。
	ownerMap := loadOwnerMap(ctx, mokaClient, cfg)

	// 5. 遍历处理
	// 单周期限量（max_candidates_per_cycle）：每个新候选人需串行下载简历、上传飞书附件、写 Bitable，
	// 成本较高。积压较大时按上限只处理前 N 个新候选人，剩余留待下一轮（已写入的会被上面的 index 跳过，
	// 不会重复处理）。已存在的候选人只查 index、不计入上限。
	maxPerCycle := cfg.MaxCandidatesPerCycle
	if maxPerCycle <= 0 {
		maxPerCycle = config.DefaultMaxCandidatesPerCycle
	}
	var created, skipped, deferred int
	for _, app := range applications {
		appID := app.BasicInfo.ApplicationID
		appIDKey := strconv.FormatInt(appID, 10)

		// 检查是否已存在
		if _, exists := index[appIDKey]; exists {
			logger.Debugf(ctx, "candidate-poll: applicationId=%d 已存在, 跳过", appID)
			skipped++
			continue
		}

		// 命中单周期上限：不再处理新候选人，累计顺延数量后由下一轮继续。
		if created >= maxPerCycle {
			deferred++
			continue
		}

		// 处理单个候选人（插入路径：ownerMap 命中即写归属者，未命中留空由回填补上）
		if err := processCandidate(ctx, app, cfg, mokaClient, lc, table, store, fieldTypes, resolveOwner, ownerMap[appIDKey]); err != nil {
			logger.Errorf(ctx, "candidate-poll: processCandidate applicationId=%d: %v", appID, err)
			continue
		}
		created++
	}
	if deferred > 0 {
		logger.Infof(ctx, "candidate-poll: 命中单周期上限 %d, 本轮顺延 %d 个新候选人至下一轮", maxPerCycle, deferred)
	}

	// 6. 回填路径：遍历已加载的候选表全表 records，编排两条回填——
	//   ·归属者（需求1.4）：「简历归属者」列为空且 ownerMap 能解析出工号的行补齐；
	//   ·通过日期：本轮候选人经 Moka get_handle_Result 命中 accept 的行，把 recommendedTime 回填「通过日期」。
	// 两路可能命中同一 recordID，故各产出纯逻辑 UpdateOp 后按 recordID 合并，只发一次 BatchUpdate。
	// 复用已加载的 fieldTypes/records/lark client，不重复扫表、不新建定时任务。
	backfilled := backfillRowFields(ctx, cfg, mokaClient, lc, table, records, applications, ownerMap, appIDCol, fieldTypes, resolveOwner, now)

	logger.Infof(ctx, "candidate-poll: 完成, 新建=%d, 跳过=%d, 回填行数=%d", created, skipped, backfilled)
	return nil
}

// loadOwnerMap 拉取 owner 报表（需求1.4），构建 applicationId → HR 工号 映射。
// 按报表「申请」列（applicationId，同需求3 固定标题）与 owner_email_column 列标题定位 dataIndex，
// 遍历 rows 建映射：HR 邮箱经 TrimSpace+ToLower 归一化后查 owner_email_mapping 转工号。
// 降级（返回空 map、不阻断轮询）：owner_report_id 未配置、报表拉取失败、报表无行、缺「申请」或邮箱列。
func loadOwnerMap(ctx context.Context, mokaClient *mokabackend.Client, cfg *config.Config) map[string]string {
	if cfg.OwnerReportID == 0 {
		logger.Debugf(ctx, "candidate-poll: owner_report_id 未配置, 跳过归属者取值与回填")
		return nil
	}
	if len(cfg.OwnerEmailMapping) == 0 {
		logger.Warningf(ctx, "candidate-poll: owner_email_mapping 未配置, 跳过归属者取值与回填")
		return nil
	}

	report, err := mokaClient.GetReportData(ctx, cfg.OwnerReportID)
	if err != nil {
		logger.Warningf(ctx, "candidate-poll: GetReportData owner_report_id=%d 失败, 跳过归属者: %v", cfg.OwnerReportID, err)
		return nil
	}
	if report == nil || len(report.Rows) == 0 {
		logger.Infof(ctx, "candidate-poll: owner 报表 %d 无行数据, 跳过归属者", cfg.OwnerReportID)
		return nil
	}

	// 按 header title 定位 dataIndex：join 键列「申请」（applicationId）+ HR 邮箱列（owner_email_column）。
	byTitle := make(map[string]string, len(report.Headers))
	for _, h := range report.Headers {
		byTitle[h.Title] = h.DataIndex
	}
	appDataIndex, ok := byTitle[config.ReportSourceApplicationTitle]
	if !ok {
		logger.Warningf(ctx, "candidate-poll: owner 报表缺少「%s」列, 跳过归属者", config.ReportSourceApplicationTitle)
		return nil
	}
	emailDataIndex, ok := byTitle[cfg.OwnerEmailColumn]
	if !ok {
		logger.Warningf(ctx, "candidate-poll: owner 报表缺少 HR 邮箱列「%s」, 跳过归属者", cfg.OwnerEmailColumn)
		return nil
	}

	ownerMap := make(map[string]string, len(report.Rows))
	for _, rowData := range report.Rows {
		appVal := ""
		if v, ok := rowData[appDataIndex]; ok {
			appVal = normalizeID(fmt.Sprintf("%v", v))
		}
		if appVal == "" {
			continue
		}
		email := ""
		if v, ok := rowData[emailDataIndex]; ok {
			email = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
		}
		if email == "" {
			continue
		}
		empNo := cfg.OwnerEmailMapping[email]
		if empNo == "" {
			logger.Warningf(ctx, "candidate-poll: HR 邮箱 %q 未配 owner_email_mapping, applicationId=%s 跳过归属者", email, appVal)
			continue
		}
		ownerMap[appVal] = empNo
	}
	logger.Infof(ctx, "candidate-poll: owner 报表解析出 %d 条 applicationId→工号 映射", len(ownerMap))
	return ownerMap
}

// backfillRowFields 遍历候选表全表 records，编排两条回填路径并合并为一次 BatchUpdate：
//   - 归属者（需求1.4）：collectOwnerBackfill 对「简历归属者」列为空且 ownerMap 有工号的行补齐（经 Persons 旁路）；
//   - 通过日期：对本轮候选人 + 候选表中最近 N 个月内插入且通过日期为空的记录调 Moka get_handle_Result，
//     命中 accept 的行把 recommendedTime 回填「通过日期」列。
//
// 两路可能命中同一 recordID，故各产出纯逻辑 UpdateOp 后按 recordID 合并（并集 Fields 与 Persons），
// 只发一次 BatchUpdate。get_handle_Result 失败仅记 warn 并退化为只做归属者回填，不阻断轮询。
// 复用已加载的 fieldTypes/records/lark client。now 为轮询开始时刻，用于计算通过日期回填的时间回溯窗口。
// 返回实际写入的行数。
func backfillRowFields(
	ctx context.Context,
	cfg *config.Config,
	mokaClient *mokabackend.Client,
	lc *lark.Client,
	table lark.Table,
	records map[string]lark.ExistingRecord,
	applications []mokabackend.EhrApplication,
	ownerMap map[string]string,
	appIDCol string,
	fieldTypes map[string]int,
	resolveOwner func(employeeNos []string) []string,
	now time.Time,
) int {
	// 归属者路：只补空、不覆写；resume_owner 未配置或 ownerMap 空时该路产出为空。
	var ownerUpdates []lark.UpdateOp
	if ownerCol := cfg.FieldMapping[config.FieldKeyResumeOwner]; ownerCol != "" && len(ownerMap) > 0 {
		ownerUpdates = collectOwnerBackfill(records, ownerMap, appIDCol, ownerCol, resolveOwner)
	}

	// 通过日期路：pass_date 未配置则跳过。
	// 候选 applicationId 集合 = 本轮从 Moka 拉到的候选人 ∪ 候选表中「最近 N 个月内插入且通过日期为空」
	// 的记录：通过日期可能在申请后 1 个月才产生，只查本轮窗口内的申请会永远漏掉它们（申请当天不在
	// accept 状态、之后不再进入轮询窗口），故用时间回溯把这类历史申请重新纳入 get_handle_Result。
	var passUpdates []lark.UpdateOp
	if passCol := cfg.FieldMapping[config.FieldKeyPassDate]; passCol != "" {
		appIDs := mergeAcceptQueryIDs(ctx, records, appIDCol, passCol, applications, now)
		if acceptMap := collectAcceptTimes(ctx, mokaClient, appIDs); len(acceptMap) > 0 {
			passUpdates = collectPassDateBackfill(records, acceptMap, appIDCol, passCol)
		}
	}

	updates := mergeBackfillUpdates(ownerUpdates, passUpdates)
	if len(updates) == 0 {
		return 0
	}

	// 类型化后一次写入：通过日期列（TypeDateTime）的毫秒串在此转 int64；人员列走 Persons 旁路不受影响。
	typeUpdateFields(updates, fieldTypes)
	if err := lc.BatchUpdate(ctx, table, updates, fieldTypes); err != nil {
		logger.Errorf(ctx, "candidate-poll: 回填 BatchUpdate 失败: %v", err)
		return 0
	}
	return len(updates)
}

// mergeAcceptQueryIDs 汇总通过日期回填的 get_handle_Result 查询集合（去重后的 applicationId）：
//   - 本轮从 Moka 拉到的全部候选人 applicationId（走 EhrApplication.BasicInfo.ApplicationID）；
//   - 候选表中「最近 N 个月（passDateLookbackMonths）内插入、且通过日期列为空」的记录 applicationId。
//
// 后者是为「通过可能发生在申请后一个月」而设的回溯：这类申请的通过日期产生时早已不在轮询窗口内，
// 仅靠本轮候选会永远漏回填。插入时间取记录的飞书创建时间（CreatedTime，ListRecordsByID 已带
// automatic_fields），创建时间缺失（0）或早于窗口的记录跳过。now 为轮询开始时刻，窗口即 [now-N月, now]。
func mergeAcceptQueryIDs(
	ctx context.Context,
	records map[string]lark.ExistingRecord,
	appIDCol, passCol string,
	applications []mokabackend.EhrApplication,
	now time.Time,
) []int64 {
	seen := make(map[int64]struct{}, len(applications))
	ids := make([]int64, 0, len(applications))

	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	// 本轮候选人。
	for _, app := range applications {
		add(app.BasicInfo.ApplicationID)
	}

	// 回溯：最近 N 个月内插入且通过日期为空的记录。
	cutoff := passDateLookbackCutoff(now)
	for _, rec := range records {
		if rec.CreatedTime <= 0 || rec.CreatedTime < cutoff {
			continue
		}
		if strings.TrimSpace(rec.Fields[passCol]) != "" {
			continue
		}
		appKey := normalizeID(rec.Fields[appIDCol])
		if appKey == "" {
			continue
		}
		id, err := strconv.ParseInt(appKey, 10, 64)
		if err != nil {
			logger.Warningf(ctx, "candidate-poll: 通过日期回溯跳过非法 applicationId=%q", appKey)
			continue
		}
		add(id)
	}

	return ids
}

// passDateLookbackMonths 是通过日期回填的时间回溯窗口（自然月）。通过可能在申请后约一个月才产生，
// 该值须覆盖「申请 → 通过」的最长间隔，否则超期的申请其通过日期永远不补。暂定 2 个月。
const passDateLookbackMonths = 2

// passDateLookbackCutoff 返回通过日期回填的时间回溯下界（毫秒）：now 往前推 passDateLookbackMonths 个月。
// 用 AddDate 按自然月回溯（而非固定天数），跨月天数差异下语义与「最近 N 个月」一致。
func passDateLookbackCutoff(now time.Time) int64 {
	return now.AddDate(0, -passDateLookbackMonths, 0).UnixMilli()
}

// collectAcceptTimes 对给定 applicationId 列表调 Moka get_handle_Result（分批），构建
// applicationId（字符串）→ 最新推荐时间 映射。仅取 handleResult==accept 且 recommendedTime>0 的
// 处理结果；同一申请多条 accept 时取最大（最新）recommendedTime。
// 任一批次调用失败即记 warn 并返回 nil，由调用方退化为只做归属者回填，不阻断轮询。
func collectAcceptTimes(ctx context.Context, mokaClient *mokabackend.Client, appIDs []int64) map[string]int64 {
	if len(appIDs) == 0 {
		return nil
	}

	const batchSize = 50
	acceptMap := make(map[string]int64)
	for start := 0; start < len(appIDs); start += batchSize {
		end := start + batchSize
		if end > len(appIDs) {
			end = len(appIDs)
		}
		results, err := mokaClient.GetHandleResult(ctx, appIDs[start:end])
		if err != nil {
			logger.Warningf(ctx, "candidate-poll: GetHandleResult 失败, 本轮跳过通过日期回填: %v", err)
			return nil
		}
		for _, r := range results {
			if r.HandleResult != mokabackend.HandleResultAccept || r.RecommendedTime <= 0 {
				continue
			}
			key := strconv.FormatInt(r.ApplicationID, 10)
			if cur, ok := acceptMap[key]; !ok || r.RecommendedTime > cur {
				acceptMap[key] = r.RecommendedTime
			}
		}
	}
	return acceptMap
}

// collectOwnerBackfill 是回填路径的纯逻辑（需求1.4）：遍历候选表全表 records，收集「简历归属者」列
// 为空且 ownerMap 能解析出工号的行为 UpdateOp。**只补空、不覆写**：records 的人员列已按 fieldTypes
// 解码为姓名，空串（TrimSpace 后）即视为未写；已写过的行跳过，保证幂等与最小写入。
// 归属人工号在此解析为 open_id 集合，经 UpdateOp.Persons 旁路写入（人员列不进 Fields）：
// 解析器为 nil 或工号解析不到 open_id 时该行不补（保持只补空、不写空值的语义）。
func collectOwnerBackfill(
	records map[string]lark.ExistingRecord,
	ownerMap map[string]string,
	appIDCol, ownerCol string,
	resolveOwner func(employeeNos []string) []string,
) []lark.UpdateOp {
	if resolveOwner == nil {
		return nil
	}
	var updates []lark.UpdateOp
	for recordID, rec := range records {
		// 只补空、不覆写：非空即已写过归属者，跳过。
		if strings.TrimSpace(rec.Fields[ownerCol]) != "" {
			continue
		}
		appKey := normalizeID(rec.Fields[appIDCol])
		if appKey == "" {
			continue
		}
		empNo := ownerMap[appKey]
		if empNo == "" {
			continue
		}
		ownerIDs := resolveOwner([]string{empNo})
		if len(ownerIDs) == 0 {
			continue
		}
		updates = append(updates, lark.UpdateOp{
			RecordID: recordID,
			Fields:   lark.WriteRow{},
			Persons:  map[string][]string{ownerCol: ownerIDs},
		})
	}
	return updates
}

// collectPassDateBackfill 是通过日期回填的纯逻辑：遍历候选表全表 records，对 applicationId 命中 acceptMap
// 的行，按写入规则产出「通过日期」列的 UpdateOp。写入规则：
//   - 当前列为空 → 直接写；
//   - 当前列非空且能解析出毫秒 curMs → 仅当新值 newMs > curMs（更新为更新的时间）才写；相等/更小跳过（幂等）；
//   - 当前列非空但无法解析 → 按「空」处理照写。
//
// 值以毫秒字符串写入 Fields，交由 typeUpdateFields 按列类型（TypeDateTime）转 int64 毫秒。
func collectPassDateBackfill(
	records map[string]lark.ExistingRecord,
	acceptMap map[string]int64,
	appIDCol, passCol string,
) []lark.UpdateOp {
	if len(acceptMap) == 0 {
		return nil
	}
	var updates []lark.UpdateOp
	for recordID, rec := range records {
		appKey := normalizeID(rec.Fields[appIDCol])
		if appKey == "" {
			continue
		}
		newMs, ok := acceptMap[appKey]
		if !ok {
			continue
		}
		// 非空且可解析时，仅当新值更大才更新；相等/更小跳过（幂等）。解析失败按「空」照写。
		if cur := strings.TrimSpace(rec.Fields[passCol]); cur != "" {
			if curMs, ok := lark.ParseEpochMillisUTC(cur); ok && newMs <= curMs {
				continue
			}
		}
		updates = append(updates, lark.UpdateOp{
			RecordID: recordID,
			Fields:   lark.WriteRow{passCol: strconv.FormatInt(newMs, 10)},
		})
	}
	return updates
}

// mergeBackfillUpdates 将归属者路与通过日期路的 UpdateOp 按 recordID 合并为一条，避免同一 recordID
// 被更新两次。合并同时并集 Fields（maps.Copy）与 Persons（逐键复制）——两路分别写日期列与人员列，
// 列不冲突。仅一路有值时直接返回该路（无需合并）。
func mergeBackfillUpdates(ownerUpdates, passUpdates []lark.UpdateOp) []lark.UpdateOp {
	if len(ownerUpdates) == 0 {
		return passUpdates
	}
	if len(passUpdates) == 0 {
		return ownerUpdates
	}
	index := make(map[string]int, len(ownerUpdates)+len(passUpdates))
	merged := make([]lark.UpdateOp, 0, len(ownerUpdates)+len(passUpdates))
	add := func(u lark.UpdateOp) {
		i, ok := index[u.RecordID]
		if !ok {
			index[u.RecordID] = len(merged)
			merged = append(merged, u)
			return
		}
		// 并集 Fields。
		if len(u.Fields) > 0 {
			if merged[i].Fields == nil {
				merged[i].Fields = lark.WriteRow{}
			}
			maps.Copy(merged[i].Fields, u.Fields)
		}
		// 并集 Persons（列名 → open_id 集合）。
		if len(u.Persons) > 0 {
			if merged[i].Persons == nil {
				merged[i].Persons = make(map[string][]string, len(u.Persons))
			}
			for col, ids := range u.Persons {
				merged[i].Persons[col] = ids
			}
		}
	}
	for _, u := range ownerUpdates {
		add(u)
	}
	for _, u := range passUpdates {
		add(u)
	}
	return merged
}

// processCandidate 处理单个候选人：获取简历 → 下载附件 → 写 Bitable → 入队。
// ownerEmployeeNo 为该候选人在 owner 报表命中的 HR 工号（未命中为空串，归属者列留空由回填补上），
// resolveOwner 把它解析为写表格应用作用域的 open_id 集合后经 Persons 旁路写入。
func processCandidate(
	ctx context.Context,
	app mokabackend.EhrApplication,
	cfg *config.Config,
	mokaClient *mokabackend.Client,
	lc *lark.Client,
	table lark.Table,
	store *state.Store,
	fieldTypes map[string]int,
	resolveOwner func(employeeNos []string) []string,
	ownerEmployeeNo string,
) error {
	appID := app.BasicInfo.ApplicationID

	// 1. 获取简历文本
	resumeContent := ""
	resumeOK := true
	if rc, err := mokaClient.GetResumeContent(ctx, appID); err != nil {
		logger.Errorf(ctx, "candidate-poll: GetResumeContent applicationId=%d: %v", appID, err)
		resumeOK = false
	} else if rc != nil {
		resumeContent = rc.ResumeContent
	}

	// 2. 下载并上传简历附件（尽力而为）
	attachments := uploadResumeAttachment(ctx, lc, table, app.BasicInfo, cfg.FieldMapping)

	// 3. 构建行数据（人员列不进 Fields，单独经 Persons 旁路传入）
	row := buildRowFromBasicInfo(app.BasicInfo, cfg.FieldMapping)
	if col := cfg.FieldMapping[config.FieldKeyResume]; col != "" {
		row[col] = resumeContent
	}
	persons := buildOwnerPersons(cfg, resolveOwner, ownerEmployeeNo)

	// 写入前把 Fields 按飞书列类型转成强类型（数字列如 applicationId/工作年限 → float64、
	// 日期列 → int64 毫秒，其余保持文本），共享库只序列化、不再解析。
	typeFields(row, fieldTypes)

	// 4. 写入 Bitable
	ids, err := lc.BatchCreate(ctx, table, []lark.CreateOp{{Fields: row, Attachments: attachments, Persons: persons}}, fieldTypes)
	if err != nil {
		return fmt.Errorf("candidate-poll: 写入 Bitable 失败: %w", err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("candidate-poll: BatchCreate 返回空 recordID")
	}
	recordID := ids[0]

	logger.Infof(ctx, "candidate-poll: 写入 applicationId=%d, recordID=%s", appID, recordID)

	// 5. 入 Redis 队列（仅当有简历时）
	if resumeOK && store != nil {
		if err := store.AddPending(ctx, recordID, appID, time.Now().UTC().UnixMilli()); err != nil {
			logger.Errorf(ctx, "candidate-poll: AddPending recordID=%s applicationId=%d: %v", recordID, appID, err)
		}
	}

	return nil
}

// buildRowFromBasicInfo 从 Moka BasicInfo 构建 Bitable 行数据（不含人员列）。
// 归属人「简历归属者」是人员字段，其 open_id 集合由 buildOwnerPersons 单独产出到 Person 旁路，
// 不放进本 Row —— Row 里放工号文本会被写进飞书人员字段并整批失败。
func buildRowFromBasicInfo(info mokabackend.ApplicationBasicInfo, fieldMapping map[string]string) lark.WriteRow {
	row := make(lark.WriteRow)

	// 映射字段（复用 field_mapping 键名）
	if col := fieldMapping[config.FieldKeyApplicationID]; col != "" {
		row[col] = strconv.FormatInt(info.ApplicationID, 10)
	}
	if col := fieldMapping[config.FieldKeyName]; col != "" {
		row[col] = info.Name
	}
	// 联系方式：合并电话和邮箱
	if col := fieldMapping[config.FieldKeyContact]; col != "" {
		contact := ""
		if info.Phone != "" {
			contact = info.Phone
		}
		if info.Email != "" {
			if contact != "" {
				contact += " / " + info.Email
			} else {
				contact = info.Email
			}
		}
		if contact != "" {
			row[col] = contact
		}
	}
	if col := fieldMapping[config.FieldKeyExperience]; col != "" {
		row[col] = strconv.Itoa(info.Experience)
	}
	if col := fieldMapping[config.FieldKeyAcademicDegree]; col != "" {
		row[col] = info.AcademicDegree
	}
	if col := fieldMapping[config.FieldKeyLastSchool]; col != "" {
		row[col] = info.LastSchool
	}
	if col := fieldMapping[config.FieldKeySource]; col != "" {
		row[col] = info.SourceName
	}

	// 注意：「简历归属者」是人员列，不在此处写入 —— 见 buildOwnerPersons。
	return row
}

// buildOwnerPersons 把归属人工号解析为「简历归属者」列的 open_id 集合并打包成人员列旁路。
// 工号来自 owner 报表（需求1.4，非 basicInfo.owner —— 后者生产实测恒为 nil），命中即写、
// 未命中/解析不到 open_id 时返回 nil，留给回填路径补齐。人员列不走 Row：飞书人员字段只接受
// 用户 id 对象数组，Row 里的工号文本会被飞书整批拒绝。
func buildOwnerPersons(
	cfg *config.Config,
	resolveOwner func(employeeNos []string) []string,
	ownerEmployeeNo string,
) map[string][]string {
	col := cfg.FieldMapping[config.FieldKeyResumeOwner]
	if col == "" || ownerEmployeeNo == "" || resolveOwner == nil {
		return nil
	}
	ids := resolveOwner([]string{ownerEmployeeNo})
	if len(ids) == 0 {
		return nil
	}
	return map[string][]string{col: ids}
}

// uploadResumeAttachment 下载 resumeUrl 并上传到飞书，返回附件 map。失败时返回 nil（降级）。
func uploadResumeAttachment(
	ctx context.Context,
	lc *lark.Client,
	table lark.Table,
	info mokabackend.ApplicationBasicInfo,
	fieldMapping map[string]string,
) map[string][]string {
	col := fieldMapping[config.FieldKeyResumeFile]
	if col == "" {
		return nil
	}
	if info.ResumeURL == "" {
		logger.Infof(ctx, "candidate-poll: applicationId=%d resumeUrl 为空, 跳过附件", info.ApplicationID)
		return nil
	}

	// 下载
	data, contentType, err := downloadResume(ctx, info.ResumeURL)
	if err != nil {
		logger.Errorf(ctx, "candidate-poll: downloadResume applicationId=%d: %v", info.ApplicationID, err)
		return nil
	}

	// 生成文件名
	fileName := resumeFileName(info.Name, info.ResumeURL, contentType)

	// 上传到飞书
	token, err := lc.UploadMedia(ctx, table.AppToken, fileName, len(data), bytes.NewReader(data))
	if err != nil {
		logger.Errorf(ctx, "candidate-poll: UploadMedia applicationId=%d: %v", info.ApplicationID, err)
		return nil
	}

	logger.Infof(ctx, "candidate-poll: 附件上传成功 applicationId=%d, fileName=%s, token=%s",
		info.ApplicationID, fileName, token)

	return map[string][]string{col: {token}}
}

// downloadResume 从 URL 下载简历文件，返回字节数组和 Content-Type
func downloadResume(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("candidate-poll: 下载简历 HTTP 状态码 %d", resp.StatusCode)
	}

	// 限制 20MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		return nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// resumeFileName 生成简历文件名：姓名-简历.扩展名
func resumeFileName(name, url, contentType string) string {
	if name == "" {
		name = "resume"
	}

	// 从 URL 或 Content-Type 推断扩展名
	ext := path.Ext(url)
	if ext == "" {
		ext = guessExtFromContentType(contentType)
	}

	return name + "-简历" + ext
}

// guessExtFromContentType 从 Content-Type 推断文件扩展名
func guessExtFromContentType(ct string) string {
	switch {
	case strings.Contains(ct, "pdf"):
		return ".pdf"
	case strings.Contains(ct, "word"), strings.Contains(ct, "msword"):
		return ".doc"
	case strings.Contains(ct, "openxmlformats"):
		return ".docx"
	default:
		return ".pdf" // 默认
	}
}

// buildCandidateIndex 按 applicationId 列建立索引
func buildCandidateIndex(records map[string]lark.ExistingRecord, appCol string) map[string]string {
	index := make(map[string]string, len(records))
	for recordID, rec := range records {
		key := normalizeID(rec.Fields[appCol])
		if key == "" {
			continue
		}
		index[key] = recordID
	}
	return index
}

// buildLarkClient 构建 Lark 客户端。客户端只负责序列化：人员列的 open_id 由调用方解析后经
// CreateOp/UpdateOp.Persons 旁路传入，故此处只声明 user_id_type=open_id（open_id 集合按写表格
// 应用作用域签发，参数与实际写入的 id 语义必须一致）。
func buildLarkClient(cfg *config.Config) (*lark.Client, lark.Table, error) {
	lc := lark.NewClient(cfg.LarkAppID, cfg.LarkAppSecret).
		WithEncoder(lark.Encoder{UserIDType: lark.UserIDTypeOpenID})
	table := lark.Table{AppToken: cfg.BitableAppToken, TableID: cfg.CandidateBitableTableID}
	return lc, table, nil
}

// loadOwnerResolver 加载「工号 → 全部 open_id」解析闭包，供「简历归属者」人员列解析。
// 按写表格的 lark_app_id 作用域批量拉取一次（后续按行走内存查找，避免 N+1）。
// employee-core 未绑定或查询失败时返回 nil —— 归属者列本轮跳过，不阻断候选人写入与入队。
func loadOwnerResolver(ctx context.Context, cfg *config.Config) func(employeeNos []string) []string {
	resolver, err := empcap.LarkOpenIDResolverByEmployeeNos(ctx, cfg.LarkAppID)
	if err != nil {
		logger.Warningf(ctx, "candidate-poll: 加载工号→open_id 映射失败, 归属者列本轮跳过: %v", err)
		return nil
	}
	if resolver == nil {
		logger.Debugf(ctx, "candidate-poll: employee-core 服务未绑定, 归属者列本轮跳过")
	}
	return resolver
}
