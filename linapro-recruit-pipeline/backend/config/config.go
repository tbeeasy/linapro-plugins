// Package config 负责加载 linapro-recruit-pipeline 插件的运行时配置。
// 飞书应用凭证和 Moka BasicAuth apiKey 来自静态 host 配置文件；
// Bitable 目标表、报表 ID、阶段 ID、等待阈值和可选列名列表来自 sys_config 行，
// 运营人员可在不重启进程的情况下调整这些参数。
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/plugin/capability"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// ============================================================================
// 第一组：静态 host 配置键（来自 config.yaml 的 `plugin.linapro-recruit-pipeline.*`）。
// 这些是凭证与基本不变的连接参数，修改后必须重启宿主进程才生效，不走后台 sys_config。
// ============================================================================
const (
	// 飞书应用凭证，与 linapro-moka-report-sync 共用，通过 YAML anchor 合并到本命名空间的 `.lark.*`。
	keyLarkAppID     = "plugin.linapro-recruit-pipeline.lark.appId"     // 飞书应用 appId（必需）
	keyLarkAppSecret = "plugin.linapro-recruit-pipeline.lark.appSecret" // 飞书应用 appSecret（必需）

	// Moka BasicAuth 凭证与调用参数。apiKey 是组织级共享的单一密钥，存放在 provider 插件命名空间下供多方共用。
	keyMokaAPIKey  = "plugin.linapro-recruit-pipeline.moka.apiKey"  // Moka BasicAuth apiKey（调 Moka 必需）
	keyMokaBaseURL = "plugin.linapro-recruit-pipeline.moka.baseURL" // Moka API 基础 URL（可选，空用默认）
	// operatorEmail 是调用 interview-information（视频面试链接）接口所需的组织管理员邮箱。
	// 该邮箱基本不变，故存于静态 host 配置而非 sys_config。
	keyMokaOperatorEmail = "plugin.linapro-recruit-pipeline.moka.operatorEmail" // 视频面试链接接口管理员邮箱（需求2 视频面试必需）

	// 定时任务间隔（分钟）- 修改后需重启才能重新注册 cron 任务，故放静态配置而非 sys_config
	keyCandidatePollIntervalMinutes = "plugin.linapro-recruit-pipeline.candidate_poll_interval_minutes" // 候选人轮询间隔（分钟），默认 defaultCandidatePollIntervalMinutes
	keyInterviewSyncIntervalMinutes = "plugin.linapro-recruit-pipeline.interview_sync_interval_minutes" // 面试同步轮询间隔（分钟），默认 defaultInterviewSyncIntervalMinutes
	keyReportSyncIntervalMinutes    = "plugin.linapro-recruit-pipeline.report_sync_interval_minutes"    // 报表同步轮询间隔（分钟），默认 defaultReportSyncIntervalMinutes

	// candidate_poll_timeout_minutes 是候选人轮询单次执行的超时（分钟）。宿主默认给每次任务套 5 分钟
	// deadline，而单周期要串行下载/上传每个新候选人的简历附件，积压较多时会超过 5 分钟。此值在注册时
	// 读取一次，用于在任务回调内以 WithoutCancel 解绑宿主 deadline 后重设更长超时，故放静态配置。
	keyCandidatePollTimeoutMinutes = "plugin.linapro-recruit-pipeline.candidate_poll_timeout_minutes" // 候选人轮询单次执行超时（分钟），默认 defaultCandidatePollTimeoutMinutes
)

