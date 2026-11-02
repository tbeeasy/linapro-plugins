// question.go 实现题目 CRUD 控制器方法。新增与编辑共用 SaveQuestion 服务。
package evalcontroller

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// CreateQuestion 新建题目。
func (c *ControllerV1) CreateQuestion(ctx context.Context, req *v1.CreateQuestionReq) (res *v1.CreateQuestionRes, err error) {
	id, err := c.questionnaireSvc.SaveQuestion(ctx, toSaveQuestionInput(0, req.QuestionnaireId, req.DimensionId, req.Type, req.Title, req.Description, req.Required, req.Sort, req.Options, req.MatrixColumns, req.VisibleRoleIds, req.ExcludedEvaluatorEmpIds))
	if err != nil {
		return nil, err
	}
	return &v1.CreateQuestionRes{Id: id}, nil
}

// UpdateQuestion 就地编辑题目（存在作答时按白名单强校验）。
func (c *ControllerV1) UpdateQuestion(ctx context.Context, req *v1.UpdateQuestionReq) (res *v1.UpdateQuestionRes, err error) {
	id, err := c.questionnaireSvc.SaveQuestion(ctx, toSaveQuestionInput(req.Id, req.QuestionnaireId, req.DimensionId, req.Type, req.Title, req.Description, req.Required, req.Sort, req.Options, req.MatrixColumns, req.VisibleRoleIds, req.ExcludedEvaluatorEmpIds))
	if err != nil {
		return nil, err
	}
	return &v1.UpdateQuestionRes{Id: id}, nil
}

// DeleteQuestion 删除题目。
func (c *ControllerV1) DeleteQuestion(ctx context.Context, req *v1.DeleteQuestionReq) (res *v1.DeleteQuestionRes, err error) {
	if err = c.questionnaireSvc.DeleteQuestion(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteQuestionRes{}, nil
}

// ReorderQuestions 按给定顺序重排维度下题目（插入新题后整体顺延）。
func (c *ControllerV1) ReorderQuestions(ctx context.Context, req *v1.ReorderQuestionsReq) (res *v1.ReorderQuestionsRes, err error) {
	err = c.questionnaireSvc.ReorderQuestions(ctx, qsvc.ReorderQuestionsInput{
		QuestionnaireID: req.QuestionnaireId,
		DimensionID:     req.DimensionId,
		QuestionIDs:     req.QuestionIds,
	})
	if err != nil {
		return nil, err
	}
	return &v1.ReorderQuestionsRes{}, nil
}

// toSaveQuestionInput 把 API 题目入参映射为服务层入参。
func toSaveQuestionInput(id, questionnaireID, dimensionID int64, qType, title, description string, required, sort int,
	options []v1.QuestionOptionDTO, cols []v1.MatrixColumnDTO,
	visibleRoleIDs, excludedEvaluatorEmpIDs []int64) qsvc.SaveQuestionInput {
	in := qsvc.SaveQuestionInput{
		Id:                      id,
		QuestionnaireID:         questionnaireID,
		DimensionID:             dimensionID,
		Type:                    qType,
		Title:                   title,
		Description:             description,
		Required:                required,
		Sort:                    sort,
		VisibleRoleIDs:          visibleRoleIDs,
		ExcludedEvaluatorEmpIDs: excludedEvaluatorEmpIDs,
	}
	for _, o := range options {
		in.Options = append(in.Options, qsvc.OptionInput{
			Id: o.Id, Label: o.Label, Score: o.Score, AllowText: o.AllowText, Sort: o.Sort,
		})
	}
	for _, c := range cols {
		in.MatrixColumns = append(in.MatrixColumns, qsvc.MatrixColumnInput{
			Id: c.Id, Label: c.Label, Score: c.Score, Sort: c.Sort,
			AllowComment:       c.AllowComment,
			CommentRequired:    c.CommentRequired,
			CommentPlaceholder: c.CommentPlaceholder,
			CommentPresets:     c.CommentPresets,
			CommentAllowCustom: c.CommentAllowCustom,
		})
	}
	return in
}
