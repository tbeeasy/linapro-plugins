// employee_batch.go 实现按集合的批量只读查询与 empcap 读模型装配。
//
// 批量契约的存在意义是消除消费方的 N+1：每次调用只做一次员工集合查询 +
// 一次部门集合查询，数据库访问次数恒定，不随输入规模或返回行数增长。
package employee

import (
	"context"
	"time"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// MapByEmployeeNos 按工号集合批量只读查询：一次查询返回「工号 → 员工」映射，
// 供消费方把一批工号一次性解析为员工，避免在循环内逐条调 GetByEmployeeNo 的 N+1。
// 仅在传入的 tenantID 范围内匹配（工号在租户内唯一，与 GetByEmployeeNo 的
// (tenantID, employeeNo) 语义一致）。空集合直接返回空 map；未命中的工号不入 map。
// 不排除离职员工——在职判定由消费方按 Status 自行决定。
func (s *Service) MapByEmployeeNos(ctx context.Context, tenantID int, employeeNos []string) (map[string]*empcap.Employee, error) {
	if len(employeeNos) == 0 {
		return map[string]*empcap.Employee{}, nil
	}
	var rows []*entity.Employee
	if err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID}).
		WhereIn(dao.Employee.Columns().MokaEmployeeNo, employeeNos).
		Scan(&rows); err != nil {
		return nil, err
	}
	return s.mapByEmployeeNo(ctx, rows)
}

// MapByEmpIDs 按 EmpID 集合批量只读查询：一次查询返回「EmpID → 员工」映射，
// 供消费方把一批 EmpID 一次性解析为员工（姓名/工号），避免逐条调 GetByID 的 N+1。
// 不带租户过滤——EmpID 是全局代理主键，跨租户唯一；与 GetByID 行为对齐。
// 空集合直接返回空 map；未命中的 EmpID 不入 map。不排除离职员工。
func (s *Service) MapByEmpIDs(ctx context.Context, empIDs []int64) (map[int64]*empcap.Employee, error) {
	if len(empIDs) == 0 {
		return map[int64]*empcap.Employee{}, nil
	}
	var rows []*entity.Employee
	if err := dao.Employee.Ctx(ctx).
		WhereIn(dao.Employee.Columns().Id, empIDs).
		Scan(&rows); err != nil {
		return nil, err
	}
	emps, err := s.toEmpcapBatch(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*empcap.Employee, len(emps))
	for _, emp := range emps {
		out[emp.EmpID] = emp
	}
	return out, nil
}

// MapByEmployeeNosAnyTenant 是 MapByEmployeeNos 的跨租户批量版本：一次 WhereIn
// 返回「工号 → 员工」映射，不按 tenantID 过滤（Moka 工号全公司唯一）。空集合
// 直接返回空 map；未命中的工号不入 map；不排除离职员工。
func (s *Service) MapByEmployeeNosAnyTenant(ctx context.Context, employeeNos []string) (map[string]*empcap.Employee, error) {
	if len(employeeNos) == 0 {
		return map[string]*empcap.Employee{}, nil
	}
	var rows []*entity.Employee
	if err := dao.Employee.Ctx(ctx).
		WhereIn(dao.Employee.Columns().MokaEmployeeNo, employeeNos).
		Scan(&rows); err != nil {
		return nil, err
	}
	return s.mapByEmployeeNo(ctx, rows)
}

// MapByNamesAnyTenant 按姓名集合跨租户一次 WhereIn 查询，返回「姓名 → 同名全部员工」映射，
// 每组按 EmpID 升序（部门经 toEmpcapBatch 一次集合查询装配）。空集合直接返回空 map；
// 未命中的姓名不入 map；不排除离职员工，重名与在职判定由消费方完成。
func (s *Service) MapByNamesAnyTenant(ctx context.Context, names []string) (map[string][]*empcap.Employee, error) {
	if len(names) == 0 {
		return map[string][]*empcap.Employee{}, nil
	}
	var rows []*entity.Employee
	if err := dao.Employee.Ctx(ctx).
		WhereIn(dao.Employee.Columns().Name, names).
		OrderAsc(dao.Employee.Columns().Id).
		Scan(&rows); err != nil {
		return nil, err
	}
	emps, err := s.toEmpcapBatch(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]*empcap.Employee, len(names))
	for _, emp := range emps {
		out[emp.Name] = append(out[emp.Name], emp)
	}
	return out, nil
}

