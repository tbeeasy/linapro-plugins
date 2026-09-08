package moka

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
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
	ApplicationID  int64            `json:"applicationId"`
	CandidateID    int64            `json:"candidateId"`
	Name           string           `json:"name"`
	Stage          ApplicationStage `json:"stage"`
	Archived       bool             `json:"archived"`
	ArchiveReasons archiveName      `json:"archiveReasons"`
}

// ArchiveReasonName 返回归档原因文本，无归档原因时返回空串。
func (b ApplicationBasicInfo) ArchiveReasonName() string {
	return string(b.ArchiveReasons)
}

// InterviewRound 是 ehrApplications 返回体内 interviewInfo 的单个面试轮次条目。
// StartTime 用 flexNumberString 容错解析（Moka 可能返回毫秒时间戳数字或 ISO 字符串）。
// IntervieweeVideoURL 在 interviewInfo 中可能为空，需求2 视频面试场景会用
// interview-information 接口的返回值覆盖。
// Interviewer 取 interviewInfo 条目的 name 字段，即该轮次的面试官姓名（同 pushCandidate
// 事件 interviewers[].name 语义）；同一轮次多名面试官时每名面试官各占一条。
type InterviewRound struct {
	Round               int              `json:"round"`
	RoundName           string           `json:"roundName"`
	InterviewType       InterviewType    `json:"interviewType"`
	Status              InterviewStatus  `json:"status"`
	StartTime           flexNumberString `json:"startTime"`
	IntervieweeVideoURL string           `json:"intervieweeVideoUrl"`
	Interviewer         string           `json:"name"`
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
}

// EhrApplicationsQuery 是 ehrApplications 的查询过滤条件。Archived 为 nil 时
// 不传 archived 参数；时间范围为空时不传，传入时须为北京时间
// `YYYY-MM-DDTHH:mm:ss.sssZ` 格式，由 Moka 服务端按申请更新时间过滤。
type EhrApplicationsQuery struct {
	StageIDs          []int64 // 招聘阶段 ID 列表，必填
	Archived          *bool   // 归档过滤：nil=不传，true=仅已归档，false=仅未归档
	UpdateAtStartTime string  // 更新时间起（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
	UpdateAtEndTime   string  // 更新时间止（北京时间 YYYY-MM-DDTHH:mm:ss.sssZ），空=不传
}

// EhrApplications 按 query 拉取候选人。支持按阶段 ID、归档状态和更新时间范围过滤，
// 过滤均由 Moka 服务端完成。阶段无候选人时返回空切片（非 error）。
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

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, gerror.Wrap(err, "moka-recruit: marshal ehrApplications body")
	}

	// 打印请求参数，便于排查日期格式等服务端解析问题。
	glog.Debugf(ctx, "moka-recruit: ehrApplications 请求参数 %s", string(body))

	raw, err := c.PostJSON(ctx, ehrApplicationsPath, body)
	if err != nil {
		return nil, err
	}

	// 打印原始返回值
	// glog.Debugf(ctx, "moka-recruit: ehrApplications 原始请求返回值 %s", string(raw))

	var env ehrApplicationsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: decode ehrApplications response: %s", truncate(string(raw), 512))
	}
	if env.Code != 200 {
		return nil, gerror.Newf("moka-recruit: ehrApplications returned code=%d msg=%q", env.Code, env.Msg)
	}
	// 打印返回值
	glog.Debugf(ctx, "moka-recruit: ehrApplications 请求返回值 %+v", env.Data)

	return env.Data, nil
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
		return nil, gerror.Wrap(err, "moka-recruit: marshal interview-information body")
	}

	glog.Debugf(ctx, "moka-recruit: GetInterviewInformation 请求参数 %s", string(body))

	raw, err := c.PostJSON(ctx, interviewInformationPath, body)
	if err != nil {
		return nil, err
	}

	// 打印原始返回值
	// glog.Debugf(ctx, "moka-recruit: GetInterviewInformation 原始请求返回值 %s", string(raw))

	var env interviewInformationEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, gerror.Wrapf(err, "moka-recruit: decode interview-information response: %s", truncate(string(raw), 512))
	}
	if env.Code != 0 {
		return nil, gerror.Newf("moka-recruit: interview-information returned code=%d msg=%q", env.Code, env.Msg)
	}

	// 打印返回值
	glog.Debugf(ctx, "moka-recruit: GetInterviewInformation 格式化之后请求返回值 %+v", env.Data)

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
