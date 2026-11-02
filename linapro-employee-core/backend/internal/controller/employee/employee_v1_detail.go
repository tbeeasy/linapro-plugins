// employee_v1_detail.go 实现后台员工详情：基本信息 + 关联后台账号 + 部门 + 全部飞书身份。
package employee

import (
	"context"

	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
)

// Detail 返回一名员工的完整资料。
func (c *ControllerV1) Detail(ctx context.Context, req *v1.DetailReq) (res *v1.DetailRes, err error) {
	detail, err := c.employees.GetDetail(ctx, req.EmpId)
	if err != nil {
		return nil, err
	}

	res = &v1.DetailRes{
		EmpId:          detail.EmpID,
		Name:           detail.Name,
		MokaEmployeeNo: detail.EmployeeNo,
		TenantId:       detail.TenantID,
		TenantName:     c.tenantNames(ctx, []int{detail.TenantID})[detail.TenantID],
		Status:         detail.Status,
		SysUserId:      detail.SysUserID,
		CreatedAt:      detail.CreatedAt,
		UpdatedAt:      detail.UpdatedAt,
		DepartmentIds:  make([]string, 0, len(detail.Departments)),
		LarkIdentities: make([]*v1.LarkIdentityItem, 0, len(detail.Identities)),
	}
	for _, d := range detail.Departments {
		res.DepartmentIds = append(res.DepartmentIds, d.LarkDepartmentID)
	}

	appNames := c.larkAppNames(ctx)
	for _, ident := range detail.Identities {
		res.LarkIdentities = append(res.LarkIdentities, &v1.LarkIdentityItem{
			LarkAppId:   ident.LarkAppID,
			LarkAppName: appNames[ident.LarkAppID],
			OpenId:      ident.OpenID,
			UserId:      ident.UserID,
			IdSource:    ident.IDSource,
			CreatedAt:   ident.CreatedAt,
		})
	}

	// 关联后台账号的名称是展示用附加信息：账号已删除或用户能力不可用时留空，
	// 不因它取不到而让详情整体失败。
	if name, nickname := c.sysUserName(ctx, detail.SysUserID); true {
		res.SysUserName = name
		res.SysUserNickname = nickname
	}
	return res, nil
}
