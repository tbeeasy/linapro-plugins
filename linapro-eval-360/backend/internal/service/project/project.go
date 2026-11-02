// Package project 实现 360 测评「项目与发送编排」管理端服务（eval-project）。
// 它拥有 project 表的写入，并编排「一键发送」：发布未发布问卷 + 推送飞书卡片
// （推送由 notify 子域实现，本模块只做发送编排与截止锁定派生）。
// 本插件为平台级：问卷对所有租户员工通用，不做租户隔离。
//
// 设计要点（见 openspec/changes/linapro-eval-360/design.md）：
//   - 项目状态为派生值（草稿/进行中/已结束），无独立锁定态、无 cron。
//   - 截止锁定派生：now > end_at 或 closed_at != nil 拒绝提交；未提交关系读时派生已作废。
//   - 一键发送 / 单份发布：校验（整批判定）→ 推送按评价者一人一卡（无快照）→ 按问卷判定，
//     只有待评评价者全部送达的问卷才写发布状态（已发布跳过、幂等）。单份发布失败返回错误；
//     一键发送部分发布，未发布问卷随结果逐份回报。
package project

import (
	"context"
	"time"

	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// 派生项目状态。
const (
	StatusDraft  = "draft"  // 无已发布问卷
	StatusActive = "active" // 有已发布问卷且未结束
	StatusClosed = "closed" // now > end_at 或已提前结束
)

// Sender 抽象一键发送的推送编排（由 notify 子域实现；notify 单向依赖本包的契约类型，
// 本包不依赖 notify，避免循环依赖）。
type Sender interface {
	// Precheck 在发布前校验推送配置完整；不完整返回业务错误，一键发送据此拦截、不发布。
	Precheck(ctx context.Context) error
	// SendAll 向待发布问卷的评价者推送飞书卡片，并按问卷回报推送结果：PushOutcome 中未出现的
	// 问卷即其待评评价者全部送达（含无待评评价者的问卷），调用方只发布这些问卷。返回 error
	// 表示请求级失败（empcap 未绑定、卡片配置读取失败、lark-push 未就绪或配置非法），此时调用方
	// 不得发布任何问卷。
	SendAll(ctx context.Context, projectID int64, questionnaireIDs []int64) (PushOutcome, error)
}

// PushOutcome 是一次推送按问卷的失败结果，键为问卷 id，值为该问卷内失败评价者的回报标签
// （工号，无工号时回退 EmpID 字符串）。同一问卷只会出现在其中一个映射里；零值表示全部通过。
type PushOutcome struct {
	// Unreachable 是干跑未通过的问卷：其中有评价者在任何企业都不可投递，本问卷评价者未因它发卡。
	Unreachable map[int64][]string
	// Failed 是干跑通过、但正式投递后仍有评价者未送达的问卷。
	Failed map[int64][]string
}

// FailReason 是一键发送中问卷未发布的原因。
type FailReason string

// 问卷未发布原因取值（同时是 API 响应 failed[].reason 的取值）。
const (
	// FailReasonUnreachable 干跑发现有评价者在任何企业都无法接收飞书卡片。
	FailReasonUnreachable FailReason = "unreachable"
	// FailReasonPushFailed 正式投递后仍有评价者未送达。
	FailReasonPushFailed FailReason = "push_failed"
)

// Service 定义项目与发送编排的租户级管理端能力。
type Service interface {
	// List 返回当前租户下的全部项目（管理员可见本租户全部，不按创建人收窄）。
	List(ctx context.Context) ([]*ProjectListItem, error)
	// GetDetail 返回项目详情（基本信息 + 跨问卷总完成率 + 问卷卡片列表）。
	GetDetail(ctx context.Context, id int64) (*ProjectDetail, error)
	// Create 新建项目。
	Create(ctx context.Context, in CreateInput) (int64, error)
	// Update 就地修改项目元信息（名称/描述/起止时间）。
	Update(ctx context.Context, in UpdateInput) error
	// Delete 删除项目（存在已发布问卷或作答时拒绝，引导先处理）。
	Delete(ctx context.Context, id int64) error

	// Close 提前结束项目：置 closed_at 为当前时间，不改 end_at。
	Close(ctx context.Context, id int64) error
	// Reopen 重新开启已提前结束的项目：清空 closed_at（不改 end_at）。
	Reopen(ctx context.Context, id int64) error

	// SendAll 一键发送：整批校验项目下全部未发布问卷 → 向其评价者推送飞书卡片 → 按问卷判定，
	// 只把待评评价者全部送达的问卷一次性写为已发布（已发布跳过、不重推）。返回本次发布的问卷 id
	// 与未发布问卷清单（Failed，含原因与失败工号）；部分或全部问卷推送失败都正常返回、不返回 error，
	// 全部失败时 PublishedIDs 为空切片。项目不存在/含空问卷/权重不合规返回本模块或问卷模块业务错误，
	// 推送配置缺失或请求级推送错误返回 notify 业务错误，这些情况下不推送或不发布任何问卷（已送达的
	// 卡片不撤回）。sender=nil 时仅发布。
	SendAll(ctx context.Context, projectID int64) (*SendResult, error)
	// PublishQuestionnaire 发布单份问卷：预检推送配置 → 校验 → 推送 → 全部评价者送达后才写发布状态；
	// 已发布幂等返回空结果、不重推。有评价者不可达返回 notifycode.CodeEvaluatorsUnreachable，
	// 有评价者未送达返回 notifycode.CodePushFailed（参数 count / list），问卷保持未发布；
	// 其余错误语义同 SendAll。成功结果的 Failed 恒为空。
	PublishQuestionnaire(ctx context.Context, questionnaireID int64) (*SendResult, error)

	// PageQuestionnaires 分页返回项目下的问卷卡片（含被评估人数/完成率），支持按标题搜索。
	PageQuestionnaires(ctx context.Context, projectID int64, keyword string, pageNum, pageSize int) ([]*QuestionnaireCard, int, error)

	// IsClosed 读时派生项目截止状态（供员工端提交校验复用）。
	IsClosed(ctx context.Context, projectID int64) (bool, error)
}

// 编译期断言 serviceImpl 实现 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现，依赖问卷服务（发布未发布问卷）。
type serviceImpl struct {
	questionnaire qsvc.Service // 问卷服务（发布未发布问卷）
	sender        Sender       // 推送编排（notify 子域注入，可为 nil=不推送）
}

// New 构造项目服务。sender 可为 nil（无推送能力时仅完成发布、不推送）。
func New(questionnaire qsvc.Service, sender Sender) Service {
	return &serviceImpl{questionnaire: questionnaire, sender: sender}
}

// ---------------------------------------------------------------------------
// 输入/输出 DTO
// ---------------------------------------------------------------------------

// ProjectListItem 是项目列表项。
type ProjectListItem struct {
	Id          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"` // draft/active/closed（派生）
	StartAt     *time.Time `json:"startAt"`
	EndAt       *time.Time `json:"endAt"`
	ClosedAt    *time.Time `json:"closedAt"`
}

// ProjectDetail 是项目详情（基本信息 + 跨问卷总完成率 + 问卷卡片）。
type ProjectDetail struct {
	ProjectListItem
	Questionnaires []*QuestionnaireCard `json:"questionnaires"`
	TotalCompleted int                  `json:"totalCompleted"` // 跨问卷已填关系数
	TotalExpected  int                  `json:"totalExpected"`  // 跨问卷应填关系数
}

// QuestionnaireCard 是项目详情下的问卷卡片（含完成率）。
type QuestionnaireCard struct {
	Id             int64      `json:"id"`
	Title          string     `json:"title"`
	Published      int        `json:"published"`
	Sort           int        `json:"sort"`
	EvaluateeCount int        `json:"evaluateeCount"`
	Completed      int        `json:"completed"`
	Expected       int        `json:"expected"`
	CreatedAt      *time.Time `json:"createdAt"`
}

// CreateInput 是新建项目入参。
type CreateInput struct {
	Name        string
	Description string
	StartAt     *time.Time
	EndAt       *time.Time
	CreatedBy   int
}

// UpdateInput 是修改项目元信息入参（指针=可选）。
type UpdateInput struct {
	Id          int64
	Name        *string
	Description *string
	StartAt     *time.Time
	EndAt       *time.Time
}

// SendResult 是发送（发布）结果。
type SendResult struct {
	PublishedIDs []int64        `json:"publishedIds"` // 本次发布的问卷 id
	Failed       []*SendFailure `json:"failed"`       // 一键发送中推送失败而未发布的问卷（按待发布顺序）
}

// SendFailure 是一键发送中一份因推送失败而未发布的问卷。
type SendFailure struct {
	QuestionnaireID int64      `json:"questionnaireId"`
	Title           string     `json:"title"`
	Reason          FailReason `json:"reason"`
	EmployeeNos     []string   `json:"employeeNos"` // 该问卷内失败评价者的工号（无工号时为 EmpID）
}
