// Package job 实现 linapro-recruit-pipeline 插件的面试状态同步定时任务。
// 需求2 从「面试」阶段分两条路径拉取候选人，权威数据源为 EhrApplications 返回体内的
// interviewInfo（逐面试轮次），归档原因取 basicInfo.archiveReasons：
//   - 路径一（未归档 archived=false）：逐轮提取面试方式/时间/视频链接/是否应约；已应约且
//     视频面试时调 GetInterviewInformation 补视频链接；按 applicationId+轮次 upsert。
//   - 路径二（已归档 archived=true + 昨日0点至今）：仅更新是否应约 + 未应约原因两列，不新建行。
//
// 飞书行以 applicationId 字段 + 面试轮次（roundName）组合作为唯一标识。
package job

import (
	"context"
	"fmt"
	"lina-core/pkg/logger"
	"strconv"
	"strings"
	"time"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	lark "linapro-lark-sdk/larkbitable"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"
)

// interviewInfoBatchSize 是 interview-information 接口每批查询的 applicationId 上限。
const interviewInfoBatchSize = 50

const (
	// attendResultYes / attendResultNo 是写入「是否应约」列的可读标签。
	attendResultYes = "已应约"
	attendResultNo  = "未应约"
	// compositeKeySep 用于拼接 applicationId+轮次 组合键，取不可见字符避免与业务值冲突。
	compositeKeySep = "\x00"
)

// interviewColumns 保存从 field_mapping 解析出的面试状态表列名。
type interviewColumns struct {
	applicationID  string // applicationId 字段列，组合键成分之一
	interviewRound string // 面试轮次列（roundName），组合键成分之一
	candidateName  string // 候选人姓名列（basicInfo.name）
	interviewType  string // 面试方式列
	interviewTime  string // 面试时间列
	interviewer    string // 面试官人员列（interviewInfo[].interviewerFeedbacks[].interviewer.employeeId 工号→open_id）
	videoURL       string // 视频面试链接列
	attendResult   string // 是否应约列
	unattendReason string // 未应约原因列
}