// ============================================================================
// 第二组：sys_config 配置键（后台数据库配置行，热生效，无需重启）。
// 均为可选：未配置时回退到本文件下方「第三组」的默认值或按注释的回退规则处理。
// ============================================================================
const (
	// —— Bitable 目标表定位（三表共用 App token，仅 table id 不同）——
	keyBitableAppToken = "plugin.linapro-recruit-pipeline.recruit_bitable_app_token" // 多维表格 App token（三表共用，无默认，飞书调用必需）
	// candidate_bitable_table_id 是候选人决策表（候选人轮询写入、AI 判定回表读取）的 table id。
	// 候选人轮询任务写入后把返回的 recordID 存入 Redis，AI 判定任务按该 recordID 回表读取判定字段，
	// 两者必须指向同一张表，故共用本键。
	// 三张 table id 键统一按用途命名：candidate_/interview_/report_。
	keyCandidateBitableTableID = "plugin.linapro-recruit-pipeline.candidate_bitable_table_id" // 候选人决策表 table id（需求1.1 写入 / 需求1.5 回读共用）
	// interview_bitable_table_id 是面试状态回写表（需求2）的 table id；
	// report_bitable_table_id 是报表评分回写表（需求3）的 table id。
	// 三张表位于同一飞书多维表格文档（共用 recruit_bitable_app_token），仅 table id 不同。
	// 面试表未配置时回退到候选人决策表 table id（candidate_bitable_table_id），保持单表部署兼容；
	// 报表表不回退，未配置则 RunReportSync 直接跳过本轮，避免把报表评分误写进候选人决策表。
	keyInterviewBitableTableID = "plugin.linapro-recruit-pipeline.interview_bitable_table_id" // 面试状态表 table id（需求2，回退候选人表）
	keyReportBitableTableID    = "plugin.linapro-recruit-pipeline.report_bitable_table_id"    // 报表评分表 table id（需求3，不回退，未配置则跳过报表同步）

	// —— 报表同步（需求3）——
	keyRecruitReportIDs = "plugin.linapro-recruit-pipeline.recruit_report_ids" // 招聘报表 ID 列表（JSON int64 数组），空则跳过报表同步

	// —— 简历归属者回填（需求1.4）——
	// EhrApplications 的 basicInfo.owner 生产实测恒为 nil，故归属人改从独立的 owner 报表取：
	// 报表「申请」列（applicationId）+ HR 邮箱列 → owner_email_mapping 转工号 → empcap 解析 open_id。
	// owner 报表独立于 recruit_report_ids（需求3 的评分报表），未配置则跳过归属者取值与回填。
	keyOwnerReportID    = "plugin.linapro-recruit-pipeline.owner_report_id"    // owner 报表 ID（int64），未配置则跳过归属者取值与回填
	keyOwnerEmailColumn = "plugin.linapro-recruit-pipeline.owner_email_column" // owner 报表内 HR 邮箱列标题，默认 defaultOwnerEmailColumn
	// owner_email_mapping 是 HR 邮箱→Moka 工号的小映射表（公司约 10 个 HR、邮箱唯一）。
	// 报表 owner 列给的是 HR 邮箱而非工号，转成工号后才能复用既有的
	// empcap.LarkOpenIDResolverByEmployeeNos（工号是 employee.moka_employee_no 稳定外键）。
	keyOwnerEmailMapping = "plugin.linapro-recruit-pipeline.owner_email_mapping" // HR 邮箱→工号映射（JSON 对象 {email:工号}）

	// —— AI 判定等待时长（运行时读取，可热更新）——
	keyAIWaitMinutes = "plugin.linapro-recruit-pipeline.ai_wait_minutes" // AI 分析等待时长（分钟），默认 defaultAIWaitMinutes

	// —— 候选人轮询单周期处理上限（运行时读取，可热更新）——
	// max_candidates_per_cycle 限制单次候选人轮询最多处理多少个新候选人。每个新候选人需串行
	// 下载简历、上传飞书附件、写入 Bitable，成本较高；积压较大时用它把单周期时长控制在超时内，
	// 剩余候选人留待下一轮继续（已写入的会被 Bitable 索引跳过，不会重复）。
	keyMaxCandidatesPerCycle = "plugin.linapro-recruit-pipeline.max_candidates_per_cycle" // 单次候选人轮询处理新候选人上限，默认 DefaultMaxCandidatesPerCycle

	// —— 字段映射与 AI 判定 ——
	keyFieldMapping = "plugin.linapro-recruit-pipeline.field_mapping" // 逻辑字段→飞书列名映射（JSON 对象，需求1/2），空用 defaultFieldMapping
	// report_field_mapping 是需求3 报表评分回写专用的列映射，独立于 field_mapping。
	// key 为 Moka 报表列标题（源），value 为飞书目标列名，空用 defaultReportFieldMapping。
	keyReportFieldMapping = "plugin.linapro-recruit-pipeline.report_field_mapping" // 报表列映射（JSON 对象，需求3）
	// ai_verdict_field 对应 Bitable 中存放 AI 判定结论的列名；
	// ai_verdict_excluded 是 JSON 字符串数组，列出命中后直接出队、不推进的判定值。
	keyAIVerdictField    = "plugin.linapro-recruit-pipeline.ai_verdict_field"    // AI 判定结论列名，默认 defaultAIVerdictField
	keyAIVerdictExcluded = "plugin.linapro-recruit-pipeline.ai_verdict_excluded" // 不推进的判定值列表（JSON 数组），默认 defaultAIVerdictExcluded

	// —— 招聘阶段 ID ——
	// InitialScreeningStageID 是「初筛」阶段 ID（type=100），候选人轮询从该阶段拉取候选人；
	// InterviewStageID 是「面试」阶段 ID（type=201），需求2 从该阶段拉取候选人做面试状态回写；
	// DeptScreeningStageID 是「用人部门筛选」阶段 ID（type=200），AI 判定通过后推进到该阶段。
	keyInitialScreeningStageID = "plugin.linapro-recruit-pipeline.initial_screening_stage_id" // 初筛阶段 ID，默认 defaultInitialScreeningStageID
	keyInterviewStageID        = "plugin.linapro-recruit-pipeline.interview_stage_id"         // 面试阶段 ID，默认 defaultInterviewStageID
	keyDeptScreeningStageID    = "plugin.linapro-recruit-pipeline.dept_screening_stage_id"    // 用人部门筛选阶段 ID，默认 defaultDeptScreeningStageID
)

