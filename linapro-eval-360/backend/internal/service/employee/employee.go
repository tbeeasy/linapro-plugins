// Package employee 实现 360 测评「员工端作答」服务（eval-employee）。
// 它消费 empcap 的 EmpContext（EmpID，经 EmpAuth 注入），直读 live 问卷结构，
// 按二维可见性逐关系过滤后返回题目；提交时按过滤后 live 结构校验必填、规范化一题一行存储。
//
// 设计要点（见 openspec/changes/linapro-eval-360/design.md 与「矩阵行=被评估人」改造计划）：
//   - 我的待办：按问卷聚合，一个评价者 + 一份已发布问卷 = 一条待办，展示完成度 X/Y 人。
//   - 取题：按问卷取批量题集，直读 live 结构（无快照）；可见性按「关系的角色 + 被评估人」逐行判定，
//     每题返回要渲染的被评估人行；对名下所有人都不可见的题与空维度直接不返回。
//   - 提交：批量提交页面上已展示的未提交被评估人，单事务全成全败；
//     请求内全部已提交 = 幂等，部分已提交 = 拒绝（两标签页交叉提交）。
//   - 落库仍按被评估人各存各的（per-relation submission）；矩阵 answer 恒 row_id=0、option_id=所选列；
//     选项级评语随答案保存；duration_seconds 整场计一次、每条 submission 存同一值。
package employee