// RunInterviewSync 执行需求2 面试状态同步：从面试阶段分未归档/已归档两条路径拉取候选人，
// 按 applicationId+面试轮次匹配面试状态表现有行，未归档路径 upsert 面试方式/时间/视频链接/是否应约，
// 已归档路径仅更新是否应约/未应约原因。回写目标为面试状态表（未配置时回退候选人决策表）。
func RunInterviewSync(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client) error {
	cols := resolveInterviewColumns(cfg.FieldMapping)
	if cols.applicationID == "" || cols.interviewRound == "" {
		return fmt.Errorf("interview-sync: field_mapping 缺少 applicationId 或 interview_round 列，无法按组合键匹配")
	}

	logger.Infof(ctx, "interview-sync: 开始同步, 面试阶段 stageId=%d", cfg.InterviewStageID)

	// 面试官人员列的 open_id 在插件边界解析一次，经 CreateOp/UpdateOp.Persons 旁路交给共享库
	// 序列化（共享库不做身份解析）。解析器在周期开始时向 employee-core 按写表格的 lark_app_id
	// 作用域批量拉取一次「工号 → 全部 open_id」映射（后续按行走内存查找，避免逐人查库 N+1）。
	// 面试官工号取自 interviewInfo 条目 interviewerFeedbacks[].interviewer.employeeId，一轮可能
	// 多名面试官，故先按逗号拼接（纯输入格式归一），交 empcap 复数版合并去重为 open_id 集合。
	// open_id 按应用作用域签发，故必须传入 cfg.LarkAppID 过滤；own+assoc、双租户同人多个 open_id
	// 全返回全写入；Bitable 请求随之以 user_id_type=open_id 识别人员字段。employee-core 未绑定或
	// 查询失败时 resolver 为 nil —— 面试官列会被跳过，不阻断本轮同步。
	var resolveInterviewers func(employeeNos []string) []string
	resolver, err := empcap.LarkOpenIDResolverByEmployeeNos(ctx, cfg.LarkAppID)
	if err != nil {
		logger.Warningf(ctx, "interview-sync: 加载工号→open_id 映射失败，面试官列本轮跳过: %v", err)
	} else if resolver != nil {
		resolveInterviewers = resolver
	} else {
		logger.Debugf(ctx, "interview-sync: employee-core 服务未绑定，面试官列本轮跳过")
	}

	// 客户端只负责序列化：人员列的 user_id_type 声明为 open_id（与旁路传入的 id 语义一致）。
	lc := lark.NewClient(cfg.LarkAppID, cfg.LarkAppSecret).
		WithEncoder(lark.Encoder{UserIDType: lark.UserIDTypeOpenID})
	table := lark.Table{AppToken: cfg.BitableAppToken, TableID: cfg.InterviewBitableTableID}

	// 先取字段类型再读记录：cellToString 按列类型解码（人员列回读姓名、富文本列拼接），
	// 必须把 ListFields 的结果传给读取函数，否则人员单元格会被按键名嗅探误判。
	fieldTypes, err := lc.ListFields(ctx, table)
	if err != nil {
		return fmt.Errorf("interview-sync: 读取字段类型失败: %w", err)
	}
	existing, err := lc.ListRecordsByID(ctx, table, fieldTypes)
	if err != nil {
		return fmt.Errorf("interview-sync: 读取 Bitable 记录失败: %w", err)
	}
	// 按 applicationId+轮次 组合键投影为规划器输入；人员列（面试官）open_id 集合旁路随之携带。
	existingRows := buildExistingRows(existing, func(f lark.Row) string {
		k := compositeKey(f[cols.applicationID], f[cols.interviewRound])
		if k == compositeKeySep {
			return "" // applicationId 与轮次均为空的行跳过
		}
		return k
	})
	logger.Infof(ctx, "interview-sync: 面试状态表已有 %d 条记录, 组合键索引 %d 项", len(existing), len(existingRows))

	keyCols := []string{cols.applicationID, cols.interviewRound}

	// 路径一：未归档候选人，全量 upsert（经规划器做字段级差异比对）。
	creates, updates, frozenUnarchived, err := syncUnarchived(ctx, cfg, mokaClient, existingRows, cols, keyCols, fieldTypes, resolveInterviewers)
	if err != nil {
		return err
	}

	// 路径二：已归档（昨日0点至今）候选人，仅更新是否应约 + 未应约原因（经规划器做差异比对，不新建）。
	archivedUpdates, frozenArchived, err := syncArchived(ctx, cfg, mokaClient, existingRows, cols, keyCols, fieldTypes)
	if err != nil {
		return err
	}
	updates = append(updates, archivedUpdates...)
	frozen := frozenUnarchived + frozenArchived

	// 统一落库：先建后更。写入前把各行 Fields 按飞书列类型转成强类型（面试时间日期列 →
	// int64 毫秒，其余保持文本），共享库只序列化、不再解析。
	typeCreateFields(creates, fieldTypes)
	typeUpdateFields(updates, fieldTypes)
	if len(creates) > 0 {
		if _, err := lc.BatchCreate(ctx, table, creates, fieldTypes); err != nil {
			return fmt.Errorf("interview-sync: 批量新建失败: %w", err)
		}
	}
	if len(updates) > 0 {
		if err := lc.BatchUpdate(ctx, table, updates, fieldTypes); err != nil {
			return fmt.Errorf("interview-sync: 批量更新失败: %w", err)
		}
	}
	logger.Infof(ctx, "interview-sync: 完成, 新建 %d 行, 更新 %d 行, 冻结 %d 行", len(creates), len(updates), frozen)
	return nil
}

// resolveInterviewColumns 从 field_mapping 解析面试状态表各列名。
// 逻辑字段键复用 config 包的常量，避免与 defaultFieldMapping 各处硬编码同一字符串。
func resolveInterviewColumns(m map[string]string) interviewColumns {
	return interviewColumns{
		applicationID:  m[config.FieldKeyApplicationID],
		interviewRound: m[config.FieldKeyInterviewRound],
		candidateName:  m[config.FieldKeyCandidateName],
		interviewType:  m[config.FieldKeyInterviewType],
		interviewTime:  m[config.FieldKeyInterviewTime],
		interviewer:    m[config.FieldKeyInterviewer],
		videoURL:       m[config.FieldKeyVideoURL],
		attendResult:   m[config.FieldKeyAttendResult],
		unattendReason: m[config.FieldKeyUnattendReason],
	}
}

// compositeKey 生成 applicationId+轮次 组合键。applicationId 做数值归一化。
func compositeKey(appVal, roundVal string) string {
	return normalizeID(appVal) + compositeKeySep + strings.TrimSpace(roundVal)
}