// ============================================================================
// 第三组：默认值（对应上面各 sys_config 键未配置时的回退值）。
// 阶段 ID 尤其需按真实 Moka 环境确认后用 sys_config 覆盖。
// ============================================================================
const (
	defaultInitialScreeningStageID int64 = 105019 // 初筛阶段（type=100）默认 ID
	defaultInterviewStageID        int64 = 105021 // 面试阶段（type=201）默认 ID
	defaultDeptScreeningStageID    int64 = 105020 // 用人部门筛选（type=200）默认 ID

	defaultAIWaitMinutes                = 10 // AI 分析等待时长（分钟）：从 analyzing_at 起等待多久后再读取 ai_verdict
	defaultCandidatePollIntervalMinutes = 5  // 候选人轮询默认间隔 5 分钟
	// 候选人轮询单次执行超时默认 30 分钟：远大于宿主 5 分钟默认 deadline，容纳积压候选人的串行简历下载/上传。
	defaultCandidatePollTimeoutMinutes = 30
	// 单次候选人轮询处理新候选人上限默认 50：按每人数秒的下载+上传成本，50 个可在超时内从容处理完，
	// 超出部分留待下一轮。运营可经 sys_config 上调。导出供 job 包作最终兜底。
	DefaultMaxCandidatesPerCycle = 50
	// 面试同步（需求2）
	defaultInterviewSyncIntervalMinutes = 5 // 面试同步定时任务默认轮询间隔（分钟）
	// 报表同步（需求3）
	defaultReportSyncIntervalMinutes = 5 // 人才画像评分报表同步定时任务默认轮询间隔（分钟）

	defaultAIVerdictField = "AI评估结论" // AI 判定结论默认列名

	defaultOwnerEmailColumn = "简历接收邮箱" // owner 报表内 HR 邮箱列标题默认值（需求1.4）
)

// defaultAIVerdictExcluded 列出命中后直接出队、不推进面试的 AI 判定值。
// 不在此列表中的非空判定均视为"通过"，触发推进 MoveApplicationStage。
// 可通过 sys_config ai_verdict_excluded（JSON 字符串数组）覆盖。
var defaultAIVerdictExcluded = []string{
	"建议淘汰",
	"无匹配类型",
	"谨慎考虑",
}

