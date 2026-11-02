// employee_v1_list.go 实现后台员工分页列表。
package employee

import (
	"context"

	"lina-core/pkg/plugin/capability/tenantcap"
	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
	"lina-plugin-linapro-employee-core/backend/internal/service/employee"
)

// List 分页查询员工花名册。租户名按当前页租户集合一次批量补齐。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (res *v1.ListRes, err error) {
	page, err := c.employees.ListPage(ctx, employee.ListFilter{
		PageNum:   req.PageNum,
		PageSize:  req.PageSize,
		Keyword:   req.Keyword,
		TenantID:  req.TenantId,
		Status:    req.Status,
		LarkBound: req.LarkBound,
	})
	if err != nil {
		return nil, err
	}

	tenantIDs := make([]int, 0, len(page.Rows))
	for _, row := range page.Rows {
		tenantIDs = append(tenantIDs, row.TenantID)
	}
	names := c.tenantNames(ctx, tenantIDs)

	res = &v1.ListRes{List: make([]*v1.EmployeeItem, 0, len(page.Rows)), Total: page.Total}
	for _, row := range page.Rows {
		appIDs := make([]string, 0, len(row.Identities))
		seen := make(map[string]struct{}, len(row.Identities))
		for _, ident := range row.Identities {
			// 同一应用下同一员工理论上只有一条 own 身份，这里按 app 去重，
			// 避免异常数据让前端把同一个应用显示两次。
			if _, dup := seen[ident.LarkAppID]; dup {
				continue
			}
			seen[ident.LarkAppID] = struct{}{}
			appIDs = append(appIDs, ident.LarkAppID)
		}
		res.List = append(res.List, &v1.EmployeeItem{
			EmpId:          row.EmpID,
			Name:           row.Name,
			MokaEmployeeNo: row.EmployeeNo,
			TenantId:       row.TenantID,
			TenantName:     names[row.TenantID],
			Status:         row.Status,
			LarkBound:      len(appIDs) > 0,
			LarkAppIds:     appIDs,
			UpdatedAt:      row.UpdatedAt,
		})
	}
	return res, nil
}

// tenantNames 按租户 id 集合一次批量取租户名。宿主租户能力不可用、或某个
// 租户已不存在时，对应名称留空（前端回退显示租户 id），不因补名字失败而让
// 整个列表报错。
func (c *ControllerV1) tenantNames(ctx context.Context, tenantIDs []int) map[int]string {
	out := make(map[int]string, len(tenantIDs))
	if c.tenants == nil || len(tenantIDs) == 0 {
		return out
	}
	// 去重：一页内多名员工常属同一租户。
	unique := make([]tenantcap.TenantID, 0, len(tenantIDs))
	seen := make(map[int]struct{}, len(tenantIDs))
	for _, id := range tenantIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, tenantcap.TenantID(id))
	}
	res, err := c.tenants.Directory().BatchGet(ctx, unique)
	if err != nil || res == nil {
		// 补名字属附加信息，失败只降级不阻断：调用方拿到的名称留空。
		return out
	}
	for _, info := range res.Items {
		if info != nil {
			out[int(info.ID)] = info.Name
		}
	}
	return out
}
