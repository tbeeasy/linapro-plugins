// dimension_impl.go 实现维度 CRUD。
package questionnaire

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// CreateDimension 在问卷下新增维度。
func (s *serviceImpl) CreateDimension(ctx context.Context, in CreateDimensionInput) (int64, error) {
	if _, err := s.mustGetQuestionnaire(ctx, in.QuestionnaireID); err != nil {
		return 0, err
	}
	return dao.Dimension.Ctx(ctx).Data(do.Dimension{
		QuestionnaireId: in.QuestionnaireID,
		Name:            in.Name,
		Sort:            in.Sort,
	}).InsertAndGetId()
}

// UpdateDimension 修改维度名称/排序。
func (s *serviceImpl) UpdateDimension(ctx context.Context, in UpdateDimensionInput) error {
	if _, err := s.mustGetDimension(ctx, in.Id); err != nil {
		return err
	}
	data := g.Map{}
	if in.Name != nil {
		data[dao.Dimension.Columns().Name] = *in.Name
	}
	if in.Sort != nil {
		data[dao.Dimension.Columns().Sort] = *in.Sort
	}
	if len(data) == 0 {
		return nil
	}
	_, err := dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().Id, in.Id).
		Data(data).Update()
	return err
}

// DeleteDimension 删除维度（含题目时拒绝，须先移除题目）。
func (s *serviceImpl) DeleteDimension(ctx context.Context, id int64) error {
	if _, err := s.mustGetDimension(ctx, id); err != nil {
		return err
	}
	cnt, err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().DimensionId, id).Count()
	if err != nil {
		return err
	}
	if cnt > 0 {
		return bizerr.NewCode(CodeDimensionNotEmpty)
	}
	_, err = dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().Id, id).Delete()
	return err
}

// mustGetDimension 读取维度（租户过滤），不存在返回 CodeDimensionNotFound。
func (s *serviceImpl) mustGetDimension(ctx context.Context, id int64) (*entitymodel.Dimension, error) {
	var d *entitymodel.Dimension
	if err := dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().Id, id).Scan(&d); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, bizerr.NewCode(CodeDimensionNotFound)
	}
	return d, nil
}
