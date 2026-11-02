// scoped.go 提供 serviceImpl 的问卷/角色/被评估人/关系读取辅助与问卷可见性辅助。
// 本插件为平台级，不做租户过滤。
package relation

import (
	"context"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// mustGetQuestionnaire 读取问卷，不存在返回错误。
// 这里不依赖 questionnaire 模块的错误码（避免跨模块 import），用一个本地 NotFound。
func (s *serviceImpl) mustGetQuestionnaire(ctx context.Context, id int64) (*entitymodel.Questionnaire, error) {
	var q *entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, id).
		Scan(&q); err != nil {
		return nil, err
	}
	if q == nil {
		return nil, bizerr.NewCode(CodeQuestionnaireNotFound)
	}
	return q, nil
}

// findRoleByName 按归一化后的名称查找问卷内的角色（精确匹配）。
func (s *serviceImpl) findRoleByName(ctx context.Context, questionnaireID int64, normName string) (*entitymodel.Role, error) {
	var r *entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().QuestionnaireId, questionnaireID).
		Where(dao.Role.Columns().Name, normName).
		Scan(&r); err != nil {
		return nil, err
	}
	return r, nil
}

// upsertRole 按归一化名称 upsert 角色：存在则返回现有，不存在则建（默认权重 0）。
func (s *serviceImpl) upsertRole(ctx context.Context, questionnaireID int64, normName string) (int64, error) {
	existing, err := s.findRoleByName(ctx, questionnaireID, normName)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.Id, nil
	}
	w := weightZero
	id, err := dao.Role.Ctx(ctx).Data(do.Role{
		QuestionnaireId: questionnaireID,
		Name:            normName,
		Weight:          &w,
	}).InsertAndGetId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// mustGetRole 读取角色，不存在返回 CodeRoleNotFound。
func (s *serviceImpl) mustGetRole(ctx context.Context, id int64) (*entitymodel.Role, error) {
	var r *entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().Id, id).Scan(&r); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, bizerr.NewCode(CodeRoleNotFound)
	}
	return r, nil
}

// findEvaluateeByEmp 按问卷 + EmpID 查找被评估人（唯一键兜底）。
func (s *serviceImpl) findEvaluateeByEmp(ctx context.Context, questionnaireID, empID int64) (*entitymodel.Evaluatee, error) {
	var e *entitymodel.Evaluatee
	if err := dao.Evaluatee.Ctx(ctx).
		Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).
		Where(dao.Evaluatee.Columns().EvaluateeEmpId, empID).
		Scan(&e); err != nil {
		return nil, err
	}
	return e, nil
}

// mustGetEvaluatee 读取被评估人，不存在返回错误。
func (s *serviceImpl) mustGetEvaluatee(ctx context.Context, id int64) (*entitymodel.Evaluatee, error) {
	var e *entitymodel.Evaluatee
	if err := dao.Evaluatee.Ctx(ctx).
		Where(dao.Evaluatee.Columns().Id, id).Scan(&e); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, bizerr.NewCode(CodeRelationNotFound)
	}
	return e, nil
}

// mustGetRelation 读取评价关系，不存在返回 CodeRelationNotFound。
func (s *serviceImpl) mustGetRelation(ctx context.Context, id int64) (*entitymodel.Relation, error) {
	var r *entitymodel.Relation
	if err := dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().Id, id).Scan(&r); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, bizerr.NewCode(CodeRelationNotFound)
	}
	return r, nil
}
