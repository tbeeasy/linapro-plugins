package moka

import (
	"context"
	"encoding/json"
	"fmt"
	"lina-core/pkg/logger"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	ehrApplicationsPath      = "/api-platform/v2/data/ehrApplications"
	interviewInformationPath = "/api-platform/v1/interview/interview-information"
	moveApplicationStagePath = "/api-platform/v1/applications/move_application_stage"
)

// InterviewType 是面试方式的命名类型，取自 Moka `interviewInfo.interviewType`
// 字符串枚举。用于集中管理判定「是否视频面试」的枚举语义，避免硬编码字符串。
type InterviewType string

const (
	// InterviewTypeVideo 视频面试，是否应约后需调用 interview-information 补视频链接的唯一类型。
	InterviewTypeVideo InterviewType = "视频面试"
	// InterviewTypeOnsite 现场面试。
	InterviewTypeOnsite InterviewType = "现场面试"
	// InterviewTypePhone 电话面试。
	InterviewTypePhone InterviewType = "电话面试"
)

// InterviewStatus 是面试状态的命名类型，取自 Moka `interviewInfo.status` 字符串枚举。
type InterviewStatus string

const (
	// InterviewStatusCancelled 已取消，语义等价「未应约」。
	InterviewStatusCancelled InterviewStatus = "已取消"
	// InterviewStatusOngoing 未结束。
	InterviewStatusOngoing InterviewStatus = "未结束"
	// InterviewStatusFinished 已结束。
	InterviewStatusFinished InterviewStatus = "已结束"
)

// ApplicationStage 是嵌在候选人 basicInfo 中的招聘阶段引用。
type ApplicationStage struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"`
}

// ApplicationBasicInfo 是 ehrApplications 返回的候选人身份与归档字段。
// ArchiveReasons 归档原因来源不稳定（Moka 文档标注为 string，实际可能是含
// name 字段的对象），故用 archiveName 容错解析，调用方通过 ArchiveReasonName 读取。
type ApplicationBasicInfo struct {
	ApplicationID  int64             `json:"applicationId"`
	CandidateID    int64             `json:"candidateId"`
	Name           string            `json:"name"`
	Phone          string            `json:"phone"`          // 候选人电话
	Email          string            `json:"email"`          // 候选人邮箱
	Experience     int               `json:"experience"`     // 工作年限
	AcademicDegree string            `json:"academicDegree"` // 学历
	LastSchool     string            `json:"lastSchool"`     // 毕业院校
	SourceName     string            `json:"sourceName"`     // 简历来源
	ResumeURL      string            `json:"resumeUrl"`      // 简历文件下载链接（48h 有效）
	ResumeKey      string            `json:"resumeKey"`      // 简历文件 key
	AppliedAt      string            `json:"appliedAt"`      // 申请时间（ISO 8601）
	Stage          ApplicationStage  `json:"stage"`
	Archived       bool              `json:"archived"`
	ArchiveReasons archiveName       `json:"archiveReasons"`
	Owner          *ApplicationOwner `json:"owner"` // 候选人归属人（简历 owner）
}

// ApplicationOwner 是候选人归属人（简历 owner）信息。
type ApplicationOwner struct {
	Name       string `json:"name"`       // 归属人姓名
	Phone      string `json:"phone"`      // 归属人电话
	Email      string `json:"email"`      // 归属人邮箱
	Number     string `json:"number"`     // 归属人工号（moka_employee_no 外键）
	EmployeeID string `json:"employeeId"` // 归属人工号（同 Number，Moka 返回两个字段）
}

// ArchiveReasonName 返回归档原因文本，无归档原因时返回空串。
func (b ApplicationBasicInfo) ArchiveReasonName() string {
	return string(b.ArchiveReasons)
}

