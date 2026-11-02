// role_impl.go 实现角色 CRUD（列表/改权重/重命名/合并/删除）。
package relation

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// ListRoles 返回问卷下的全部角色（按 id 升序）。
func (s *serviceImpl) ListRoles(ctx context.Context, questionnaireID int64) ([]*RoleItem, error) {
	if _, err := s.mustGetQuestionnaire(ctx, questionnaireID); err != nil {
		return nil, err
	}
	var rows []*entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Role.Columns().Id).Scan(&rows); err != nil {
		return nil, err
	}
	items := make([]*RoleItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, &RoleItem{Id: r.Id, Name: r.Name, Weight: decimalString(r.Weight)})
	}
	return items, nil
}

// UpdateRoleWeight 修改单个角色权重（HR 在发布前配置）。
func (s *serviceImpl) UpdateRoleWeight(ctx context.Context, roleID int64, weight string) error {
	if _, err := s.mustGetRole(ctx, roleID); err != nil {
		return err
	}
	w, err := parseWeight(weight)
	if err != nil {
		return err
	}
	_, err = dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().Id, roleID).
		Data(map[string]any{dao.Role.Columns().Weight: &w}).Update()
	return err
}

// RenameRole 重命名角色（改实体名，关系走 FK 不受影响）。
// 名称先归一化后精确匹配，避免与现有角色撞重名。
func (s *serviceImpl) RenameRole(ctx context.Context, roleID int64, name string) error {
	r, err := s.mustGetRole(ctx, roleID)
	if err != nil {
		return err
	}
	norm := NormalizeRoleName(name)
	if norm == "" {
		return bizerr.NewCode(CodeRoleNameEmpty)
	}
	// 若归一化后名称未变，幂等返回。
	if norm == r.Name {
		return nil
	}
	// 撞问卷内其他角色名 → 视为合并语义，提示走合并。
	existing, err := s.findRoleByName(ctx, r.QuestionnaireId, norm)
	if err != nil {
		return err
	}
	if existing != nil && existing.Id != roleID {
		return bizerr.NewCode(CodeRoleNameEmpty)
	}
	_, err = dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().Id, roleID).
		Data(map[string]any{dao.Role.Columns().Name: norm}).Update()
	return err
}

// MergeRole 合并角色：把 fromID 名下的评价关系改派到 toID，再删除空角色 fromID。
// fromID 与 toID 必须同属一份问卷；fromID 下仍有关系时由本方法先改派，故删除前不会再有悬挂关系。
func (s *serviceImpl) MergeRole(ctx context.Context, fromID, toID int64) error {
	if fromID == toID {
		return bizerr.NewCode(CodeRoleNotFound)
	}
	from, err := s.mustGetRole(ctx, fromID)
	if err != nil {
		return err
	}
	to, err := s.mustGetRole(ctx, toID)
	if err != nil {
		return err
	}
	if from.QuestionnaireId != to.QuestionnaireId {
		return bizerr.NewCode(CodeRoleNotFound)
	}
	return dao.Role.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 1. 把 fromID 名下的评价关系改派到 toID。
		if _, err := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().RoleId, fromID).
			Data(map[string]any{dao.Relation.Columns().RoleId: toID}).Update(); err != nil {
			return err
		}
		// 2. 删除空角色 fromID（此时其下已无关系）。
		_, err := dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().Id, fromID).Delete()
		return err
	})
}

// DeleteRole 删除问卷下的一个角色。cascade=false 时其下仍有评价关系即拒绝（CodeRoleNotEmpty），
// 引导先在关系明细里改派/删除评价关系；cascade=true 时连带删除该角色下的全部评价关系。
// 角色是问卷级的，级联会同时清掉各个被评估人在该角色下的关系。
// 任一评价关系已有作答时一律拒绝（CodeRoleHasSubmission），引导先清空作答。
func (s *serviceImpl) DeleteRole(ctx context.Context, roleID int64, cascade bool) error {
	role, err := s.mustGetRole(ctx, roleID)
	if err != nil {
		return err
	}
	// 角色下全部评价关系 id：作答表无 role_id，只能经 relation 关联统计。
	relIDs, err := dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().RoleId, roleID).
		Fields(dao.Relation.Columns().Id).Array()
	if err != nil {
		return err
	}
	if len(relIDs) == 0 {
		_, err = dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().Id, roleID).Delete()
		return err
	}
	if !cascade {
		return bizerr.NewCode(CodeRoleNotEmpty)
	}
	// 作答拦截：按 relation_id 一次性统计，避免逐条查询（无 N+1）。
	cnt, err := dao.Submission.Ctx(ctx).
		WhereIn(dao.Submission.Columns().RelationId, relIDs).Count()
	if err != nil {
		return err
	}
	if cnt > 0 {
		return bizerr.NewCode(CodeRoleHasSubmission)
	}
	return dao.Role.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 评价关系删除同时带上问卷维度，与 DeleteEvaluatee 一致、命中组合索引。
		if _, dErr := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, role.QuestionnaireId).
			Where(dao.Relation.Columns().RoleId, roleID).Delete(); dErr != nil {
			return dErr
		}
		_, dErr := dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().Id, roleID).Delete()
		return dErr
	})
}

// parseWeight 把权重字符串解析为 decimal，空串记 0，非法返回错误。
func parseWeight(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, bizerr.NewCode(CodeInvalidScore)
	}
	return d, nil
}

// decimalString 把 NUMERIC 指针转为字符串，nil 记 "0"。
func decimalString(d *decimal.Decimal) string {
	if d == nil {
		return "0"
	}
	return d.String()
}
