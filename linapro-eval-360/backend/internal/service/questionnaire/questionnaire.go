// Package questionnaire 实现 360 测评「问卷 / 维度 / 题目 / 选项」管理端服务。
// 它拥有 questionnaire / dimension / question / question_option / matrix_row /
// matrix_column / question_visibility / question_excluded_evaluator 等表的写入。
// 本插件为平台级：问卷对所有租户员工通用，不做租户隔离。
//
// 设计要点（见 openspec/changes/linapro-eval-360/design.md）：
//   - 无快照：取题/计分/导出一律直读 live 结构，本包只维护 live 结构本身。
//   - 编辑三策略：①保留答案（白名单强校验）②删除答案 ③复制问卷。
//   - 发布前权重校验：每角色权重 > 0 且各角色之和 = 100%。
package questionnaire

import (
	"context"
	"time"

	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// 题型常量：仅单选 / 单行文本 / 多行文本 / 矩阵（不含多选、评分）。
const (
	QuestionTypeSingleChoice = "single_choice"
	QuestionTypeTextShort    = "text_short"
	QuestionTypeTextLong     = "text_long"
	QuestionTypeMatrix       = "matrix"
)

// 问卷发布状态（跨模块共享：employee/relation 等消费方引用，避免散落字面量）。
const (
	PublishedNo  = 0 // 未发布
	PublishedYes = 1 // 已发布
)

// 以下为本包内部沿用的小写别名，向后兼容本包历史引用。
const (
	publishedNo  = PublishedNo
	publishedYes = PublishedYes
)

// 以下为本包对生成实体的稳定别名，供 controller 层引用同名类型。
type (
	// QuestionnaireEntity 映射 plugin_linapro_eval_360_questionnaire。
	QuestionnaireEntity = entitymodel.Questionnaire
)

// Service 定义问卷/维度/题目/选项的租户级管理端能力。
type Service interface {
	// ListByProject 返回某项目下的问卷列表（不含结构树）。
	ListByProject(ctx context.Context, projectID int64) ([]*QuestionnaireListItem, error)
	// PageByProject 分页返回某项目下的问卷列表（可按标题模糊搜索），返回当页数据与总数。
	PageByProject(ctx context.Context, projectID int64, keyword string, pageNum, pageSize int) ([]*QuestionnaireListItem, int, error)
	// GetDetail 返回某问卷的完整 live 结构树（维度→题目→选项/矩阵/可见性 + 角色）。
	GetDetail(ctx context.Context, id int64) (*QuestionnaireDetail, error)
	// CreateQuestionnaire 新建问卷（默认未发布）。
	CreateQuestionnaire(ctx context.Context, in CreateQuestionnaireInput) (int64, error)
	// UpdateQuestionnaire 就地修改问卷元信息（草稿/收集期间均可）。
	UpdateQuestionnaire(ctx context.Context, in UpdateQuestionnaireInput) error
	// DeleteQuestionnaire 删除问卷及其结构（存在作答时拒绝，引导改用删除答案/复制）。
	DeleteQuestionnaire(ctx context.Context, id int64) error

	// CreateDimension 在问卷下新增维度。
	CreateDimension(ctx context.Context, in CreateDimensionInput) (int64, error)
	// UpdateDimension 修改维度名称/排序。
	UpdateDimension(ctx context.Context, in UpdateDimensionInput) error
	// DeleteDimension 删除空维度（含题目时拒绝）。
	DeleteDimension(ctx context.Context, id int64) error

	// SaveQuestion 新增或就地编辑题目（连同选项/矩阵行列/可见性一体保存）。
	// 存在作答时按策略①白名单强校验。
	SaveQuestion(ctx context.Context, in SaveQuestionInput) (int64, error)
	// ReorderQuestions 按给定顺序重写某维度下全部题目的 sort（0 起连续）。
	// questionIds 必须恰好覆盖该维度当前全部题目（不重不漏），保证原子顺延。
	ReorderQuestions(ctx context.Context, in ReorderQuestionsInput) error
	// DeleteQuestion 删除题目及其从属结构（存在作答时拒绝）。
	DeleteQuestion(ctx context.Context, id int64) error

	// ValidatePublish 校验单份问卷能否发布（题目非空 + 角色权重合规），不写发布状态、不推送。
	// 返回所属项目 id 与是否需要发布（已发布幂等返回 needPublish=false、不再校验）；
	// 问卷不存在、题目为空或权重不合规返回对应业务错误，此时 projectID 为 0。
	ValidatePublish(ctx context.Context, id int64) (projectID int64, needPublish bool, err error)
	// Unpublish 取消发布单份问卷。
	Unpublish(ctx context.Context, id int64) error
	// ValidateProjectPublish 校验项目下全部未发布问卷能否发布，返回待发布问卷 id（已发布跳过、
	// 无待发布时为空切片）；任一问卷不合规返回其业务错误。不写发布状态、不推送。
	ValidateProjectPublish(ctx context.Context, projectID int64) ([]int64, error)
	// MarkPublished 一次性把给定问卷置为已发布（单条 UPDATE，原子）；已发布的行不受影响，
	// ids 为空时无操作。调用方须在推送全员送达后才调用。
	MarkPublished(ctx context.Context, ids []int64) error

	// ClearAnswers 策略②：清空问卷全部作答并重置相关评价关系完成态。
	ClearAnswers(ctx context.Context, id int64) error
	// CopyQuestionnaire 策略③：深拷贝结构 + 被评估人→评价关系树为未发布新问卷（不复制答案）。
	// title 为新问卷标题（复制件不得与同项目内其他问卷重名，故由调用方指定）。
	CopyQuestionnaire(ctx context.Context, id int64, title string) (int64, error)
}

// 编译期断言 serviceImpl 实现 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现。
type serviceImpl struct{}

// New 构造问卷服务。
func New() Service {
	return &serviceImpl{}
}

// ---------------------------------------------------------------------------
// 输入/输出 DTO
// ---------------------------------------------------------------------------

// QuestionnaireListItem 是问卷列表项（不含结构树）。
type QuestionnaireListItem struct {
	Id          int64      `json:"id"`
	ProjectId   int64      `json:"projectId"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Published   int        `json:"published"`
	Sort        int        `json:"sort"`
	CreatedAt   *time.Time `json:"createdAt"`
}

// QuestionnaireDetail 是问卷完整 live 结构树。
type QuestionnaireDetail struct {
	Id          int64  `json:"id"`
	ProjectId   int64  `json:"projectId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Published   int    `json:"published"`
	// HasAnswers 标记问卷是否已有任何提交作答。前端据 published||hasAnswers 判断编辑是否受限
	// （草稿且无作答可自由编辑，否则须走编辑三策略）；与后端 editRestricted 闸门语义一致。
	HasAnswers bool             `json:"hasAnswers"`
	Sort       int              `json:"sort"`
	Dimensions []*DimensionNode `json:"dimensions"`
	Roles      []*RoleNode      `json:"roles"`
}

// DimensionNode 是结构树中的维度节点。
type DimensionNode struct {
	Id        int64           `json:"id"`
	Name      string          `json:"name"`
	Sort      int             `json:"sort"`
	Questions []*QuestionNode `json:"questions"`
}

// QuestionNode 是结构树中的题目节点（含选项/矩阵/可见性）。
// ExcludedEvaluatorEmpIds 为受限评价者 EmpID 名单（排除语义，按评价者本人全局生效）。
type QuestionNode struct {
	Id                      int64               `json:"id"`
	DimensionId             int64               `json:"dimensionId"`
	Type                    string              `json:"type"`
	Title                   string              `json:"title"`
	Description             string              `json:"description"`
	Required                int                 `json:"required"`
	Sort                    int                 `json:"sort"`
	Options                 []*OptionNode       `json:"options"`
	MatrixColumns           []*MatrixColumnNode `json:"matrixColumns"`
	VisibleRoleIds          []int64             `json:"visibleRoleIds"`
	ExcludedEvaluatorEmpIds []int64             `json:"excludedEvaluatorEmpIds"`
}

// OptionNode 是单选选项节点；Score 以字符串承载 NUMERIC，避免 float。
type OptionNode struct {
	Id        int64  `json:"id"`
	Label     string `json:"label"`
	Score     string `json:"score"`
	AllowText int    `json:"allowText"`
	Sort      int    `json:"sort"`
}

// MatrixColumnNode 是矩阵列选项节点。
// 评语配置（AllowComment/CommentRequired/CommentPlaceholder/CommentPresets/CommentAllowCustom）
// 随节点一并下发给管理端编辑器与员工端作答页；Presets 为候选文案切片，空表示无预设。
type MatrixColumnNode struct {
	Id                 int64    `json:"id"`
	Label              string   `json:"label"`
	Score              string   `json:"score"`
	Sort               int      `json:"sort"`
	AllowComment       int      `json:"allowComment"`
	CommentRequired    int      `json:"commentRequired"`
	CommentPlaceholder string   `json:"commentPlaceholder"`
	CommentPresets     []string `json:"commentPresets"`
	CommentAllowCustom int      `json:"commentAllowCustom"`
}

// RoleNode 是问卷角色节点（由导入生成，Weight 以字符串承载）。
type RoleNode struct {
	Id     int64  `json:"id"`
	Name   string `json:"name"`
	Weight string `json:"weight"`
}

// CreateQuestionnaireInput 是新建问卷入参。
type CreateQuestionnaireInput struct {
	ProjectID   int64
	Title       string
	Description string
	Sort        int
}

// UpdateQuestionnaireInput 是就地修改问卷元信息入参（指针=可选）。
type UpdateQuestionnaireInput struct {
	Id          int64
	Title       *string
	Description *string
	Sort        *int
}

// CreateDimensionInput 是新建维度入参。
type CreateDimensionInput struct {
	QuestionnaireID int64
	Name            string
	Sort            int
}

// UpdateDimensionInput 是修改维度入参。
type UpdateDimensionInput struct {
	Id   int64
	Name *string
	Sort *int
}

// SaveQuestionInput 是新增/就地编辑题目入参（Id=0 表示新增）。
// ExcludedEvaluatorEmpIDs 为受限评价者 EmpID 名单，保存时整体替换。
type SaveQuestionInput struct {
	Id                      int64
	QuestionnaireID         int64
	DimensionID             int64
	Type                    string
	Title                   string
	Description             string
	Required                int
	Sort                    int
	Options                 []OptionInput
	MatrixColumns           []MatrixColumnInput
	VisibleRoleIDs          []int64
	ExcludedEvaluatorEmpIDs []int64
}

// OptionInput 是单选选项入参（Id=0 表示新增）。
type OptionInput struct {
	Id        int64
	Label     string
	Score     string
	AllowText int
	Sort      int
}

// MatrixColumnInput 是矩阵列入参（Id=0 表示新增）。
// 评语配置随列一并保存：CommentPresets 为候选文案切片，落库时序列化为 JSON 数组文本。
type MatrixColumnInput struct {
	Id                 int64
	Label              string
	Score              string
	Sort               int
	AllowComment       int
	CommentRequired    int
	CommentPlaceholder string
	CommentPresets     []string
	CommentAllowCustom int
}

// ReorderQuestionsInput 是题目重排序入参。
// QuestionIDs 为某维度下题目按目标顺序排列的完整 id 列表（0 起连续落 sort）。
type ReorderQuestionsInput struct {
	QuestionnaireID int64
	DimensionID     int64
	QuestionIDs     []int64
}