// field_mapping 的逻辑字段键常量。sys_config field_mapping、defaultFieldMapping、
// 候选人轮询任务与需求2 面试同步任务都按这些稳定键关联飞书列名，集中定义避免多处
// 硬编码同一字符串。运营人员改的是各键映射到的飞书列名（值），键本身是代码契约。
const (
	FieldKeyName           = "name"            // 候选人姓名列
	FieldKeyContact        = "contact"         // 联系方式列（包含电话和邮箱）
	FieldKeyExperience     = "experience"      // 从业年限列
	FieldKeyAcademicDegree = "academic_degree" // 学历列
	FieldKeyLastSchool     = "last_school"     // 毕业院校列
	FieldKeySource         = "source"          // 简历来源列
	FieldKeyResume         = "resume"          // 简历纯文本列
	FieldKeyResumeFile     = "resume_file"     // 简历附件列
	// FieldKeyResumeOwner 是「简历归属者」人员列。归属人工号来自 owner 报表（需求1.4·FB-18：
	// basicInfo.owner 生产实测恒为 nil，改从 owner_report_id 报表取 HR 邮箱经 owner_email_mapping
	// 转工号），再经 empcap.LarkOpenIDResolverByEmployeeNos 解析为 open_id 集合后写入。
	// 该列为飞书人员字段（TypeUser），不接受纯字符串，故解析结果经 CreateOp/UpdateOp.Persons
	// 旁路交给共享库序列化，不进 Fields 文本列。
	FieldKeyResumeOwner = "resume_owner" // 简历归属者人员列（工号→open_id）
	// 需求2 面试状态表字段键。
	FieldKeyApplicationID  = "applicationId"   // applicationId 字段，与轮次组合为唯一键
	FieldKeyInterviewRound = "interview_round" // 面试轮次名称（roundName），与 applicationId 组合为唯一键
	FieldKeyCandidateName  = "candidate_name"  // 候选人姓名（basicInfo.name）
	FieldKeyInterviewType  = "interview_type"  // 面试方式（现场/电话/视频面试）
	FieldKeyInterviewTime  = "interview_time"  // 面试开始时间（startTime）
	FieldKeyInterviewer    = "interviewer"     // 面试官工号（interviewInfo[].interviewerFeedbacks[].interviewer.employeeId→open_id）
	FieldKeyVideoURL       = "video_url"       // 视频面试链接（intervieweeVideoUrl）
	FieldKeyAttendResult   = "attend_result"   // 是否应约（status=已取消 即未应约）
	FieldKeyUnattendReason = "unattend_reason" // 未应约原因（取自 basicInfo.archiveReasons）
)

// 需求3 报表评分表列映射固定的 Moka 报表源列标题键。
// report_field_mapping 的 key 是 Moka 报表 headers 的 title（源列名），value 是飞书目标列名。
// 「申请」列的 value（默认 applicationId）作为报表评分表唯一键列，由报表同步任务单独处理。
const ReportSourceApplicationTitle = "申请" // Moka 报表 applicationId 列的固定标题（唯一键源列）

// defaultFieldMapping 将逻辑候选人字段键映射到飞书 Bitable 列名。
// 当 sys_config field_mapping 未配置时使用，保证插件开箱即用。
// 运营人员可通过 field_mapping sys_config 键覆盖（免部署改列名）。
var defaultFieldMapping = map[string]string{
	FieldKeyName:           "候选人",
	FieldKeyContact:        "联系方式",
	FieldKeyExperience:     "从业年限",
	FieldKeyAcademicDegree: "学历",
	FieldKeyLastSchool:     "毕业院校",
	FieldKeySource:         "简历来源",          // 简历来源渠道，取自 basicInfo.sourceName（如「Boss直聘」）
	FieldKeyResume:         "简历内容",          // 简历纯文本，值由 Moka resumeContent 接口注入
	FieldKeyResumeFile:     "简历附件",          // 简历原文件附件，值由 basicInfo.resumeUrl 下载后转存飞书注入
	FieldKeyResumeOwner:    "简历归属者",         // 简历归属者人员列，值由 owner 报表 HR 邮箱经 owner_email_mapping 转工号、再经 empcap 解析为 open_id 注入
	FieldKeyApplicationID:  "applicationId", // 需求2：Moka applicationId，与 interview_round 组合为面试状态表唯一键
	FieldKeyInterviewRound: "面试轮次",          // 需求2：面试轮次名称（roundName），与 applicationId 组合为唯一键
	FieldKeyCandidateName:  "姓名",            // 需求2：候选人姓名（basicInfo.name）
	FieldKeyInterviewType:  "面试方式",          // 需求2：面试方式（现场/电话/视频面试）
	FieldKeyInterviewTime:  "面试时间",          // 需求2：面试开始时间（startTime）
	FieldKeyInterviewer:    "面试官",           // 需求2：面试官人员列（interviewInfo[].interviewerFeedbacks[].interviewer.employeeId 工号→open_id）
	FieldKeyVideoURL:       "视频面试链接",        // 需求2：视频面试链接（intervieweeVideoUrl）
	FieldKeyAttendResult:   "是否应约",          // 需求2：是否应约（status=已取消 即未应约）
	FieldKeyUnattendReason: "未应约原因",         // 需求2：未应约原因（取自 basicInfo.archiveReasons）
}