// syncUnarchived 处理路径一：拉取未归档面试阶段候选人，逐面试轮次构建 desired 行，经差异
// 规划器与现有行做字段级比对：命中仅更新变更列、无变更冻结、未命中新建。
// 已应约的视频面试轮次若 interviewInfo 内视频链接为空，批量调 GetInterviewInformation 补齐。
// resolveInterviewers 把该轮次面试官工号（逗号拼接）解析为 open_id 集合，经 desiredRow.persons
// 旁路交规划器按集合比差、经 op.Persons 写入。
func syncUnarchived(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client, existingRows map[string]existingRow, cols interviewColumns, keyCols []string, fieldTypes map[string]int, resolveInterviewers func(employeeNos []string) []string) (creates []lark.CreateOp, updates []lark.UpdateOp, frozen int, err error) {
	archived := false
	apps, err := mokaClient.EhrApplications(ctx, mokabackend.EhrApplicationsQuery{
		StageIDs: []int64{cfg.InterviewStageID},
		Archived: &archived,
	})
	if err != nil {
		return nil, nil, 0, fmt.Errorf("interview-sync: 调用未归档 EhrApplications 失败: %w", err)
	}
	logger.Infof(ctx, "interview-sync: 路径一(未归档) 拉取 %d 名候选人", len(apps))
	if len(apps) == 0 {
		return nil, nil, 0, nil
	}

	// 批量解析视频面试链接，避免逐轮调用产生 N+1。
	videoLookup, err := resolveVideoURLs(ctx, cfg, mokaClient, apps)
	if err != nil {
		return nil, nil, 0, err
	}

	var desired []desiredRow
	for _, app := range apps {
		appID := app.BasicInfo.ApplicationID
		candidateName := app.BasicInfo.Name
		reason := app.BasicInfo.ArchiveReasonName()
		for _, round := range app.Rounds() {
			attended := round.Status != mokabackend.InterviewStatusCancelled

			// 组装该面试轮次的完整业务列。
			row := lark.WriteRow{
				cols.interviewType: string(round.InterviewType),
				cols.interviewTime: round.StartTime.String(),
				cols.attendResult:  attendLabel(attended),
			}
			// 候选人姓名：列未配置时由 setCol 跳过。面试官是人员列，不进 Row —— 该轮次全部
			// 面试官工号先逗号拼接（输入格式归一），交 empcap 解析为去重 open_id 集合后经
			// persons 旁路交规划器（按集合比差、写入同一多人字段）。
			setCol(row, cols.candidateName, candidateName)
			persons := buildInterviewerPersons(cols.interviewer, round.InterviewerEmployeeNos(), resolveInterviewers)
			// 视频链接：优先 interviewInfo 原值，为空且已应约视频面试时取批量查询结果。
			videoURL := round.IntervieweeVideoURL
			if videoURL == "" && attended && round.InterviewType == mokabackend.InterviewTypeVideo {
				videoURL = videoLookup[appID][strings.TrimSpace(round.RoundName)]
			}
			setCol(row, cols.videoURL, videoURL)
			// 未应约原因列无条件写：未应约写归档原因，已应约写空串（显式清列），使该列恒定
			// 参与差异比对，消除「有时写、有时不写」的比对特例。
			unattendReason := ""
			if !attended {
				unattendReason = reason
			}
			setCol(row, cols.unattendReason, unattendReason)
			// 唯一键列恒定出现：更新时被 keyCols 跳过、新建时作为键列写入。
			row[cols.applicationID] = strconv.FormatInt(appID, 10)
			row[cols.interviewRound] = strings.TrimSpace(round.RoundName)

			key := compositeKey(strconv.FormatInt(appID, 10), round.RoundName)
			desired = append(desired, desiredRow{key: key, fields: row, persons: persons})
		}
	}

	res := planDiff(desired, existingRows, keyCols, fieldTypes)
	return res.creates, res.updates, res.frozen, nil
}

