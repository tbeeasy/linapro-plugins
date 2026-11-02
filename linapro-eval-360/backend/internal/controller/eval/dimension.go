// dimension.go 实现维度 CRUD 控制器方法。
package evalcontroller

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
)

// CreateDimension 新建维度。
func (c *ControllerV1) CreateDimension(ctx context.Context, req *v1.CreateDimensionReq) (res *v1.CreateDimensionRes, err error) {
	id, err := c.questionnaireSvc.CreateDimension(ctx, qsvc.CreateDimensionInput{
		QuestionnaireID: req.QuestionnaireId,
		Name:            req.Name,
		Sort:            req.Sort,
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateDimensionRes{Id: id}, nil
}

// UpdateDimension 修改维度。
func (c *ControllerV1) UpdateDimension(ctx context.Context, req *v1.UpdateDimensionReq) (res *v1.UpdateDimensionRes, err error) {
	if err = c.questionnaireSvc.UpdateDimension(ctx, qsvc.UpdateDimensionInput{
		Id:   req.Id,
		Name: req.Name,
		Sort: req.Sort,
	}); err != nil {
		return nil, err
	}
	return &v1.UpdateDimensionRes{}, nil
}

// DeleteDimension 删除维度。
func (c *ControllerV1) DeleteDimension(ctx context.Context, req *v1.DeleteDimensionReq) (res *v1.DeleteDimensionRes, err error) {
	if err = c.questionnaireSvc.DeleteDimension(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteDimensionRes{}, nil
}
