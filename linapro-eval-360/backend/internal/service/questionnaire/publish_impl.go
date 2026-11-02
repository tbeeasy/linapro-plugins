// publish_impl.go 实现问卷发布前校验与发布状态写入。
//
// 校验（ValidatePublish / ValidateProjectPublish）与写入（MarkPublished）分开：发布以
// 「全部评价者收到飞书卡片」为前提，调用方须在校验通过、推送全员送达后才写发布状态。
package questionnaire

import (
	"context"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// ValidatePublish 校验单份问卷能否发布（题目非空 + 角色权重合规），不写发布状态。
// 返回问卷所属项目 id 与是否需要发布（已发布幂等返回 false、不再校验），
// 供调用方先推送、全员送达后再调用 MarkPublished。
func (s *serviceImpl) ValidatePublish(ctx context.Context, id int64) (projectID int64, needPublish bool, err error) {
	q, err := s.mustGetQuestionnaire(ctx, id)
	if err != nil {
		return 0, false, err
	}
	if q.Published == publishedYes {
		return q.ProjectId, false, nil // 幂等：已发布无需重复。
	}
	if err = s.validateForPublish(ctx, id); err != nil {
		return 0, false, err
	}
	return q.ProjectId, true, nil
}

// Unpublish 取消发布单份问卷。
func (s *serviceImpl) Unpublish(ctx context.Context, id int64) error {
	q, err := s.mustGetQuestionnaire(ctx, id)
	if err != nil {
		return err
	}
	if q.Published == publishedNo {
		return nil
	}
	return s.setPublished(ctx, id, publishedNo)
}

// ValidateProjectPublish 校验项目下全部未发布问卷能否发布（已发布跳过、幂等），不写发布状态。
// 任一问卷不合规即整体失败；全部合规返回待发布问卷 id（按 sort、id 升序，无则空切片）。
func (s *serviceImpl) ValidateProjectPublish(ctx context.Context, projectID int64) ([]int64, error) {
	cols := dao.Questionnaire.Columns()
	var rows []*entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Fields(cols.Id).
		Where(cols.ProjectId, projectID).
		Where(cols.Published, publishedNo).
		OrderAsc(cols.Sort).OrderAsc(cols.Id).
		Scan(&rows); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, q := range rows {
		if err := s.validateForPublish(ctx, q.Id); err != nil {
			return nil, err
		}
		ids = append(ids, q.Id)
	}
	return ids, nil
}

// MarkPublished 一次性把给定问卷置为已发布（单条 UPDATE，天然原子）。只翻转仍为未发布的行，
// 已发布的行不受影响；ids 为空时直接返回。
func (s *serviceImpl) MarkPublished(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	cols := dao.Questionnaire.Columns()
	_, err := dao.Questionnaire.Ctx(ctx).
		WhereIn(cols.Id, ids).
		Where(cols.Published, publishedNo).
		Data(do.Questionnaire{Published: publishedYes}).
		Update()
	return err
}

// setPublished 写入问卷发布状态。
func (s *serviceImpl) setPublished(ctx context.Context, id int64, published int) error {
	_, err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, id).
		Data(map[string]any{dao.Questionnaire.Columns().Published: published}).Update()
	return err
}

// validateForPublish 校验问卷是否满足发布条件：题目非空 + 角色权重合规。
func (s *serviceImpl) validateForPublish(ctx context.Context, questionnaireID int64) error {
	qCnt, err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, questionnaireID).Count()
	if err != nil {
		return err
	}
	if qCnt == 0 {
		return bizerr.NewCode(CodeQuestionnaireNoQuestions)
	}
	return s.validateRoleWeights(ctx, questionnaireID)
}

// validateRoleWeights 校验角色权重：每个角色 > 0 且各角色之和 = 100%。
func (s *serviceImpl) validateRoleWeights(ctx context.Context, questionnaireID int64) error {
	var roles []*entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().QuestionnaireId, questionnaireID).Scan(&roles); err != nil {
		return err
	}
	return checkRoleWeights(roles)
}