// defaultReportFieldMapping 是需求3 报表评分回写的默认列映射，独立于 defaultFieldMapping（需求1/2 共用）。
// key 为 Moka 报表 headers 的 title（源列名），value 为飞书报表评分表的目标列名；
// 报表同步任务按 key 定位 Moka 列 dataIndex、按 value 回写飞书列，修正「两侧列名相同」的错误假设。
// 「申请」映射到的飞书列（applicationId）作为唯一键列由报表同步任务单独处理。
// 运营人员可通过 report_field_mapping sys_config 键覆盖（免部署改列名）。
// 「匹配度等级-初/中/高」是三个不同报表数据源的匹配度列，因各表字段名无法统一而分别命名，
// 但在飞书报表评分表中都回写到同一列「匹配度等级」（多源列→单目标列合并）。同一候选人在多个
// 报表命中时，按 recordID 合并、后出现的报表覆盖先出现的值（见 mergeReportUpdates）。
var defaultReportFieldMapping = map[string]string{
	ReportSourceApplicationTitle: "applicationId", // Moka「申请」列 → 飞书 applicationId（唯一键）
	"候选人":                        "姓名",            // Moka「候选人」列 → 飞书「姓名」列
	"最终总分":                       "人才画像评分",        // Moka「最终总分」列 → 飞书「人才画像评分」列
	"匹配度等级-初":                    "匹配度等级",         // Moka 数据源1「匹配度等级-初」列 → 飞书「匹配度等级」列
	"匹配度等级-中":                    "匹配度等级",         // Moka 数据源2「匹配度等级-中」列 → 飞书「匹配度等级」列
	"匹配度等级-高":                    "匹配度等级",         // Moka 数据源3「匹配度等级-高」列 → 飞书「匹配度等级」列
}

// Config 是插件每次执行周期解析出的完整配置。
type Config struct {
	// 飞书应用凭证（静态 host 配置）。
	LarkAppID     string // 飞书应用 App ID（来自静态 host 配置）
	LarkAppSecret string // 飞书应用 App Secret（来自静态 host 配置）
	// Bitable 目标表（sys_config 可调）。三张表共用同一 App token，仅 table id 不同。
	BitableAppToken string // 飞书多维表格所在 App 的 token（三表共用，sys_config 可调）
	// CandidateBitableTableID 是候选人决策表 table id：候选人轮询写入、AI 判定回表读取共用（recordID 必须同表）。
	CandidateBitableTableID string // 候选人决策表 table id（RunCandidatePoll + RunAIVerdict，sys_config 可调）
	// InterviewBitableTableID 是面试取消状态回写表（需求2）的 table id；未配置回退候选人决策表。
	// ReportBitableTableID 是报表评分回写表（需求3）的 table id；不回退，未配置为空串则跳过报表同步。
	InterviewBitableTableID string // 面试状态回写表 table id（需求2，sys_config 可调，未配置回退候选人表）
	ReportBitableTableID    string // 报表评分回写表 table id（需求3，sys_config 可调，未配置则跳过报表同步，不回退）

	// Moka 招聘 BasicAuth 凭证（用于调用 moka-recruit 客户端）。
	MokaAPIKey  string // 调用 Moka 招聘 API 的 BasicAuth apiKey（组织级共享）
	MokaBaseURL string // Moka 招聘 API 的基础 URL
	// MokaOperatorEmail 是调用 interview-information（视频面试链接）接口所需的组织管理员邮箱。
	MokaOperatorEmail string // 视频面试链接接口所需管理员邮箱（静态 host 配置）

	// 招聘流水线阶段 ID。
	InitialScreeningStageID int64 // 「初筛」阶段 ID（type=100），候选人轮询从该阶段拉取候选人
	InterviewStageID        int64 // 「面试」阶段 ID（type=201），需求2 从该阶段拉取候选人做面试取消状态回写
	DeptScreeningStageID    int64 // 「用人部门筛选」阶段 ID（type=200），AI 判定通过后将候选人推进到该阶段

	// 报表同步：为空时跳过本轮报表同步任务。
	RecruitReportIDs []int64 // Moka 招聘报表 ID 列表，报表同步的数据源；为空则跳过报表同步

	// 简历归属者回填（需求1.4）。owner 报表独立于 RecruitReportIDs（需求3 评分报表）。
	OwnerReportID int64 // owner 报表 ID；未配置（0）则跳过归属者取值与回填，不影响其余字段
	// OwnerEmailColumn 是 owner 报表内 HR 邮箱列标题，未配置回退 defaultOwnerEmailColumn。
	OwnerEmailColumn string // owner 报表 HR 邮箱列标题（sys_config，默认「简历接收邮箱」）
	// OwnerEmailMapping 是 HR 邮箱→Moka 工号映射；key 在加载时已做 TrimSpace+ToLower 归一化，
	// 查找前对报表邮箱值同样归一化，避免大小写/空格漏配。
	OwnerEmailMapping map[string]string // HR 邮箱→工号映射（sys_config，JSON，key 已归一化）

	// AI 判定任务：从 analyzing_at 起等待多少分钟后再读取 ai_verdict。
	AIWaitMinutes int // AI 分析等待时长（分钟）：从 analyzing_at 起等待多久后再读取 ai_verdict

	// MaxCandidatesPerCycle 限制单次候选人轮询最多处理多少个新候选人，超出部分留待下一轮。
	// 未配置或非正时回退 DefaultMaxCandidatesPerCycle。
	MaxCandidatesPerCycle int // 单次候选人轮询处理新候选人上限（sys_config 可调）

	// 候选人轮询（需求1.1）
	CandidatePollIntervalMinutes int // 候选人轮询定时任务轮询间隔（分钟）
	// 面试同步（需求2）
	InterviewSyncIntervalMinutes int // 面试同步定时任务轮询间隔（分钟）
	// 人才画像评分报表同步（需求3）
	ReportSyncIntervalMinutes int // 人才画像评分报表同步定时任务轮询间隔（分钟）

	// FieldMapping 将逻辑候选人字段键映射到候选人轮询任务写入用的飞书列名（需求1/2），
	// 未配置时回退到 defaultFieldMapping。
	FieldMapping map[string]string // 逻辑字段→飞书列名映射（sys_config，JSON，需求1/2）

	// ReportFieldMapping 是需求3 报表评分回写专用的列映射，独立于 FieldMapping。
	// key 为 Moka 报表列标题（源），value 为飞书目标列名；未配置时回退 defaultReportFieldMapping。
	ReportFieldMapping map[string]string // Moka 报表列标题→飞书目标列名映射（sys_config，JSON，需求3）

	// AIVerdictField 是 Bitable 中存放 AI 判定结论的列名，默认"AI评估结论"。
	// 可通过 sys_config ai_verdict_field 覆盖，以适应不同表结构。
	AIVerdictField string // AI 判定结论列名（sys_config 可调）

	// AIVerdictExcluded 列出命中后直接出队、不推进面试的 AI 判定值。
	// 不在此列表中的非空判定均视为"通过"，触发 MoveApplicationStage。
	// 可通过 sys_config ai_verdict_excluded（JSON 字符串数组）覆盖。
	AIVerdictExcluded []string // 不推进的判定值列表（sys_config 可调，JSON 数组）
}

