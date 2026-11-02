// name_unique.go 实现问卷标题在同项目内唯一的校验基础件。
//
// 唯一性口径：标题先经 textutil.NormalizeName 归一化（全角空格转半角 + 折叠内部空白 + 去首尾空白），
// 再做**大小写敏感**的精确匹配。同一项目下归一化后相同的两个标题视为重名。
//
// 两级兜底：本文件做应用层预检（给出友好错误码），真正的一致性由数据库唯一索引
// uq_eval_360_questionnaire_project_title 保证；并发穿透时由调用方捕获唯一冲突转同一错误码。
package questionnaire

import (
	"context"
	"strings"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
)

// titleExists 判断同一项目下是否存在同名（归一化后精确匹配）问卷。
// excludeID > 0 时排除该问卷自身——改名场景下问卷必然与「自己」重名，不排除会永远误报。
func (s *serviceImpl) titleExists(ctx context.Context, projectID int64, title string, excludeID int64) (bool, error) {
	m := dao.Questionnaire.Ctx(ctx).Where(dao.Questionnaire.Columns().ProjectId, projectID)
	if excludeID > 0 {
		m = m.WhereNot(dao.Questionnaire.Columns().Id, excludeID)
	}
	cnt, err := m.Where(dao.Questionnaire.Columns().Title, title).Count()
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// isUniqueViolation 判定错误是否为唯一索引冲突（并发穿透兜底）。
// 应用层预检与写入之间存在竞态窗口，并发请求可能双双通过预检；此时由数据库唯一索引
// 拒绝其一，本函数把该错误识别出来，供调用方转为统一的「名称已存在」错误码。
// GoFrame 把唯一冲突归为 CodeDbOperationError，错误信息含 "duplicate key" / "unique constraint"。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
