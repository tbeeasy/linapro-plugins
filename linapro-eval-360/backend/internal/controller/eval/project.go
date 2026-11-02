// project.go 实现 360 测评管理端「项目与发送编排」控制器方法。
package evalcontroller

import (
	"context"
	"time"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	psvc "lina-plugin-linapro-eval-360/backend/internal/service/project"
)

// timePtrToMillis 把 *time.Time 投影为 Unix 毫秒时间戳（nil 透传 nil）。
func timePtrToMillis(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

// millisToTimePtr 把 Unix 毫秒时间戳解析回 *time.Time（nil 透传 nil）。
func millisToTimePtr(ms *int64) *time.Time {
	if ms == nil {
		return nil
	}
	t := time.UnixMilli(*ms)
	return &t
}

// ListProject 列出当前租户下的全部项目。
func (c *ControllerV1) ListProject(ctx context.Context, req *v1.ListProjectReq) (res *v1.ListProjectRes, err error) {
	list, err := c.projectSvc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.ProjectListItem, 0, len(list))
	for _, p := range list {
		out = append(out, toProjectListItemDTO(p))
	}
	return &v1.ListProjectRes{List: out}, nil
}

// GetProject 返回项目详情。
func (c *ControllerV1) GetProject(ctx context.Context, req *v1.GetProjectReq) (res *v1.GetProjectRes, err error) {
	detail, err := c.projectSvc.GetDetail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.GetProjectRes{Detail: toProjectDetailDTO(detail)}, nil
}

// CreateProject 新建项目。
func (c *ControllerV1) CreateProject(ctx context.Context, req *v1.CreateProjectReq) (res *v1.CreateProjectRes, err error) {
	id, err := c.projectSvc.Create(ctx, psvc.CreateInput{
		Name: req.Name, Description: req.Description,
		StartAt: millisToTimePtr(req.StartAt), EndAt: millisToTimePtr(req.EndAt),
	})
	if err != nil {
		return nil, err
	}
	return &v1.CreateProjectRes{Id: id}, nil
}

// UpdateProject 就地修改项目元信息。
func (c *ControllerV1) UpdateProject(ctx context.Context, req *v1.UpdateProjectReq) (res *v1.UpdateProjectRes, err error) {
	if err = c.projectSvc.Update(ctx, psvc.UpdateInput{
		Id: req.Id, Name: req.Name, Description: req.Description,
		StartAt: millisToTimePtr(req.StartAt), EndAt: millisToTimePtr(req.EndAt),
	}); err != nil {
		return nil, err
	}
	return &v1.UpdateProjectRes{}, nil
}

// DeleteProject 删除项目。
func (c *ControllerV1) DeleteProject(ctx context.Context, req *v1.DeleteProjectReq) (res *v1.DeleteProjectRes, err error) {
	if err = c.projectSvc.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteProjectRes{}, nil
}

// CloseProject 提前结束项目。
func (c *ControllerV1) CloseProject(ctx context.Context, req *v1.CloseProjectReq) (res *v1.CloseProjectRes, err error) {
	if err = c.projectSvc.Close(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.CloseProjectRes{}, nil
}

// ReopenProject 重新开启已提前结束的项目。
func (c *ControllerV1) ReopenProject(ctx context.Context, req *v1.ReopenProjectReq) (res *v1.ReopenProjectRes, err error) {
	if err = c.projectSvc.Reopen(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.ReopenProjectRes{}, nil
}

// SendProject 一键发送。
func (c *ControllerV1) SendProject(ctx context.Context, req *v1.SendProjectReq) (res *v1.SendProjectRes, err error) {
	result, err := c.projectSvc.SendAll(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.SendProjectRes{PublishedIDs: result.PublishedIDs, Failed: toSendFailedDTOs(result.Failed)}, nil
}

// toSendFailedDTOs 把一键发送的未发布问卷清单转为 API 层 DTO（nil 时返回空切片）。
func toSendFailedDTOs(items []*psvc.SendFailure) []*v1.SendFailedItem {
	out := make([]*v1.SendFailedItem, 0, len(items))
	for _, it := range items {
		out = append(out, &v1.SendFailedItem{
			QuestionnaireId: it.QuestionnaireID,
			Title:           it.Title,
			Reason:          string(it.Reason),
			EmployeeNos:     it.EmployeeNos,
		})
	}
	return out
}

// toProjectListItemDTO 把 service 层列表项转为 API 层 DTO。
func toProjectListItemDTO(p *psvc.ProjectListItem) *v1.ProjectListItem {
	if p == nil {
		return nil
	}
	return &v1.ProjectListItem{
		Id: p.Id, Name: p.Name, Description: p.Description, Status: p.Status,
		StartAt: timePtrToMillis(p.StartAt), EndAt: timePtrToMillis(p.EndAt), ClosedAt: timePtrToMillis(p.ClosedAt),
	}
}

// toProjectDetailDTO 把 service 层详情转为 API 层 DTO。
func toProjectDetailDTO(d *psvc.ProjectDetail) *v1.ProjectDetail {
	if d == nil {
		return nil
	}
	item := toProjectListItemDTO(&d.ProjectListItem)
	cards := make([]*v1.QuestionnaireCard, 0, len(d.Questionnaires))
	for _, c := range d.Questionnaires {
		cards = append(cards, &v1.QuestionnaireCard{
			Id: c.Id, Title: c.Title, Published: c.Published, Sort: c.Sort,
			EvaluateeCount: c.EvaluateeCount, Completed: c.Completed, Expected: c.Expected,
			CreatedAt: timePtrToMillis(c.CreatedAt),
		})
	}
	return &v1.ProjectDetail{
		ProjectListItem: *item,
		Questionnaires:  cards,
		TotalCompleted:  d.TotalCompleted,
		TotalExpected:   d.TotalExpected,
	}
}
