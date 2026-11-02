// emp_v1_me.go 实现受 EmpAuth 保护的当前员工查询：它从 EmpAuth 注入的
// EmpContext 读取身份，并返回按 EmpID 解析的实时身份快照（含部门）。

package emp

import (
	"context"

	v1 "lina-plugin-linapro-employee-core/backend/api/emp/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// Me 从 EmpContext 返回当前员工身份。
func (c *MeController) Me(ctx context.Context, _ *v1.MeReq) (*v1.MeRes, error) {
	empCtx, ok := empcap.EmpFromContext(ctx)
	if !ok {
		return nil, empcap.ErrUnauthorized
	}
	emp, err := c.employees.GetByID(ctx, empCtx.EmpID)
	if err != nil {
		return nil, err
	}
	deptIDs := make([]string, 0, len(emp.Departments))
	for _, d := range emp.Departments {
		deptIDs = append(deptIDs, d.LarkDepartmentID)
	}
	return &v1.MeRes{
		EmpId:          emp.EmpID,
		TenantId:       emp.TenantID,
		Name:           emp.Name,
		MokaEmployeeNo: emp.EmployeeNo,
		Status:         emp.Status,
		DepartmentIds:  deptIDs,
	}, nil
}
