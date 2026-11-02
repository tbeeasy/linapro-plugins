// 本文件定义 linapro-eval-360 评价关系与角色模块（eval-relation）的业务错误码。
package relation

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeQuestionnaireNotFound 问卷不存在或不在当前租户可见范围。
	CodeQuestionnaireNotFound = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_NOT_FOUND", "问卷不存在", gcode.CodeNotFound,
	)
	// CodeQuestionnaireNotImportable 问卷已发布，禁止全量导入（改用关系列表页新增）。
	CodeQuestionnaireNotImportable = bizerr.MustDefine(
		"EVAL360_QUESTIONNAIRE_NOT_IMPORTABLE", "问卷已发布，不可全量导入，请在关系列表页新增评价关系", gcode.CodeInvalidParameter,
	)
	// CodeImportExcelParseFailed Excel 解析失败。
	CodeImportExcelParseFailed = bizerr.MustDefine(
		"EVAL360_IMPORT_EXCEL_PARSE_FAILED", "评价关系导入文件解析失败", gcode.CodeInvalidParameter,
	)
	// CodeImportSheetReadFailed 工作表读取失败。
	CodeImportSheetReadFailed = bizerr.MustDefine(
		"EVAL360_IMPORT_SHEET_READ_FAILED", "评价关系导入工作表读取失败", gcode.CodeInvalidParameter,
	)
	// CodeImportFileRequired 未上传导入文件。
	CodeImportFileRequired = bizerr.MustDefine(
		"EVAL360_IMPORT_FILE_REQUIRED", "请上传评价关系导入文件", gcode.CodeInvalidParameter,
	)
	// CodeImportHeaderMissing 导入文件表头缺少必需列（按表头识别列，{column} 为缺失的列名）。
	CodeImportHeaderMissing = bizerr.MustDefine(
		"EVAL360_IMPORT_HEADER_MISSING", "The import file is missing the required column \"{column}\"; please use the import template", gcode.CodeInvalidParameter,
	)
	// CodeImportSelfEvaluate 被评估人与评价者为同一员工，禁止自评。
	CodeImportSelfEvaluate = bizerr.MustDefine(
		"EVAL360_IMPORT_SELF_EVALUATE", "被评估人与评价者不能为同一员工", gcode.CodeInvalidParameter,
	)
	// CodeEmployeeNameEmpty 新增评价关系时被评估人或评价者姓名为空。
	CodeEmployeeNameEmpty = bizerr.MustDefine(
		"EVAL360_EMPLOYEE_NAME_EMPTY", "Evaluatee and evaluator names are required", gcode.CodeInvalidParameter,
	)
	// CodeEmployeeNameDuplicate 按姓名录入时存在在职同名员工，无法确定是哪一位（{names} 为重名姓名清单）。
	CodeEmployeeNameDuplicate = bizerr.MustDefine(
		"EVAL360_EMPLOYEE_NAME_DUPLICATE", "These names are shared by more than one active employee, so nothing was submitted: {names}", gcode.CodeInvalidParameter,
	)
	// CodeEmployeeNameNotFound 按姓名录入时找不到在职员工（{names} 为未找到的姓名清单）。
	CodeEmployeeNameNotFound = bizerr.MustDefine(
		"EVAL360_EMPLOYEE_NAME_NOT_FOUND", "No active employee found for these names, so nothing was submitted: {names}", gcode.CodeInvalidParameter,
	)
	// CodeRelationItemsTooMany 单次新增评价关系条目超过上限（{max}）。
	CodeRelationItemsTooMany = bizerr.MustDefine(
		"EVAL360_RELATION_ITEMS_TOO_MANY", "At most {max} evaluation relations can be added at once", gcode.CodeInvalidParameter,
	)
	// CodeRelationItemsEmpty 新增评价关系时未提交任何条目。
	CodeRelationItemsEmpty = bizerr.MustDefine(
		"EVAL360_RELATION_ITEMS_EMPTY", "Please fill in at least one evaluation relation", gcode.CodeInvalidParameter,
	)
	// CodeImportEmpty 模板无有效数据行。
	CodeImportEmpty = bizerr.MustDefine(
		"EVAL360_IMPORT_EMPTY", "导入文件无有效数据行", gcode.CodeInvalidParameter,
	)
	// CodeRelationNotFound 评价关系不存在。
	CodeRelationNotFound = bizerr.MustDefine(
		"EVAL360_RELATION_NOT_FOUND", "评价关系不存在", gcode.CodeNotFound,
	)
	// CodeRoleNotFound 角色不存在。
	CodeRoleNotFound = bizerr.MustDefine(
		"EVAL360_ROLE_NOT_FOUND", "角色不存在", gcode.CodeNotFound,
	)
	// CodeRoleNameEmpty 角色名为空。
	CodeRoleNameEmpty = bizerr.MustDefine(
		"EVAL360_ROLE_NAME_EMPTY", "角色名不能为空", gcode.CodeInvalidParameter,
	)
	// CodeRoleNotEmpty 角色下仍有评价关系，不能删除（合并角色时）。
	CodeRoleNotEmpty = bizerr.MustDefine(
		"EVAL360_ROLE_NOT_EMPTY", "角色下仍有评价关系，请先改派后再删除", gcode.CodeInvalidParameter,
	)
	// CodeEmpcapUnbound empcap 员工查询服务未绑定（employee-core 未启用）。
	CodeEmpcapUnbound = bizerr.MustDefine(
		"EVAL360_EMPCAP_UNBOUND", "员工能力服务未就绪", gcode.CodeNotImplemented,
	)
	// CodeInvalidScore 权重/分值非法（无法解析为数值）。
	CodeInvalidScore = bizerr.MustDefine(
		"EVAL360_INVALID_SCORE", "分值非法", gcode.CodeInvalidParameter,
	)
	// CodeRelationHasSubmission 评价关系已有作答，删除前需二次确认/清空作答。
	CodeRelationHasSubmission = bizerr.MustDefine(
		"EVAL360_RELATION_HAS_SUBMISSION", "该评价关系已有作答，删除前请先清空作答或二次确认", gcode.CodeInvalidParameter,
	)
	// CodeEvaluateeHasSubmission 被评估人下仍有已提交作答，删除前需先清空作答。
	CodeEvaluateeHasSubmission = bizerr.MustDefine(
		"EVAL360_EVALUATEE_HAS_SUBMISSION", "该被评估人已有作答，删除前请先清空作答", gcode.CodeInvalidParameter,
	)
	// CodeRoleHasSubmission 角色下仍有已提交作答，级联删除前需先清空作答。
	CodeRoleHasSubmission = bizerr.MustDefine(
		"EVAL360_ROLE_HAS_SUBMISSION", "该角色下已有作答，删除前请先清空作答", gcode.CodeInvalidParameter,
	)
)