// InterviewerRef 是面试官反馈条目内的面试官人员引用。EmployeeID 取 employeeId 字段，
// 即该面试官的 Moka 员工工号（与 pushCandidate 事件 owners.employee_id 同工号语义），
// 供人员字段按工号解析为 open_id。Name 仅作排障可读，不用于人员字段编码。
type InterviewerRef struct {
	Name       string `json:"name"`
	EmployeeID string `json:"employeeId"`
}

// InterviewerFeedback 是 interviewInfo 条目 interviewerFeedbacks 数组的单个元素，
// 承载一名面试官及其反馈；本同步仅取其中的面试官人员引用（Interviewer）。
type InterviewerFeedback struct {
	Interviewer InterviewerRef `json:"interviewer"`
}

// InterviewRound 是 ehrApplications 返回体内 interviewInfo 的单个面试轮次条目。
// StartTime 用 flexNumberString 容错解析（Moka 可能返回毫秒时间戳数字或 ISO 字符串）。
// IntervieweeVideoURL 在 interviewInfo 中可能为空，需求2 视频面试场景会用
// interview-information 接口的返回值覆盖。
// InterviewerFeedbacks 承载该轮次全部面试官（一轮可能多名），面试官工号由
// InterviewerEmployeeNos 汇总提取，用于人员字段按工号解析。
type InterviewRound struct {
	Round                int                   `json:"round"`
	RoundName            string                `json:"roundName"`
	InterviewType        InterviewType         `json:"interviewType"`
	Status               InterviewStatus       `json:"status"`
	StartTime            flexNumberString      `json:"startTime"`
	IntervieweeVideoURL  string                `json:"intervieweeVideoUrl"`
	InterviewerFeedbacks []InterviewerFeedback `json:"interviewerFeedbacks"`
}

// InterviewerEmployeeNos 返回该轮次全部面试官的工号
// （interviewerFeedbacks[].interviewer.employeeId），按出现顺序去重、跳过空工号；
// 无面试官时返回 nil。供「面试官」人员列按工号批量解析为 open_id。
func (r InterviewRound) InterviewerEmployeeNos() []string {
	seen := make(map[string]struct{}, len(r.InterviewerFeedbacks))
	nos := make([]string, 0, len(r.InterviewerFeedbacks))
	for _, fb := range r.InterviewerFeedbacks {
		no := strings.TrimSpace(fb.Interviewer.EmployeeID)
		if no == "" {
			continue
		}
		if _, dup := seen[no]; dup {
			continue
		}
		seen[no] = struct{}{}
		nos = append(nos, no)
	}
	return nos
}

// EhrApplication 是 ehrApplications 响应列表的单个候选人条目。
// InterviewInfo 用 interviewRounds 容错解析（可能是 null、单对象或数组）。
type EhrApplication struct {
	BasicInfo     ApplicationBasicInfo `json:"basicInfo"`
	InterviewInfo interviewRounds      `json:"interviewInfo"`
}

// Rounds 返回该候选人的全部面试轮次条目，无面试时返回 nil。
func (a EhrApplication) Rounds() []InterviewRound {
	return a.InterviewInfo
}

type ehrApplicationsEnvelope struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Data []EhrApplication `json:"data"`
	// Next 是 Moka 的分页游标：非空表示还有下一页，下次请求仅需带上该值即可续拉。
	Next string `json:"next"`
}

// EhrApplicationsQuery 是 ehrApplications 的查询过滤条件。Archived 为 nil 时
// 不传 archived 参数；时间范围为空时不传，传入时须为北京时间
// `YYYY-MM-DDTHH:mm:ss.sssZ` 格式，由 Moka 服务端按时间过滤。
type EhrApplicationsQuery struct {
	StageIDs          []int64 // 招聘阶段 ID 列表，必填
	Archived          *bool   // 归档过滤：nil=不传，true=仅已归档，false=仅未归档
	UpdateAtStartTime string  // 更新时间起（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
	UpdateAtEndTime   string  // 更新时间止（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
	// 申请时间过滤（候选人投递简历的时间）
	ApplicationAppliedAtStartTime string // 申请时间起（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
	ApplicationAppliedAtEndTime   string // 申请时间止（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
}

