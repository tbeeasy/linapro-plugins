// relation.go 实现 360 测评管理端「评价关系与角色」控制器方法。
// 文件下载（导入模板/导出关系）走 WriteOver + ExitAll，避免响应中间件包裹 JSON。
package evalcontroller

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/closeutil"
	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	rsvc "lina-plugin-linapro-eval-360/backend/internal/service/relation"
)

// ListRoles 列出问卷下的全部角色。
func (c *ControllerV1) ListRoles(ctx context.Context, req *v1.ListRolesReq) (res *v1.ListRolesRes, err error) {
	list, err := c.relationSvc.ListRoles(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.RoleItem, 0, len(list))
	for _, r := range list {
		out = append(out, &v1.RoleItem{Id: r.Id, Name: r.Name, Weight: r.Weight})
	}
	return &v1.ListRolesRes{List: out}, nil
}

// UpdateRoleWeight 修改单个角色权重。
func (c *ControllerV1) UpdateRoleWeight(ctx context.Context, req *v1.UpdateRoleWeightReq) (res *v1.UpdateRoleWeightRes, err error) {
	if err = c.relationSvc.UpdateRoleWeight(ctx, req.Id, req.Weight); err != nil {
		return nil, err
	}
	return &v1.UpdateRoleWeightRes{}, nil
}

// RenameRole 重命名角色。
func (c *ControllerV1) RenameRole(ctx context.Context, req *v1.RenameRoleReq) (res *v1.RenameRoleRes, err error) {
	if err = c.relationSvc.RenameRole(ctx, req.Id, req.Name); err != nil {
		return nil, err
	}
	return &v1.RenameRoleRes{}, nil
}

// MergeRole 合并角色。
func (c *ControllerV1) MergeRole(ctx context.Context, req *v1.MergeRoleReq) (res *v1.MergeRoleRes, err error) {
	if err = c.relationSvc.MergeRole(ctx, req.FromId, req.ToId); err != nil {
		return nil, err
	}
	return &v1.MergeRoleRes{}, nil
}

// DeleteRole 删除角色；cascade=true 时连带删除其下全部评价关系。
func (c *ControllerV1) DeleteRole(ctx context.Context, req *v1.DeleteRoleReq) (res *v1.DeleteRoleRes, err error) {
	if err = c.relationSvc.DeleteRole(ctx, req.Id, req.Cascade); err != nil {
		return nil, err
	}
	return &v1.DeleteRoleRes{}, nil
}

// ImportTemplate 下载评价关系全量导入模板。
func (c *ControllerV1) ImportTemplate(ctx context.Context, req *v1.ImportTemplateReq) (res *v1.ImportTemplateRes, err error) {
	data, err := c.relationSvc.GenerateImportTemplate(ctx)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	r.Response.Header().Set("Content-Disposition", "attachment; filename=eval360-relation-import-template.xlsx")
	r.Response.WriteOver(data)
	r.ExitAll()
	return nil, nil
}

// ImportRelations 全量导入评价关系。
func (c *ControllerV1) ImportRelations(ctx context.Context, req *v1.ImportRelationsReq) (res *v1.ImportRelationsRes, err error) {
	r := g.RequestFromCtx(ctx)
	file := r.GetUploadFile("file")
	if file == nil {
		return nil, bizerr.NewCode(rsvc.CodeImportFileRequired)
	}
	f, err := file.Open()
	if err != nil {
		return nil, bizerr.WrapCode(err, rsvc.CodeImportExcelParseFailed)
	}
	defer closeutil.Close(ctx, f, &err, "close eval360 relation import file failed")

	result, err := c.relationSvc.ImportRelations(ctx, rsvc.ImportInput{
		QuestionnaireID: req.QuestionnaireId,
		Reader:          f,
	})
	if err != nil {
		return nil, err
	}
	failList := make([]v1.ImportFailItem, 0, len(result.FailList))
	for _, item := range result.FailList {
		failList = append(failList, v1.ImportFailItem{Row: item.Row, Reason: item.Reason})
	}
	return &v1.ImportRelationsRes{Success: result.Success, Fail: result.Fail, FailList: failList}, nil
}

