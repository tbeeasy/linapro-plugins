// name_unique.go 实现项目名称全局唯一的校验基础件。
//
// 唯一性口径：名称先经 textutil.NormalizeName 归一化（全角空格转半角 + 折叠内部空白 + 去首尾空白），
// 再做**大小写敏感**的精确匹配。本插件为平台级（无 tenant_id），故是**全平台唯一**而非租户内唯一。
//
// 两级兜底：本文件做应用层预检（给出友好错误码），真正的一致性由数据库唯一索引
// uq_eval_360_project_name 保证；并发穿透时由调用方捕获唯一冲突转同一错误码。
package project

import (
	"context"
	"strings"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
)

// projectNameExists 判断是否已存在同名（归一化后精确匹配）项目。
// excludeID > 0 时排除该项目自身——改名场景下项目必然与「自己」重名，不排除会永远误报。
func (s *serviceImpl) projectNameExists(ctx context.Context, name string, excludeID int64) (bool, error) {
	m := dao.Project.Ctx(ctx)
	if excludeID > 0 {
		m = m.WhereNot(dao.Project.Columns().Id, excludeID)
	}
	cnt, err := m.Where(dao.Project.Columns().Name, name).Count()
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