// EhrApplications 按 query 拉取候选人。支持按阶段 ID、归档状态和更新时间/申请时间范围过滤，
// 过滤均由 Moka 服务端完成。阶段无候选人时返回空切片（非 error）。
// Moka 该接口通过与 data 平级的 next 游标分页：返回非空 next 表示还有下一页，
// 续拉时只需带上 next 参数（其余过滤条件无需再传），直至 next 为空聚合全部结果。
func (c *Client) EhrApplications(ctx context.Context, query EhrApplicationsQuery) ([]EhrApplication, error) {
	payload := map[string]any{"stageIds": query.StageIDs}
	if query.Archived != nil {
		payload["archived"] = *query.Archived
	}
	if query.UpdateAtStartTime != "" {
		payload["updateAtStartTime"] = query.UpdateAtStartTime
	}
	if query.UpdateAtEndTime != "" {
		payload["updateAtEndTime"] = query.UpdateAtEndTime
	}
	if query.ApplicationAppliedAtStartTime != "" {
		payload["applicationAppliedAtStartTime"] = query.ApplicationAppliedAtStartTime
	}
	if query.ApplicationAppliedAtEndTime != "" {
		payload["applicationAppliedAtEndTime"] = query.ApplicationAppliedAtEndTime
	}

	var all []EhrApplication
	for page := 1; ; page++ {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, gerror.Wrap(err, "moka-recruit: 序列化 ehrApplications 请求体失败")
		}

		// 打印请求参数，便于排查日期格式等服务端解析问题。
		logger.Debugf(ctx, "moka-recruit: ehrApplications 第 %d 页请求参数 %s", page, string(body))

		raw, err := c.PostJSON(ctx, ehrApplicationsPath, body)
		if err != nil {
			return nil, err
		}

		var env ehrApplicationsEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, gerror.Wrapf(err, "moka-recruit: 解析 ehrApplications 响应失败: %s", truncate(string(raw), 512))
		}
		if env.Code != 200 {
			return nil, gerror.Newf("moka-recruit: ehrApplications 接口返回 code=%d msg=%q", env.Code, env.Msg)
		}

		all = append(all, env.Data...)
		logger.Debugf(ctx, "moka-recruit: ehrApplications 第 %d 页返回 %d 条, next=%q", page, len(env.Data), env.Next)

		// next 为空表示无更多分页，结束聚合。
		if strings.TrimSpace(env.Next) == "" {
			break
		}
		// 续拉时仅需带上 next 游标，其余过滤条件不再传递。
		payload = map[string]any{"next": env.Next}
	}

	return all, nil
}

// MoveApplicationStage 将候选人申请推进到指定阶段。
// 使用 PutQuery，因为 Moka 期望无请求体的 PUT，参数放在查询串中。
// 该 Moka 端点以 code=0 表示成功。
func (c *Client) MoveApplicationStage(ctx context.Context, applicationID int64, stageID int64) error {
	params := url.Values{}
	params.Set("applicationId", fmt.Sprintf("%d", applicationID))
	params.Set("stageId", fmt.Sprintf("%d", stageID))

	if err := c.PutQuery(ctx, moveApplicationStagePath, params); err != nil {
		return err
	}
	return nil
}

// InterviewInformationEntity 是 interview-information 接口返回的单条面试实体，
// 需求2 视频面试场景从中取 IntervieweeVideoURL 作为面试链接。
type InterviewInformationEntity struct {
	ID                  int64  `json:"id"`
	Round               int    `json:"round"`
	RoundName           string `json:"roundName"`
	StartTime           int64  `json:"startTime"`
	IntervieweeVideoURL string `json:"intervieweeVideoUrl"`
}

