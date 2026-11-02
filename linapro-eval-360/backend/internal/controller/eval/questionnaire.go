// questionnaire.go 实现问卷 CRUD 控制器方法。
package evalcontroller

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// ListQuestionnaire 列出某项目下的问卷。
func (c *ControllerV1) ListQuestionnaire(ctx context.Context, req *v1.ListQuestionnaireReq) (res *v1.ListQuestionnaireRes, err error) {
	list, err := c.questionnaireSvc.ListByProject(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	return &v1.ListQuestionnaireRes{List: list}, nil
}

// PageQuestionnaire 分页返回项目下的问卷卡片（含完成率统计）。
func (c *ControllerV1) PageQuestionnaire(ctx context.Context, req *v1.PageQuestionnaireReq) (res *v1.PageQuestionnaireRes, err error) {
	items, total, err := c.projectSvc.PageQuestionnaires(ctx, req.ProjectId, req.Keyword, req.PageNum, req.PageSize)
	if err != nil {
		return nil, err
	}
	cards := make([]*v1.QuestionnaireCard, 0, len(items))
	for _, it := range items {
		cards = append(cards, &v1.QuestionnaireCard{
			Id: it.Id, Title: it.Title, Published: it.Published, Sort: it.Sort,
			EvaluateeCount: it.EvaluateeCount, Completed: it.Completed, Expected: it.Expected,
			CreatedAt: timePtrToMillis(it.CreatedAt),
		})
	}
	return &v1.PageQuestionnaireRes{Items: cards, Total: total}, nil
}

// GetQuestionnaire 返回问卷完整结构树。
func (c *ControllerV1) GetQuestionnaire(ctx context.Context, req *v1.GetQuestionnaireReq) (res *v1.GetQuestionnaireRes, err error) {
	detail, err := c.questionnaireSvc.GetDetail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.GetQuestionnaireRes{Detail: detail}, nil
}

// CreateQuestionnaire 新建问卷。
func (c *ControllerV1) CreateQuestionnaire(ctx context.Context, req *v1.CreateQuestionnaireReq) (res *v1.CreateQuestionnaireRes, err error) {
	id, err := c.questionnaireSvc.CreateQuestionnaire(ctx, qsvc.CreateQuestionnaireInput{
		ProjectID:   req.ProjectId,
		Title:       req.Title,
		Description: req.Description,
		Sort:        req.Sort,
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateQuestionnaireRes{Id: id}, nil
}

// UpdateQuestionnaire 就地修改问卷元信息。
func (c *ControllerV1) UpdateQuestionnaire(ctx context.Context, req *v1.UpdateQuestionnaireReq) (res *v1.UpdateQuestionnaireRes, err error) {
	if err = c.questionnaireSvc.UpdateQuestionnaire(ctx, qsvc.UpdateQuestionnaireInput{
		Id:          req.Id,
		Title:       req.Title,
		Description: req.Description,
		Sort:        req.Sort,
	}); err != nil {
		return nil, err
	}
	return &v1.UpdateQuestionnaireRes{}, nil
}

// DeleteQuestionnaire 删除问卷。
func (c *ControllerV1) DeleteQuestionnaire(ctx context.Context, req *v1.DeleteQuestionnaireReq) (res *v1.DeleteQuestionnaireRes, err error) {
	if err = c.questionnaireSvc.DeleteQuestionnaire(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteQuestionnaireRes{}, nil
}
