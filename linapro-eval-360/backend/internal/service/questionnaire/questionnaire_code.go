// 本文件定义 linapro-eval-360 问卷模块的业务错误码。
package questionnaire

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeQuestionnaireNotFound 问卷不存在或不在当前租户可见范围。
	CodeQuestionnaireNotFound = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_NOT_FOUND", "问卷不存在", gcode.CodeNotFound,
	)
	// CodeDimensionNotFound 维度不存在。
	CodeDimensionNotFound = bizerr.MustDefine(
		"EVAL360_DIMENSION_NOT_FOUND", "维度不存在", gcode.CodeNotFound,
	)
	// CodeQuestionNotFound 题目不存在。
	CodeQuestionNotFound = bizerr.MustDefine(
		"EVAL360_QUESTION_NOT_FOUND", "题目不存在", gcode.CodeNotFound,
	)
	// CodeQuestionOrderMismatch 重排序入参的题目集合与维度下 actual 集合不一致（缺漏/多余/重复/跨维度）。
	CodeQuestionOrderMismatch = bizerr.MustDefine(
		"EVAL360_QUESTION_ORDER_MISMATCH", "题目排序参数与当前维度下的题目不一致，请刷新后重试", gcode.CodeInvalidParameter,
	)
	// CodeInvalidQuestionType 题型非法（仅单选/单行文本/多行文本/矩阵）。
	CodeInvalidQuestionType = bizerr.MustDefine(
		"EVAL360_INVALID_QUESTION_TYPE", "题型非法：仅支持单选/单行文本/多行文本/矩阵", gcode.CodeInvalidParameter,
	)
	// CodeQuestionDimensionRequired 题目必须挂到一个维度。
	CodeQuestionDimensionRequired = bizerr.MustDefine(
		"EVAL360_QUESTION_DIMENSION_REQUIRED", "题目必须归属一个维度", gcode.CodeInvalidParameter,
	)
	// CodeInvalidScore 选项/列分值非法（无法解析为数值）。
	CodeInvalidScore = bizerr.MustDefine(
		"EVAL360_INVALID_SCORE", "分值非法", gcode.CodeInvalidParameter,
	)
	// CodeSingleChoiceOptionsRequired 单选题至少需要一个选项。
	CodeSingleChoiceOptionsRequired = bizerr.MustDefine(
		"EVAL360_SINGLE_CHOICE_OPTIONS_REQUIRED", "单选题至少需要一个选项", gcode.CodeInvalidParameter,
	)
	// CodeMatrixColumnsRequired 矩阵题至少需要一列（行改由被评估人清单动态渲染，不再手填）。
	CodeMatrixColumnsRequired = bizerr.MustDefine(
		"EVAL360_MATRIX_COLUMNS_REQUIRED", "矩阵题至少需要一列评价项", gcode.CodeInvalidParameter,
	)
	// CodeDimensionNotEmpty 维度下仍有题目，不能删除。
	CodeDimensionNotEmpty = bizerr.MustDefine(
		"EVAL360_DIMENSION_NOT_EMPTY", "维度下仍有题目，请先移除题目再删除维度", gcode.CodeInvalidParameter,
	)
	// CodeEditRequiresStrategy 存在历史作答，白名单外的破坏性编辑被拒绝，需改用删除答案/复制问卷。
	CodeEditRequiresStrategy = bizerr.MustDefine(
		"EVAL360_EDIT_REQUIRES_STRATEGY", "问卷已有作答，该操作会破坏历史答案，请改用「删除历史答案」或「复制问卷」", gcode.CodeInvalidParameter,
	)
	// CodeWeightMissing 存在权重未填（<=0）的角色，拒绝发布。
	CodeWeightMissing = bizerr.MustDefine(
		"EVAL360_WEIGHT_MISSING", "存在权重为 0 的角色，请先为每个角色填写大于 0 的权重", gcode.CodeInvalidParameter,
	)
	// CodeWeightSumInvalid 各角色权重之和不为 100%，拒绝发布。
	CodeWeightSumInvalid = bizerr.MustDefine(
		"EVAL360_WEIGHT_SUM_INVALID", "各角色权重之和必须等于 100%", gcode.CodeInvalidParameter,
	)
	// CodeNoRoles 问卷尚无任何角色（角色由导入关系生成），无法发布。
	CodeNoRoles = bizerr.MustDefine(
		"EVAL360_NO_ROLES", "问卷尚无角色，请先导入评价关系以生成角色并配置权重", gcode.CodeInvalidParameter,
	)
	// CodeQuestionnaireNoQuestions 问卷无任何题目，无法发布。
	CodeQuestionnaireNoQuestions = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_NO_QUESTIONS", "问卷尚未配置题目，无法发布", gcode.CodeInvalidParameter,
	)
	// CodeQuestionnaireTitleExists 同一项目下已存在同名问卷（归一化后精确匹配）。
	CodeQuestionnaireTitleExists = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_TITLE_EXISTS", "同一项目下已存在同名问卷，请换一个名称", gcode.CodeInvalidParameter,
	)
	// CodeQuestionnaireTitleEmpty 问卷标题归一化后为空（纯空白视为未填）。
	CodeQuestionnaireTitleEmpty = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_TITLE_EMPTY", "问卷标题不能为空", gcode.CodeInvalidParameter,
	)
)
