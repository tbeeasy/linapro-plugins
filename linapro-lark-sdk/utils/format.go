package utils

import (
	"fmt"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

// CodeErrorDetail 从飞书 CodeError 提取结构化的字段级违规详情，拼成可读串附加到错误信息。
// 飞书对 99992402（field validation failed）等错误会在 error.field_violations / error.details
// 里给出**具体哪个字段、什么值、为什么被拒**；这些信息不在顶层 msg 中，不提取就只能靠猜。
// 无任何详情时返回空串，不污染错误信息。
func CodeErrorDetail(ce larkcore.CodeError) string {
	if ce.Err == nil {
		return ""
	}
	var sb strings.Builder
	for _, fv := range ce.Err.FieldViolations {
		if fv == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("; field_violation field=%q value=%q desc=%q", fv.Field, fv.Value, fv.Description))
	}
	for _, d := range ce.Err.Details {
		if d == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("; detail key=%q value=%q", d.Key, d.Value))
	}
	for _, pv := range ce.Err.PermissionViolations {
		if pv == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("; permission_violation type=%q subject=%q desc=%q", pv.Type, pv.Subject, pv.Description))
	}
	return sb.String()
}