// mapByEmployeeNo 把员工行按工号装配为「工号 → 员工」映射：空工号行跳过，
// 同一工号出现多行时保留首行，部门经 toEmpcapBatch 一次批量装配。
func (s *Service) mapByEmployeeNo(ctx context.Context, rows []*entity.Employee) (map[string]*empcap.Employee, error) {
	seen := make(map[string]struct{}, len(rows))
	picked := make([]*entity.Employee, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.MokaEmployeeNo == "" {
			continue
		}
		if _, dup := seen[row.MokaEmployeeNo]; dup {
			continue
		}
		seen[row.MokaEmployeeNo] = struct{}{}
		picked = append(picked, row)
	}
	emps, err := s.toEmpcapBatch(ctx, picked)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*empcap.Employee, len(emps))
	for _, emp := range emps {
		out[emp.EmployeeNo] = emp
	}
	return out, nil
}

// toEmpcap 把单个员工行转换为 empcap 读模型（含部门）。
func (s *Service) toEmpcap(ctx context.Context, e *entity.Employee) (*empcap.Employee, error) {
	emps, err := s.toEmpcapBatch(ctx, []*entity.Employee{e})
	if err != nil {
		return nil, err
	}
	return emps[0], nil
}

// toEmpcapBatch 把一批员工行转换为 empcap 读模型：部门行按 emp_id 集合一次
// WhereIn 查询后在内存中分组，数据库访问次数恒为 1，不随行数增长（避免 N+1）。
// 输出顺序与输入一致；nil 行与重复 EmpID 被跳过。
func (s *Service) toEmpcapBatch(ctx context.Context, rows []*entity.Employee) ([]*empcap.Employee, error) {
	empIDs := make([]int64, 0, len(rows))
	unique := make([]*entity.Employee, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, dup := seen[row.Id]; dup {
			continue
		}
		seen[row.Id] = struct{}{}
		empIDs = append(empIDs, row.Id)
		unique = append(unique, row)
	}
	if len(unique) == 0 {
		return []*empcap.Employee{}, nil
	}

	var depts []*entity.EmployeeDept
	if err := dao.EmployeeDept.Ctx(ctx).
		WhereIn(dao.EmployeeDept.Columns().EmpId, empIDs).
		OrderAsc(dao.EmployeeDept.Columns().Id).
		Scan(&depts); err != nil {
		return nil, err
	}
	deptsByEmp := make(map[int64][]empcap.Department, len(unique))
	for _, d := range depts {
		deptsByEmp[d.EmpId] = append(deptsByEmp[d.EmpId], empcap.Department{
			LarkDepartmentID: d.LarkDepartmentId,
			IsPrimary:        d.IsPrimary == 1,
		})
	}

	out := make([]*empcap.Employee, 0, len(unique))
	for _, e := range unique {
		out = append(out, &empcap.Employee{
			EmpID:            e.Id,
			TenantID:         e.TenantId,
			EmployeeNo:       e.MokaEmployeeNo,
			Name:             e.Name,
			Status:           e.Status,
			Departments:      deptsByEmp[e.Id],
			OnBoardingDate:   formatDate(e.OnBoardingDate),
			EndProbationDate: formatDate(e.EndProbationDate),
			LeaveDate:        formatDate(e.LeaveDate),
		})
	}
	return out, nil
}

// formatDate 把库中的可空日期列转为契约字符串：NULL 输出空字符串。
func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(dateLayout)
}

// boolToInt 把布尔位转成库中的 SMALLINT 位（1/0）。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