import (
	"context"
	"time"

	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// Service 定义员工端作答能力。所有方法只作用于当前员工（EmpContext.EmpID）作为评价者的评价关系，
// 属「当前用户自隔离」数据边界；EmpContext 缺失时统一返回 empcap.CodeUnauthorized。
type Service interface {
	// ListTodo 返回当前员工的待办：按问卷聚合，一个评价者 + 一份已发布问卷 = 一条，含完成度 X/Y 人与派生状态。
	// 无任何关系时返回空切片（非 nil）；未发布问卷不出现。排序：待填写 → 已完成 → 已作废，同状态按问卷 id 倒序。
	ListTodo(ctx context.Context) ([]*TodoItem, error)
	// GetQuestionnaireSet 按问卷取批量题集：questionnaireID 为目标问卷；返回当前员工在该问卷下的全部被评估人行、
	// 逐行可见的题目（对名下所有人都不可见的题与空维度不返回）与已提交答案回显。
	// 问卷不存在或未发布返回 CodeQuestionnaireNotFound；当前员工在该问卷下无关系返回 CodeRelationNotFound。
	GetQuestionnaireSet(ctx context.Context, questionnaireID int64) (*QuestionnaireSet, error)
	// SubmitBatch 批量提交：in.Items 为页面上已展示的未提交被评估人；单事务全成全败，按被评估人各自落库，
	// 每条 submission 存同一 duration_seconds 与 submitted_at。请求内全部已提交按幂等返回（Idempotent=true）。
	// 可能返回：CodeQuestionnaireNotFound、CodeProjectClosed、CodeEmptySubmission、CodeRelationNotFound、
	// CodePartiallySubmitted、CodeInvalidAnswer / CodeMissingRequired（带被评估人姓名参数）。
	// 并发重复提交由 submission.relation_id 唯一索引兜底，冲突后按提交集合规则重新归类。
	SubmitBatch(ctx context.Context, in SubmitBatchInput) (*SubmitBatchResult, error)
}

// 编译期断言 serviceImpl 实现 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现，依赖问卷服务（取 live 结构）。
type serviceImpl struct {
	questionnaire qsvc.Service
}

// New 构造员工端服务。
func New(questionnaire qsvc.Service) Service {
	return &serviceImpl{questionnaire: questionnaire}
}

// ---------------------------------------------------------------------------
// 输入/输出 DTO
// ---------------------------------------------------------------------------

// DerivedStatus 是问卷级待办的派生状态（读时派生，不落库）。
type DerivedStatus string

const (
	// DerivedStatusPending 尚有被评估人未提交且项目未截止。
	DerivedStatusPending DerivedStatus = "pending"
	// DerivedStatusFilled 名下被评估人全部已提交。
	DerivedStatusFilled DerivedStatus = "filled"
	// DerivedStatusVoided 未填完且项目已截止。
	DerivedStatusVoided DerivedStatus = "voided"
)

// TodoItem 是待办列表项（一份问卷一条）。
type TodoItem struct {
	QuestionnaireID    int64            `json:"questionnaireId"`
	ProjectID          int64            `json:"projectId"`
	ProjectName        string           `json:"projectName"`
	QuestionnaireTitle string           `json:"questionnaireTitle"`
	StartAt            *time.Time       `json:"startAt"`
	EndAt              *time.Time       `json:"endAt"`
	Total              int              `json:"total"`          // 名下被评估人总数
	CompletedCount     int              `json:"completedCount"` // 已提交人数
	DerivedStatus      DerivedStatus    `json:"derivedStatus"`
	Evaluatees         []*TodoEvaluatee `json:"evaluatees"` // 名下被评估人明细（按关系 id 升序），供员工端按人展示
}

// TodoEvaluatee 是待办下的一个被评估人（对应一条评价关系）。
type TodoEvaluatee struct {
	RelationID    int64  `json:"relationId"`
	EvaluateeName string `json:"evaluateeName"`
	JobTitle      string `json:"jobTitle"`
	Department    string `json:"department"`
	RoleName      string `json:"roleName"`
	Submitted     bool   `json:"submitted"`
}

// QuestionnaireSet 是按问卷聚合的批量题集（供员工端一屏作答）。
type QuestionnaireSet struct {
	QuestionnaireID    int64              `json:"questionnaireId"`
	QuestionnaireTitle string             `json:"questionnaireTitle"`
	Evaluatees         []*EvaluateeRow    `json:"evaluatees"`
	Dimensions         []*SetDimension    `json:"dimensions"`
	SubmittedAnswers   []*SubmittedAnswer `json:"submittedAnswers"`
	Closed             bool               `json:"closed"` // 项目已截止
}

// EvaluateeRow 是当前评价者名下的一个被评估人行（对应一条评价关系）。
type EvaluateeRow struct {
	RelationID    int64  `json:"relationId"`
	EvaluateeName string `json:"evaluateeName"`
	RoleName      string `json:"roleName"`
	Submitted     bool   `json:"submitted"`
}

// SetDimension 是批量题集中的维度节点（只含至少一题可见的维度）。
type SetDimension struct {
	Id        int64          `json:"id"`
	Name      string         `json:"name"`
	Sort      int            `json:"sort"`
	Questions []*SetQuestion `json:"questions"`
}

// SetQuestion 是批量题集中的题目：live 题目节点 + 该题要渲染的被评估人行。
type SetQuestion struct {
	Node *qsvc.QuestionNode `json:"node"`
	// RowRelationIDs 为该题可见的关系 id（服务端已按关系的角色 + 被评估人算好），按被评估人行顺序排列。
	RowRelationIDs []int64 `json:"rowRelationIds"`
}

// SubmittedAnswer 是已提交关系的答案（只读回显用）。
type SubmittedAnswer struct {
	RelationID int64  `json:"relationId"`
	QuestionID int64  `json:"questionId"`
	OptionID   int64  `json:"optionId"`
	TextValue  string `json:"textValue"`
}

// SubmitBatchInput 是批量提交入参。
type SubmitBatchInput struct {
	QuestionnaireID int64
	DurationSeconds int               // 前端整场计时上传的整数秒，每条 submission 存同一值
	Items           []SubmitItemInput // 按被评估人（关系）分组的答案
}

// SubmitItemInput 是单个被评估人（关系）的答案集合。
type SubmitItemInput struct {
	RelationID int64
	Answers    []AnswerInput // 一题一条
}

// AnswerInput 是单条答案入参。矩阵已退化为每人选一列，不再有行 id。
type AnswerInput struct {
	QuestionID int64  // 所属题目
	OptionID   int64  // 单选选项 id / 矩阵列 id；文本为 0
	TextValue  string // 文本答案 或 选项级评语
}

// SubmitBatchResult 是批量提交结果（幂等时返回首次提交时间）。
type SubmitBatchResult struct {
	SubmittedCount int        `json:"submittedCount"` // 本次（或幂等命中的）提交人数
	SubmittedAt    *time.Time `json:"submittedAt"`
	Idempotent     bool       `json:"idempotent"` // true=重复提交返回首次结果
}
