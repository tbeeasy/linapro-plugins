// employee_code.go 定义 linapro-eval-360 员工端作答模块（eval-employee）的业务错误码。

package employee

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeQuestionnaireNotFound 问卷不存在或未发布（员工端不可见）。
	CodeQuestionnaireNotFound = bizerr.MustDefine(
		"EVAL360_EMP_QUESTIONNAIRE_NOT_FOUND", "问卷不存在或未发布", gcode.CodeNotFound,
	)
	// CodeRelationNotFound 评价关系不存在或不属于当前员工（或不属于该问卷）。
	CodeRelationNotFound = bizerr.MustDefine(
		"EVAL360_EMP_RELATION_NOT_FOUND", "评价关系不存在或不属于当前员工", gcode.CodeNotFound,
	)
	// CodeProjectClosed 项目已截止，拒绝提交。
	CodeProjectClosed = bizerr.MustDefine(
		"EVAL360_EMP_PROJECT_CLOSED", "评估已截止，不可提交", gcode.CodeInvalidParameter,
	)
	// CodeMissingRequired 提交时某被评估人存在未作答的必填题；参数 evaluatee=被评估人姓名。
	CodeMissingRequired = bizerr.MustDefine(
		"EVAL360_EMP_MISSING_REQUIRED", "「{evaluatee}」存在未作答的必填题", gcode.CodeInvalidParameter,
	)
	// CodeInvalidAnswer 答案非法（选项不属于该题、答案指向不可见题、重复作答等）；参数 evaluatee=被评估人姓名。
	CodeInvalidAnswer = bizerr.MustDefine(
		"EVAL360_EMP_INVALID_ANSWER", "「{evaluatee}」的答案非法", gcode.CodeInvalidParameter,
	)
	// CodeEmptySubmission 批量提交未包含任何被评估人，或同一被评估人重复出现。
	CodeEmptySubmission = bizerr.MustDefine(
		"EVAL360_EMP_EMPTY_SUBMISSION", "提交内容为空或包含重复的被评估人", gcode.CodeInvalidParameter,
	)
	// CodePartiallySubmitted 本次提交的被评估人中部分已在别处提交（如两个标签页交叉提交），整体拒绝。
	CodePartiallySubmitted = bizerr.MustDefine(
		"EVAL360_EMP_PARTIALLY_SUBMITTED", "部分被评估人已提交，请刷新页面后重试", gcode.CodeInvalidParameter,
	)
	// CodeEmpcapUnbound empcap 员工查询服务未绑定。
	CodeEmpcapUnbound = bizerr.MustDefine(
		"EVAL360_EMP_EMPCAP_UNBOUND", "员工能力服务未就绪", gcode.CodeNotImplemented,
	)
	// CodeMatrixCommentRequired 某评价项开启了「允许评语 + 必填」，但该行未填写评语；
	// 参数 evaluatee=被评估人姓名，title=题目。
	CodeMatrixCommentRequired = bizerr.MustDefine(
		"EVAL360_EMP_MATRIX_COMMENT_REQUIRED", "「{evaluatee}」的题目「{title}」需要填写评语", gcode.CodeInvalidParameter,
	)
)