// CandidatePollIntervalMinutes 仅读取候选人轮询任务已配置的轮询间隔（分钟），
// 未配置或无效时返回 defaultCandidatePollIntervalMinutes。在完整配置加载之前的任务注册阶段调用。
func CandidatePollIntervalMinutes(ctx context.Context, services capability.Services) int {
	return intervalMinutesOrDefault(ctx, services, keyCandidatePollIntervalMinutes, defaultCandidatePollIntervalMinutes)
}

// InterviewSyncIntervalMinutes 仅读取面试同步任务已配置的轮询间隔（分钟），
// 未配置或无效时返回 defaultInterviewSyncIntervalMinutes。在完整配置加载之前的任务注册阶段调用。
func InterviewSyncIntervalMinutes(ctx context.Context, services capability.Services) int {
	return intervalMinutesOrDefault(ctx, services, keyInterviewSyncIntervalMinutes, defaultInterviewSyncIntervalMinutes)
}

// ReportSyncIntervalMinutes 仅读取报表同步任务已配置的轮询间隔（分钟），
// 未配置或无效时返回 defaultReportSyncIntervalMinutes。在完整配置加载之前的任务注册阶段调用。
func ReportSyncIntervalMinutes(ctx context.Context, services capability.Services) int {
	return intervalMinutesOrDefault(ctx, services, keyReportSyncIntervalMinutes, defaultReportSyncIntervalMinutes)
}

// CandidatePollTimeoutMinutes 读取候选人轮询单次执行超时（分钟），未配置或非正时返回
// defaultCandidatePollTimeoutMinutes。在任务注册阶段调用一次，用于回调内解绑宿主 deadline 后重设超时。
func CandidatePollTimeoutMinutes(ctx context.Context, services capability.Services) int {
	return intervalMinutesOrDefault(ctx, services, keyCandidatePollTimeoutMinutes, defaultCandidatePollTimeoutMinutes)
}

