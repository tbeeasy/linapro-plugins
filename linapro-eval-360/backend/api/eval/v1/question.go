// question.go 定义 360 测评管理端「题目」API 的请求/响应 DTO。
// 题目连同选项、矩阵行列、可见性一体保存。
package v1

import "github.com/gogf/gf/v2/frame/g"

// QuestionOptionDTO 是单选选项入参（Id=0 表示新增）。
type QuestionOptionDTO struct {
	Id        int64  `json:"id" dc:"选项 id，0=新增"`
	Label     string `json:"label" dc:"选项文案"`
	Score     string `json:"score" dc:"分值（NUMERIC，字符串承载）"`
	AllowText int    `json:"allowText" dc:"是否允许附评语：0/1"`
	Sort      int    `json:"sort" dc:"排序"`
}

// MatrixColumnDTO 是矩阵列入参（Id=0 表示新增）。
// 评语配置随列一并提交：Enabled 为开关，Required 仅在开关开启时有意义（提交作答时校验），
// Placeholder 为评语输入框占位提示，Presets 为预设填空候选，AllowCustom 表示有预设时是否还允许自定义输入。
type MatrixColumnDTO struct {
	Id                 int64    `json:"id"                 dc:"列 id，0=新增" eg:"401"`
	Label              string   `json:"label"              dc:"列文案" eg:"优秀"`
	Score              string   `json:"score"              dc:"分值（NUMERIC，字符串承载）" eg:"5"`
	Sort               int      `json:"sort"               dc:"排序" eg:"1"`
	AllowComment       int      `json:"allowComment"       dc:"是否允许附评语：0=否 1=是" eg:"1"`
	CommentRequired    int      `json:"commentRequired"    dc:"评语是否必填：0=否 1=是（仅允许评语时生效，提交作答时校验）" eg:"0"`
	CommentPlaceholder string   `json:"commentPlaceholder" dc:"评语输入框占位提示（提示语），可空；与预设填空互不冲突" eg:"请举例说明"`
	CommentPresets     []string `json:"commentPresets"     dc:"预设填空候选文案；空=无预设" eg:"[\"沟通清晰\",\"逻辑严谨\"]"`
	CommentAllowCustom int      `json:"commentAllowCustom" dc:"有预设时是否允许自定义输入：0=只能选预设 1=可自定义" eg:"1"`
}

// QuestionBody 是题目公共字段（新增/编辑共用）。
// 必须导出：GoFrame 请求绑定基于反射，无法为未导出的匿名嵌入结构填充字段，
// 否则 Type/Title 等会停留在零值且其 v: 校验被跳过。
type QuestionBody struct {
	QuestionnaireId         int64               `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"所属问卷 id"`
	DimensionId             int64               `json:"dimensionId" v:"required|min:1#题目必须挂维度|维度 id 非法" dc:"所属维度 id"`
	Type                    string              `json:"type" v:"required|in:single_choice,text_short,text_long,matrix#请选择题型|题型非法" dc:"题型：single_choice|text_short|text_long|matrix"`
	Title                   string              `json:"title" v:"required#请填写题目标题" dc:"题目标题（富文本 HTML）"`
	Description             string              `json:"description" dc:"题目描述（富文本 HTML，可空）"`
	Required                int                 `json:"required" dc:"是否必填：0/1"`
	Sort                    int                 `json:"sort" dc:"排序"`
	Options                 []QuestionOptionDTO `json:"options" dc:"单选选项（单选题）"`
	MatrixColumns           []MatrixColumnDTO   `json:"matrixColumns" dc:"矩阵列（矩阵题的评价项；矩阵行由被评估人清单动态渲染，无需传入）"`
	VisibleRoleIds          []int64             `json:"visibleRoleIds" dc:"可见角色 id，空=全员可见"`
	ExcludedEvaluatorEmpIds []int64             `json:"excludedEvaluatorEmpIds" dc:"受限评价者 EmpID（排除语义）：名单内的评价者本人在该问卷中看不到该题，与其评谁无关；空=不限制" eg:"[1001,1002]"`
}

// CreateQuestionReq 新建题目。
type CreateQuestionReq struct {
	g.Meta `path:"/question" method:"post" tags:"Eval360 Question" summary:"新建题目" dc:"在维度下新增题目（连同选项/矩阵/可见性一体保存）。" permission:"eval:360:edit"`
	QuestionBody
}

// CreateQuestionRes 返回新题目 id。
type CreateQuestionRes struct {
	Id int64 `json:"id" dc:"新题目 id"`
}

// UpdateQuestionReq 就地编辑题目（存在作答时按白名单强校验）。
type UpdateQuestionReq struct {
	g.Meta `path:"/question/{id}" method:"put" tags:"Eval360 Question" summary:"编辑题目" dc:"就地编辑题目；存在作答时仅允许改文案/新增/调排序。" permission:"eval:360:edit"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少题目 id|题目 id 非法" dc:"题目 id"`
	QuestionBody
}

// UpdateQuestionRes 返回题目 id。
type UpdateQuestionRes struct {
	Id int64 `json:"id" dc:"题目 id"`
}

// DeleteQuestionReq 删除题目。
type DeleteQuestionReq struct {
	g.Meta `path:"/question/{id}" method:"delete" tags:"Eval360 Question" summary:"删除题目" dc:"删除题目及其从属结构（存在作答时拒绝）。" permission:"eval:360:edit"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少题目 id|题目 id 非法" dc:"题目 id"`
}

// DeleteQuestionRes 是空响应。
type DeleteQuestionRes struct{}

// ReorderQuestionsReq 重排某维度下题目的顺序。
// QuestionIds 为该维度全部题目按目标顺序排列的完整 id 列表（不重不漏），落库为 0 起连续的 sort。
type ReorderQuestionsReq struct {
	g.Meta          `path:"/question/reorder" method:"put" tags:"Eval360 Question" summary:"题目重排" dc:"按给定顺序重写维度下全部题目的 sort，用于在题目之间插入新题后的整体顺延。" permission:"eval:360:edit"`
	QuestionnaireId int64   `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"所属问卷 id"`
	DimensionId     int64   `json:"dimensionId" v:"required|min:1#缺少维度 id|维度 id 非法" dc:"所属维度 id"`
	QuestionIds     []int64 `json:"questionIds" v:"required#缺少题目顺序|题目顺序不能为空" dc:"该维度全部题目按目标顺序排列的 id 列表"`
}

// ReorderQuestionsRes 是空响应。
type ReorderQuestionsRes struct{}
