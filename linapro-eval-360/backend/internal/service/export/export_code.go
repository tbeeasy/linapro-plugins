// 本文件定义 linapro-eval-360 明细导出模块（eval-export）的业务错误码。
package export

import (
	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeQuestionnaireNotFound 问卷不存在（本模块自用，避免跨模块 import 错误码）。
	CodeQuestionnaireNotFound = bizerr.MustDefine(
		"EVAL360_EXPORT_QUESTIONNAIRE_NOT_FOUND", "问卷不存在", gcode.CodeNotFound,
	)
	// CodeExportExcelFailed 生成 Excel 文件失败。
	CodeExportExcelFailed = bizerr.MustDefine(
		"EVAL360_EXPORT_EXCEL_FAILED", "导出 Excel 失败", gcode.CodeInternalError,
	)
)
