// 本文件定义 linapro-eval-360 项目与发送编排模块（eval-project）的业务错误码。
package project

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeProjectNotFound 项目不存在或不在当前租户可见范围。
	CodeProjectNotFound = bizerr.MustDefine(
		"EVAL360_PROJECT_NOT_FOUND", "项目不存在", gcode.CodeNotFound,
	)
	// CodeProjectClosed 项目已截止（now > end_at 或已提前结束），拒绝提交。
	CodeProjectClosed = bizerr.MustDefine(
		"EVAL360_PROJECT_CLOSED", "评估已截止，不可提交", gcode.CodeInvalidParameter,
	)
	// CodeProjectHasEmptyQuestionnaire 发送前发现项目内存在无题目的问卷。
	CodeProjectHasEmptyQuestionnaire = bizerr.MustDefine(
		"EVAL360_PROJECT_HAS_EMPTY_QUESTIONNAIRE", "项目内存在未配置题目的问卷，无法发送", gcode.CodeInvalidParameter,
	)
	// CodeProjectHasPublishedQuestionnaire 删除项目时存在已发布问卷，需先取消发布。
	CodeProjectHasPublishedQuestionnaire = bizerr.MustDefine(
		"EVAL360_PROJECT_HAS_PUBLISHED_QUESTIONNAIRE", "项目下存在已发布问卷，删除前请先取消发布", gcode.CodeInvalidParameter,
	)
	// CodeProjectHasSubmissions 删除项目时存在已提交作答，需先清空作答。
	CodeProjectHasSubmissions = bizerr.MustDefine(
		"EVAL360_PROJECT_HAS_SUBMISSIONS", "项目下存在已提交作答，删除前请先清空作答", gcode.CodeInvalidParameter,
	)
	// CodeProjectNameExists 项目名称已被占用（全平台唯一，归一化后精确匹配）。
	CodeProjectNameExists = bizerr.MustDefine(
		"EVAL360_PROJECT_NAME_EXISTS", "项目名称已存在，请换一个名称", gcode.CodeInvalidParameter,
	)
	// CodeProjectNameEmpty 项目名称归一化后为空（纯空白视为未填）。
	CodeProjectNameEmpty = bizerr.MustDefine(
		"EVAL360_PROJECT_NAME_EMPTY", "项目名称不能为空", gcode.CodeInvalidParameter,
	)
	// CodeQuestionnaireNotFound 问卷不存在（本模块自用，避免跨模块 import 错误码）。
	CodeQuestionnaireNotFound = bizerr.MustDefine(
		"EVAL360_PROJECT_QUESTIONNAIRE_NOT_FOUND", "问卷不存在", gcode.CodeNotFound,
	)
	// CodeRelationNotFound 评价关系不存在。
	CodeRelationNotFound = bizerr.MustDefine(
		"EVAL360_PROJECT_RELATION_NOT_FOUND", "评价关系不存在", gcode.CodeNotFound,
	)
)