// InterviewInformation 是 interview-information 接口返回的单个 application 的面试信息。
type InterviewInformation struct {
	ApplicationID int64                        `json:"applicationId"`
	Entities      []InterviewInformationEntity `json:"entities"`
}

type interviewInformationEnvelope struct {
	Code    int                    `json:"code"`
	Msg     string                 `json:"msg"`
	Success bool                   `json:"success"`
	Data    []InterviewInformation `json:"data"`
}

// GetInterviewInformation 查询给定 application 的面试详情，用于需求2 视频面试场景
// 补齐 intervieweeVideoUrl。email 为组织管理员邮箱（Moka 接口必填）。
// 该接口 code=0 为成功（与 v3 端点一致）。applicationIDs 为空时直接返回空切片，不发请求。
func (c *Client) GetInterviewInformation(ctx context.Context, applicationIDs []int64, email string) ([]InterviewInformation, error) {
	if len(applicationIDs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"applicationIds": applicationIDs,
		"email":          email,
	})
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: 序列化 interview-information 请求体失败")
	}

	logger.Debugf(ctx, "moka-recruit: GetInterviewInformation 请求参数 %s", string(body))

	raw, err := c.PostJSON(ctx, interviewInformationPath, body)
	if err != nil {
		return nil, err
	}

	// 打印原始返回值
	logger.Debugf(ctx, "moka-recruit: GetInterviewInformation 原始请求返回值 %s", string(raw))

	var env interviewInformationEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: 解析 interview-information 响应失败: %s", truncate(string(raw), 512))
	}
	if env.Code != 0 {
		return nil, gerror.Newf("moka-recruit: interview-information 接口返回 code=%d msg=%q", env.Code, env.Msg)
	}

	// 打印返回值
	logger.Debugf(ctx, "moka-recruit: GetInterviewInformation 格式化之后请求返回值 %+v", env.Data)

	return env.Data, nil
}

// archiveName 容错解析 Moka `basicInfo.archiveReasons`：可能是字符串（文档标注类型），
// 也可能是含 name 字段的对象。两种形态都归一化为归档原因文本。
type archiveName string

// UnmarshalJSON 兼容字符串与 {"name": "..."} 对象两种归档原因形态。
func (a *archiveName) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*a = ""
		return nil
	}
	// 形态一：纯字符串。
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = archiveName(s)
		return nil
	}
	// 形态二：含 name 字段的对象。
	if trimmed[0] == '{' {
		var obj struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		*a = archiveName(obj.Name)
		return nil
	}
	// 其它形态（如数字/数组）忽略，归一化为空串，避免整体解析失败。
	*a = ""
	return nil
}

// flexNumberString 容错解析可能是数字（毫秒时间戳）或字符串（ISO 时间）的字段，
// 统一保存为原始字符串，交由调用方按需解释。
type flexNumberString string

// UnmarshalJSON 兼容 JSON number 与 string 两种时间表达。
func (f *flexNumberString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexNumberString(s)
		return nil
	}
	// 数字原样保留（毫秒时间戳）。
	*f = flexNumberString(trimmed)
	return nil
}

// String 返回时间字段的原始字符串表达。
func (f flexNumberString) String() string { return string(f) }

// interviewRounds 容错解析 ehrApplications 的 `interviewInfo` 字段：可能是 null、
// 单个面试轮次对象或面试轮次数组。归一化为切片，供调用方逐轮处理。
type interviewRounds []InterviewRound

// UnmarshalJSON 兼容 null、单对象与数组三种 interviewInfo 形态。
func (r *interviewRounds) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*r = nil
		return nil
	}
	// 形态一：数组。
	if trimmed[0] == '[' {
		var list []InterviewRound
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		*r = list
		return nil
	}
	// 形态二：单个对象。
	if trimmed[0] == '{' {
		var one InterviewRound
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*r = []InterviewRound{one}
		return nil
	}
	// 其它形态忽略，归一化为 nil。
	*r = nil
	return nil
}