// intervalMinutesOrDefault 读取指定 sys_config 键的轮询间隔（分钟），
// services/HostConfig 缺失或值非正时返回传入的 def 默认值。
func intervalMinutesOrDefault(ctx context.Context, services capability.Services, key string, def int) int {
	if services == nil || services.HostConfig() == nil {
		return def
	}
	n, _ := services.HostConfig().Int(ctx, key, def)
	if n <= 0 {
		return def
	}
	return n
}

// Load 解析完整配置。必需凭证缺失时返回 error，调用方可据此记录日志并跳过本轮。
func Load(ctx context.Context, services capability.Services) (*Config, error) {
	if services == nil {
		return nil, gerror.New("recruit-pipeline: 宿主服务不可用")
	}
	hc := services.HostConfig()
	if hc == nil {
		return nil, gerror.New("recruit-pipeline: 宿主配置能力不可用")
	}

	larkAppID, _ := hc.String(ctx, keyLarkAppID, "")
	larkAppSecret, _ := hc.String(ctx, keyLarkAppSecret, "")

	if larkAppID == "" || larkAppSecret == "" {
		return nil, gerror.New("recruit-pipeline: 飞书凭证不完整（lark.appId/lark.appSecret）")
	}

	mokaAPIKey, _ := hc.String(ctx, keyMokaAPIKey, "")
	mokaBaseURL, _ := hc.String(ctx, keyMokaBaseURL, "")
	mokaOperatorEmail, _ := hc.String(ctx, keyMokaOperatorEmail, "")

	// 定时任务间隔从静态配置读取（修改后需重启）
	candidatePollInterval, _ := hc.Int(ctx, keyCandidatePollIntervalMinutes, defaultCandidatePollIntervalMinutes)
	interviewSyncInterval, _ := hc.Int(ctx, keyInterviewSyncIntervalMinutes, defaultInterviewSyncIntervalMinutes)
	reportSyncInterval, _ := hc.Int(ctx, keyReportSyncIntervalMinutes, defaultReportSyncIntervalMinutes)

	cfg := &Config{
		LarkAppID:                    larkAppID,
		LarkAppSecret:                larkAppSecret,
		MokaAPIKey:                   mokaAPIKey,
		MokaBaseURL:                  mokaBaseURL,
		MokaOperatorEmail:            mokaOperatorEmail,
		InitialScreeningStageID:      defaultInitialScreeningStageID,
		InterviewStageID:             defaultInterviewStageID,
		DeptScreeningStageID:         defaultDeptScreeningStageID,
		AIWaitMinutes:                defaultAIWaitMinutes,
		CandidatePollIntervalMinutes: candidatePollInterval,
		InterviewSyncIntervalMinutes: interviewSyncInterval,
		ReportSyncIntervalMinutes:    reportSyncInterval,
		FieldMapping:                 maps.Clone(defaultFieldMapping),
		ReportFieldMapping:           maps.Clone(defaultReportFieldMapping),
		AIVerdictField:               defaultAIVerdictField,
		AIVerdictExcluded:            defaultAIVerdictExcluded,
		OwnerEmailColumn:             defaultOwnerEmailColumn,
		MaxCandidatesPerCycle:        DefaultMaxCandidatesPerCycle,
	}

	loadSysConfig(ctx, hc, cfg)
	return cfg, nil
}