// syncArchived 处理路径二：拉取已归档（昨日0点至今，北京时间）面试阶段候选人，
// 按 applicationId+轮次 匹配现有行，仅在是否应约 + 未应约原因两列存在变更时更新，不新建行。
// 无匹配行的轮次直接跳过（不进入规划器，故绝不产出新建）。
func syncArchived(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client, existingRows map[string]existingRow, cols interviewColumns, keyCols []string, fieldTypes map[string]int) ([]lark.UpdateOp, int, error) {
	archived := true
	now := time.Now()
	startTime := yesterdayStartInBeijing(now)
	start, end := formatMokaTimeRange(startTime, now)
	apps, err := mokaClient.EhrApplications(ctx, mokabackend.EhrApplicationsQuery{
		StageIDs:          []int64{cfg.InterviewStageID},
		Archived:          &archived,
		UpdateAtStartTime: start,
		UpdateAtEndTime:   end,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("interview-sync: 调用已归档 EhrApplications 失败: %w", err)
	}
	logger.Infof(ctx, "interview-sync: 路径二(已归档 %s~%s) 拉取 %d 名候选人", start, end, len(apps))

	var desired []desiredRow
	for _, app := range apps {
		appID := app.BasicInfo.ApplicationID
		reason := app.BasicInfo.ArchiveReasonName()
		for _, round := range app.Rounds() {
			key := compositeKey(strconv.FormatInt(appID, 10), round.RoundName)
			// 路径二不新建：无匹配行的轮次跳过，不进入规划器。
			if _, ok := existingRows[key]; !ok {
				logger.Debugf(ctx, "interview-sync: 已归档 applicationId=%d 轮次=%q 无匹配行, 跳过", appID, round.RoundName)
				continue
			}
			attended := round.Status != mokabackend.InterviewStatusCancelled
			// 是否应约 + 未应约原因两列参与差异比对；未应约原因无条件写（已应约时空串=清列）。
			row := lark.WriteRow{cols.attendResult: attendLabel(attended)}
			unattendReason := ""
			if !attended {
				unattendReason = reason
			}
			setCol(row, cols.unattendReason, unattendReason)
			desired = append(desired, desiredRow{key: key, fields: row})
		}
	}

	res := planDiff(desired, existingRows, keyCols, fieldTypes)
	return res.updates, res.frozen, nil
}

// resolveVideoURLs 批量查询已应约视频面试轮次的视频链接。
// 收集所有「已应约 + 视频面试 + interviewInfo 内链接为空」的 applicationId，分批调
// GetInterviewInformation，返回 appID → roundName → 视频链接 的查找表。
func resolveVideoURLs(ctx context.Context, cfg *config.Config, mokaClient *mokabackend.Client, apps []mokabackend.EhrApplication) (map[int64]map[string]string, error) {
	// 收集待查询的 applicationId（去重）。
	needSet := make(map[int64]struct{})
	for _, app := range apps {
		for _, round := range app.Rounds() {
			attended := round.Status != mokabackend.InterviewStatusCancelled
			if attended && round.InterviewType == mokabackend.InterviewTypeVideo && round.IntervieweeVideoURL == "" {
				needSet[app.BasicInfo.ApplicationID] = struct{}{}
			}
		}
	}
	if len(needSet) == 0 {
		return map[int64]map[string]string{}, nil
	}
	ids := make([]int64, 0, len(needSet))
	for id := range needSet {
		ids = append(ids, id)
	}

	lookup := make(map[int64]map[string]string, len(ids))
	for start := 0; start < len(ids); start += interviewInfoBatchSize {
		end := min(start+interviewInfoBatchSize, len(ids))
		batch := ids[start:end]
		infos, err := mokaClient.GetInterviewInformation(ctx, batch, cfg.MokaOperatorEmail)
		if err != nil {
			return nil, fmt.Errorf("interview-sync: GetInterviewInformation 批次[%d:%d] 失败: %w", start, end, err)
		}
		for _, info := range infos {
			for _, entity := range info.Entities {
				if entity.IntervieweeVideoURL == "" {
					continue
				}
				if lookup[info.ApplicationID] == nil {
					lookup[info.ApplicationID] = make(map[string]string)
				}
				lookup[info.ApplicationID][strings.TrimSpace(entity.RoundName)] = entity.IntervieweeVideoURL
			}
		}
	}
	return lookup, nil
}

// attendLabel 按是否应约返回可读标签。
func attendLabel(attended bool) string {
	if attended {
		return attendResultYes
	}
	return attendResultNo
}

// setCol 仅在列名非空时写入，跳过未配置的列，避免写入空列名。
func setCol(row lark.WriteRow, col, val string) {
	if col == "" {
		return
	}
	row[col] = val
}

// buildInterviewerPersons 把一轮面试的面试官工号解析为「面试官」人员列的 open_id 集合，
// 打包成人员列旁路。工号先按逗号拼接成单元格原值语义的字符串，再经 empcap 复数版解析 ——
// 合并与去重（同一 HR 命中多个 open_id、重复工号）由数据 owner 侧完成，本处只做输入格式归一。
// 列未配置、无工号或解析不到 open_id 时返回 nil，该人员列本轮不写入（不阻断其余列）。
func buildInterviewerPersons(
	col string,
	employeeNos []string,
	resolveInterviewers func(employeeNos []string) []string,
) map[string][]string {
	if col == "" || len(employeeNos) == 0 || resolveInterviewers == nil {
		return nil
	}
	ids := resolveInterviewers(employeeNos)
	if len(ids) == 0 {
		return nil
	}
	return map[string][]string{col: ids}
}
