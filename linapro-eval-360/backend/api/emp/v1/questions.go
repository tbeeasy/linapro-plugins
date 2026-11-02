// questions.go 定义员工端「按问卷取批量题集」接口 DTO：被评估人行 + 逐行可见题目 + 已提交答案回显。

package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// QuestionsReq 按问卷取批量题集。
type QuestionsReq struct {
	g.Meta          `path:"/questions" method:"get" tags:"Eval360 Emp" summary:"按问卷取批量题集" dc:"返回当前员工在该问卷下的全部被评估人行、逐行可见的题目及已提交答案；行数受当前员工在该问卷下的评价关系数约束。"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id" eg:"10"`
}

// QuestionsRes 是批量题集响应。
type QuestionsRes struct {
	Set *QuestionnaireSet `json:"set" dc:"批量题集" eg:"{...}"`
}

// QuestionnaireSet 是按问卷聚合的批量题集。
type QuestionnaireSet struct {
	QuestionnaireID    int64             `json:"questionnaireId" dc:"问卷 id" eg:"10"`
	QuestionnaireTitle string            `json:"questionnaireTitle" dc:"问卷标题" eg:"领导力评估"`
	Evaluatees         []EvaluateeRow    `json:"evaluatees" dc:"当前评价者在该问卷下的全部被评估人行（按关系 id 升序）" eg:"[]"`
	Dimensions         []DimensionNode   `json:"dimensions" dc:"至少一题可见的维度节点列表" eg:"[]"`
	SubmittedAnswers   []SubmittedAnswer `json:"submittedAnswers" dc:"已提交关系的答案（只读回显）" eg:"[]"`
	Closed             bool              `json:"closed" dc:"项目是否已截止" eg:"false"`
}

// EvaluateeRow 是一个被评估人行（对应一条评价关系）。
type EvaluateeRow struct {
	RelationID    int64  `json:"relationId" dc:"评价关系 id" eg:"21"`
	EvaluateeName string `json:"evaluateeName" dc:"被评估人姓名" eg:"张三"`
	RoleName      string `json:"roleName" dc:"当前评价者在该关系中的角色名" eg:"上级"`
	Submitted     bool   `json:"submitted" dc:"该关系是否已提交" eg:"false"`
}

// DimensionNode 是维度节点。
type DimensionNode struct {
	Id        int64          `json:"id" dc:"维度 id" eg:"1"`
	Name      string         `json:"name" dc:"维度名" eg:"领导力"`
	Sort      int            `json:"sort" dc:"排序" eg:"1"`
	Questions []QuestionNode `json:"questions" dc:"至少对一个被评估人可见的题目节点列表" eg:"[]"`
}

// QuestionNode 是题目节点。
type QuestionNode struct {
	Id             int64              `json:"id" dc:"题目 id" eg:"101"`
	DimensionId    int64              `json:"dimensionId" dc:"所属维度 id" eg:"1"`
	Type           string             `json:"type" dc:"题型：single_choice|text_short|text_long|matrix" eg:"single_choice"`
	Title          string             `json:"title" dc:"题目标题（富文本 HTML）" eg:"能清晰传达目标"`
	Description    string             `json:"description" dc:"题目描述（富文本 HTML，可空）" eg:""`
	Required       int                `json:"required" dc:"是否必填：0|1（必填=每个可见被评估人行都须作答）" eg:"1"`
	Sort           int                `json:"sort" dc:"排序" eg:"1"`
	Options        []OptionNode       `json:"options" dc:"单选选项（单选题）" eg:"[]"`
	MatrixColumns  []MatrixColumnNode `json:"matrixColumns" dc:"矩阵列（矩阵题的评价项）" eg:"[]"`
	RowRelationIds []int64            `json:"rowRelationIds" dc:"该题要渲染的被评估人行（评价关系 id，服务端已按关系的角色 + 评价者本人（受限评价者名单）判定可见性）" eg:"[21,22]"`
}

// OptionNode 是单选选项节点。
type OptionNode struct {
	Id        int64  `json:"id" dc:"选项 id" eg:"201"`
	Label     string `json:"label" dc:"选项文案" eg:"优秀"`
	Score     string `json:"score" dc:"分值（NUMERIC，字符串承载）" eg:"4"`
	AllowText int    `json:"allowText" dc:"是否允许附评语：0|1" eg:"1"`
	Sort      int    `json:"sort" dc:"排序" eg:"1"`
}

// MatrixColumnNode 是矩阵列节点。
// 评语配置随节点下发：AllowComment 为开关，CommentRequired 决定提交时是否必须有评语，
// CommentPlaceholder 为输入框占位提示，CommentPresets 为预设候选（空=自由输入），
// CommentAllowCustom 表示有预设时是否还允许自定义输入。
type MatrixColumnNode struct {
	Id                 int64    `json:"id"                 dc:"列 id" eg:"401"`
	Label              string   `json:"label"              dc:"列文案" eg:"优秀"`
	Score              string   `json:"score"              dc:"分值（NUMERIC，字符串承载）" eg:"4"`
	Sort               int      `json:"sort"               dc:"排序" eg:"1"`
	AllowComment       int      `json:"allowComment"       dc:"是否允许附评语：0|1" eg:"1"`
	CommentRequired    int      `json:"commentRequired"    dc:"评语是否必填：0|1（仅允许评语时生效，提交时校验）" eg:"0"`
	CommentPlaceholder string   `json:"commentPlaceholder" dc:"评语输入框占位提示，可空" eg:"请举例说明"`
	CommentPresets     []string `json:"commentPresets"     dc:"预设填空候选；空=无预设（自由输入）" eg:"[\"沟通清晰\"]"`
	CommentAllowCustom int      `json:"commentAllowCustom" dc:"有预设时是否允许自定义输入：0|1" eg:"1"`
}

// SubmittedAnswer 是已提交关系的一条答案。
type SubmittedAnswer struct {
	RelationID int64  `json:"relationId" dc:"评价关系 id" eg:"21"`
	QuestionID int64  `json:"questionId" dc:"题目 id" eg:"101"`
	OptionID   int64  `json:"optionId" dc:"单选选项 id / 矩阵列 id / 文本为 0" eg:"201"`
	TextValue  string `json:"textValue" dc:"文本答案 或 选项级评语" eg:""`
}