// loadSysConfig 从 sys_config 行填充可选字段。读取失败时静默忽略，保留默认值。
func loadSysConfig(ctx context.Context, hc hostconfigcap.Service, cfg *Config) {
	sc := hc.SysConfig()
	if sc == nil {
		return
	}

	if v, ok := sysConfigString(ctx, sc, keyBitableAppToken); ok {
		cfg.BitableAppToken = v
	}
	if v, ok := sysConfigString(ctx, sc, keyCandidateBitableTableID); ok {
		cfg.CandidateBitableTableID = v
	}
	// 面试表 table id 未配置时回退到候选人决策表，保持单表部署兼容。
	if v, ok := sysConfigString(ctx, sc, keyInterviewBitableTableID); ok {
		cfg.InterviewBitableTableID = v
	} else {
		cfg.InterviewBitableTableID = cfg.CandidateBitableTableID
	}
	// 报表评分表 table id 不回退：未配置时保持空串，RunReportSync 直接跳过本轮，避免误写候选人决策表。
	if v, ok := sysConfigString(ctx, sc, keyReportBitableTableID); ok {
		cfg.ReportBitableTableID = v
	}
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyInitialScreeningStageID); ok {
		cfg.InitialScreeningStageID = v
	}
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyInterviewStageID); ok {
		cfg.InterviewStageID = v
	}
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyDeptScreeningStageID); ok {
		cfg.DeptScreeningStageID = v
	}
	if raw, ok := sysConfigString(ctx, sc, keyRecruitReportIDs); ok {
		var ids []int64
		if err := json.Unmarshal([]byte(raw), &ids); err == nil && len(ids) > 0 {
			cfg.RecruitReportIDs = ids
		}
	}
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyAIWaitMinutes); ok {
		cfg.AIWaitMinutes = int(v)
	}
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyMaxCandidatesPerCycle); ok {
		cfg.MaxCandidatesPerCycle = int(v)
	}

	if raw, ok := sysConfigString(ctx, sc, keyFieldMapping); ok {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil && len(m) > 0 {
			// 覆盖式合并：只替换运营显式配置的键，保留 defaultFieldMapping
			// 中未覆盖的键（尤其是需求2 的 applicationId/interview_round 等组合键列），
			// 避免运营只配了需求1.1 候选人列时丢失需求2 列导致面试同步守卫失败。
			// FieldMapping 为 nil 时（如直接调用 loadSysConfig 未经 Load 初始化）先补一份默认映射底座。
			if cfg.FieldMapping == nil {
				cfg.FieldMapping = maps.Clone(defaultFieldMapping)
			}
			maps.Copy(cfg.FieldMapping, m)
		}
	}
	// 需求3 报表列映射独立于 field_mapping，同样采用覆盖式合并：
	// 只替换运营显式配置的 Moka 报表列标题，保留 defaultReportFieldMapping 中未覆盖的键，
	// 避免运营只改一列时丢失其余列映射。ReportFieldMapping 为 nil 时先补默认底座。
	if raw, ok := sysConfigString(ctx, sc, keyReportFieldMapping); ok {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil && len(m) > 0 {
			if cfg.ReportFieldMapping == nil {
				cfg.ReportFieldMapping = maps.Clone(defaultReportFieldMapping)
			}
			maps.Copy(cfg.ReportFieldMapping, m)
		}
	}
	if v, ok := sysConfigString(ctx, sc, keyAIVerdictField); ok {
		cfg.AIVerdictField = v
	}
	if raw, ok := sysConfigString(ctx, sc, keyAIVerdictExcluded); ok {
		var list []string
		if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
			cfg.AIVerdictExcluded = list
		}
	}

	// —— 简历归属者回填（需求1.4）——
	// owner_report_id 未配置（或非正）时保持 0，loadOwnerMap 据此跳过归属者取值与回填。
	if v, ok := sysConfigPositiveInt64(ctx, sc, keyOwnerReportID); ok {
		cfg.OwnerReportID = v
	}
	if v, ok := sysConfigString(ctx, sc, keyOwnerEmailColumn); ok {
		cfg.OwnerEmailColumn = v
	}
	// owner_email_mapping 的 key（HR 邮箱）在存入前统一 TrimSpace+ToLower 归一化，
	// 与 loadOwnerMap 查找侧的归一化对齐，避免大小写/空格漏配。
	if raw, ok := sysConfigString(ctx, sc, keyOwnerEmailMapping); ok {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil && len(m) > 0 {
			normalized := make(map[string]string, len(m))
			for email, empNo := range m {
				key := strings.ToLower(strings.TrimSpace(email))
				if key == "" {
					continue
				}
				normalized[key] = empNo
			}
			if len(normalized) > 0 {
				cfg.OwnerEmailMapping = normalized
			}
		}
	}
}

// sysConfigString 读取非空的 sys_config 值。键不存在、出错或值为空白时 ok 为 false，
// 调用方保留默认值。
func sysConfigString(ctx context.Context, sc hostconfigcap.SysConfigService, key hostconfigcap.SysConfigKey) (string, bool) {
	info, err := sc.Get(ctx, key)
	if err != nil || info == nil || info.Value == "" {
		return "", false
	}
	return info.Value, true
}

// sysConfigPositiveInt64 读取并解析为正整数的 sys_config 值。
// 键不存在、无法解析或不大于零时 ok 为 false。
func sysConfigPositiveInt64(ctx context.Context, sc hostconfigcap.SysConfigService, key hostconfigcap.SysConfigKey) (int64, bool) {
	raw, ok := sysConfigString(ctx, sc, key)
	if !ok {
		return 0, false
	}
	var n int64
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