// ExportRelations 导出评价关系 Excel。
func (c *ControllerV1) ExportRelations(ctx context.Context, req *v1.ExportRelationsReq) (res *v1.ExportRelationsRes, err error) {
	data, err := c.relationSvc.ExportRelations(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	r.Response.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E8%AF%84%E4%BB%B7%E5%85%B3%E7%B3%BB%E5%AF%BC%E5%87%BA.xlsx")
	r.Response.WriteOver(data)
	r.ExitAll()
	return nil, nil
}

// ListRelations 列出评价关系明细。
func (c *ControllerV1) ListRelations(ctx context.Context, req *v1.ListRelationsReq) (res *v1.ListRelationsRes, err error) {
	matrix, err := c.relationSvc.ListRelations(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	return &v1.ListRelationsRes{Matrix: toRelationMatrixDTO(matrix)}, nil
}

// SearchEmployees 搜索在职员工。
func (c *ControllerV1) SearchEmployees(ctx context.Context, req *v1.SearchEmployeesReq) (res *v1.SearchEmployeesRes, err error) {
	list, err := c.relationSvc.SearchEmployees(ctx, req.Keyword)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.EmployeeOption, 0, len(list))
	for _, e := range list {
		out = append(out, &v1.EmployeeOption{EmpID: e.EmpID, EmployeeNo: e.EmployeeNo, Name: e.Name, Department: e.Department})
	}
	return &v1.SearchEmployeesRes{List: out}, nil
}

// AddRelation 按姓名批量新增评价关系。
func (c *ControllerV1) AddRelation(ctx context.Context, req *v1.AddRelationReq) (res *v1.AddRelationRes, err error) {
	items := make([]rsvc.AddRelationItem, 0, len(req.Items))
	for _, it := range req.Items {
		if it == nil {
			continue
		}
		items = append(items, rsvc.AddRelationItem{
			EvaluateeName:       it.EvaluateeName,
			EvaluateeJobTitle:   it.EvaluateeJobTitle,
			EvaluateeDepartment: it.EvaluateeDepartment,
			EvaluatorName:       it.EvaluatorName,
			RoleName:            it.RoleName,
		})
	}
	ids, err := c.relationSvc.AddRelations(ctx, rsvc.AddRelationsInput{
		QuestionnaireID: req.QuestionnaireId,
		Items:           items,
	})
	if err != nil {
		return nil, err
	}
	return &v1.AddRelationRes{Ids: ids}, nil
}

// UpdateRelationRole 改派评价关系角色。
func (c *ControllerV1) UpdateRelationRole(ctx context.Context, req *v1.UpdateRelationRoleReq) (res *v1.UpdateRelationRoleRes, err error) {
	if err = c.relationSvc.UpdateRelationRole(ctx, req.Id, req.RoleId); err != nil {
		return nil, err
	}
	return &v1.UpdateRelationRoleRes{}, nil
}

// DeleteRelation 删除评价关系。
func (c *ControllerV1) DeleteRelation(ctx context.Context, req *v1.DeleteRelationReq) (res *v1.DeleteRelationRes, err error) {
	if err = c.relationSvc.DeleteRelation(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteRelationRes{}, nil
}

// DeleteEvaluatee 删除被评估人及其全部评价关系。
func (c *ControllerV1) DeleteEvaluatee(ctx context.Context, req *v1.DeleteEvaluateeReq) (res *v1.DeleteEvaluateeRes, err error) {
	if err = c.relationSvc.DeleteEvaluatee(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.DeleteEvaluateeRes{}, nil
}

// toRelationMatrixDTO 把 service 层 RelationMatrix 转为 API 层 DTO（同构）。
func toRelationMatrixDTO(m *rsvc.RelationMatrix) *v1.RelationMatrix {
	if m == nil {
		return nil
	}
	out := &v1.RelationMatrix{QuestionnaireID: m.QuestionnaireID, Roles: toRoleItemDTOs(m.Roles)}
	for _, e := range m.Evaluatees {
		row := &v1.EvaluateeRow{
			EvaluateeID:         e.EvaluateeID,
			EvaluateeEmpID:      e.EvaluateeEmpID,
			EvaluateeName:       e.EvaluateeName,
			EvaluateeNo:         e.EvaluateeNo,
			EvaluateeJobTitle:   e.EvaluateeJobTitle,
			EvaluateeDepartment: e.EvaluateeDepartment,
			Groups:              make([]*v1.RoleGroup, 0, len(e.Groups)),
		}
		for _, g := range e.Groups {
			rg := &v1.RoleGroup{RoleID: g.RoleID, RoleName: g.RoleName, Items: make([]*v1.RelationItem, 0, len(g.Items))}
			for _, it := range g.Items {
				rg.Items = append(rg.Items, &v1.RelationItem{
					ID: it.ID, EvaluateeID: it.EvaluateeID, EvaluatorEmpID: it.EvaluatorEmpID,
					EvaluatorName: it.EvaluatorName, EvaluatorNo: it.EvaluatorNo,
					RoleID: it.RoleID, RoleName: it.RoleName, Source: it.Source,
					Completed: it.Completed, DerivedStatus: it.DerivedStatus,
				})
			}
			row.Groups = append(row.Groups, rg)
		}
		out.Evaluatees = append(out.Evaluatees, row)
	}
	return out
}

// toRoleItemDTOs 把 service 层 RoleItem 切片转为 API 层 DTO。
func toRoleItemDTOs(items []*rsvc.RoleItem) []*v1.RoleItem {
	out := make([]*v1.RoleItem, 0, len(items))
	for _, r := range items {
		out = append(out, &v1.RoleItem{Id: r.Id, Name: r.Name, Weight: r.Weight})
	}
	return out
}
