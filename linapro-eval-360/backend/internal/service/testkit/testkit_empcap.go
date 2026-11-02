// testkit_empcap.go 提供一个基于真实数据库表的 empcap.Service 测试实现。
//
// 为什么需要它：linapro-eval-360 经 empcap 消费员工能力（工号解析、姓名回显），
// 而 employee-core 的实现位于其 internal 目录、无法跨模块 import。集成测试因此
// 直连员工表（与生产同一张表、同一 DDL、同一 (tenant_id, moka_employee_no) 语义），
// 只实现 eval-360 真正调用的读/写路径，其余方法显式返回「未实现」以避免静默假通过。
package testkit

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	empcap "lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// EmployeeService 是 empcap.Service 的测试实现（仅覆盖 eval-360 用到的能力）。
type EmployeeService struct{}

// 编译期断言：满足 empcap.Service 契约。
var _ empcap.Service = (*EmployeeService)(nil)

// NewEmployeeService 返回基于真实员工表的测试实现。
func NewEmployeeService() *EmployeeService { return &EmployeeService{} }

// employeeRow 是测试读模型。
type employeeRow struct {
	Id             int64  `orm:"id"`
	TenantId       int    `orm:"tenant_id"`
	Name           string `orm:"name"`
	MokaEmployeeNo string `orm:"moka_employee_no"`
	Status         int    `orm:"status"`
}

// UpsertEmployee 按 (tenant_id, moka_employee_no) 覆盖或创建员工行。
func (s *EmployeeService) UpsertEmployee(ctx context.Context, tenantID int, profile empcap.Profile) (int64, error) {
	if profile.EmployeeNo == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	var existing *employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("moka_employee_no", profile.EmployeeNo).
		WhereNull("deleted_at").
		Scan(&existing); err != nil {
		return 0, err
	}
	if existing != nil {
		if _, err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
			Where("id", existing.Id).
			Data(g.Map{"name": profile.Name}).Update(); err != nil {
			return 0, err
		}
		return existing.Id, nil
	}
	id, err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).Data(g.Map{
		"tenant_id":        tenantID,
		"name":             profile.Name,
		"moka_employee_no": profile.EmployeeNo,
		"status":           empcap.StatusActive,
		"created_at":       gtime.Now(),
		"updated_at":       gtime.Now(),
	}).InsertAndGetId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// MarkDeparted 把员工置为离职。
func (s *EmployeeService) MarkDeparted(ctx context.Context, tenantID int, empID int64) error {
	_, err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).Where("id", empID).
		Data(g.Map{"status": empcap.StatusDeparted}).Update()
	return err
}

// GetByID 按内部 EmpID 返回员工。
func (s *EmployeeService) GetByID(ctx context.Context, empID int64) (*empcap.Employee, error) {
	var row *employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("id", empID).WhereNull("deleted_at").Scan(&row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return toEmployee(row), nil
}

// GetByEmployeeNo 按 (tenant_id, 工号) 返回员工（导入路径的关键查询）。
func (s *EmployeeService) GetByEmployeeNo(ctx context.Context, tenantID int, employeeNo string) (*empcap.Employee, error) {
	if employeeNo == "" {
		return nil, empcap.ErrEmployeeNotFound
	}
	var row *employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).
		Where("moka_employee_no", employeeNo).
		WhereNull("deleted_at").
		Scan(&row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return toEmployee(row), nil
}

// GetByEmployeeNoAnyTenant 按工号跨租户返回员工。
func (s *EmployeeService) GetByEmployeeNoAnyTenant(ctx context.Context, employeeNo string) (*empcap.Employee, error) {
	var row *employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("moka_employee_no", employeeNo).
		WhereNull("deleted_at").
		Scan(&row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return toEmployee(row), nil
}

// GetByName 按 (tenant_id, 姓名) 返回员工。
func (s *EmployeeService) GetByName(ctx context.Context, tenantID int, name string) (*empcap.Employee, error) {
	var row *employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).Where("name", name).
		WhereNull("deleted_at").Scan(&row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return toEmployee(row), nil
}

// ListActiveByTenant 返回租户内在职员工。
func (s *EmployeeService) ListActiveByTenant(ctx context.Context, tenantID int) ([]*empcap.Employee, error) {
	var rows []*employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).Where("status", empcap.StatusActive).
		WhereNull("deleted_at").Scan(&rows); err != nil {
		return nil, err
	}
	out := make([]*empcap.Employee, 0, len(rows))
	for _, r := range rows {
		out = append(out, toEmployee(r))
	}
	return out, nil
}

// ResolveEmpIDByLarkIdentity 飞书身份反查（eval-360 不用）。
func (s *EmployeeService) ResolveEmpIDByLarkIdentity(_ context.Context, _, _ string) (int64, error) {
	return 0, errNotImplemented("ResolveEmpIDByLarkIdentity")
}

// ResolveEmpIDByLarkUserID 飞书 user_id 反查（eval-360 不用）。
func (s *EmployeeService) ResolveEmpIDByLarkUserID(_ context.Context, _, _ string) (int64, error) {
	return 0, errNotImplemented("ResolveEmpIDByLarkUserID")
}

// UpsertLarkIdentity 写入飞书身份映射（eval-360 不用）。
func (s *EmployeeService) UpsertLarkIdentity(_ context.Context, _, _, _, _ string, _ int64) error {
	return errNotImplemented("UpsertLarkIdentity")
}

// MapLarkOpenIDsByEmployeeNo 多维表格人员字段寻址（eval-360 不用）。
func (s *EmployeeService) MapLarkOpenIDsByEmployeeNo(_ context.Context, _ string) (map[string][]string, error) {
	return nil, errNotImplemented("MapLarkOpenIDsByEmployeeNo")
}

// MapOwnLarkOpenIDsByEmployeeNo 出站推送寻址（推送不在本次集成测试范围）。
func (s *EmployeeService) MapOwnLarkOpenIDsByEmployeeNo(_ context.Context, _ string) (map[string]string, error) {
	return nil, errNotImplemented("MapOwnLarkOpenIDsByEmployeeNo")
}

// MapOwnLarkUserIDsByEmployeeNo 出站推送寻址的 user_id 版本（推送不在本次集成测试范围）。
func (s *EmployeeService) MapOwnLarkUserIDsByEmployeeNo(_ context.Context, _ string) (empcap.LarkUserIDIndex, error) {
	return empcap.LarkUserIDIndex{}, errNotImplemented("MapOwnLarkUserIDsByEmployeeNo")
}

// MapByEmployeeNos 批量按工号查询（导入路径用）：一次 WhereIn 解析全部工号。
func (s *EmployeeService) MapByEmployeeNos(ctx context.Context, tenantID int, employeeNos []string) (map[string]*empcap.Employee, error) {
	if len(employeeNos) == 0 {
		return map[string]*empcap.Employee{}, nil
	}
	var rows []*employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		Where("tenant_id", tenantID).
		WhereIn("moka_employee_no", employeeNos).
		WhereNull("deleted_at").
		Scan(&rows); err != nil {
		return nil, err
	}
	out := make(map[string]*empcap.Employee, len(rows))
	for _, r := range rows {
		if r == nil || r.MokaEmployeeNo == "" {
			continue
		}
		if _, dup := out[r.MokaEmployeeNo]; dup {
			continue
		}
		out[r.MokaEmployeeNo] = toEmployee(r)
	}
	return out, nil
}

// MapByEmployeeNosAnyTenant 跨租户批量按工号查询（导入路径用）：一次 WhereIn，不带租户过滤。
func (s *EmployeeService) MapByEmployeeNosAnyTenant(ctx context.Context, employeeNos []string) (map[string]*empcap.Employee, error) {
	if len(employeeNos) == 0 {
		return map[string]*empcap.Employee{}, nil
	}
	var rows []*employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		WhereIn("moka_employee_no", employeeNos).
		WhereNull("deleted_at").
		Scan(&rows); err != nil {
		return nil, err
	}
	out := make(map[string]*empcap.Employee, len(rows))
	for _, r := range rows {
		if r == nil || r.MokaEmployeeNo == "" {
			continue
		}
		if _, dup := out[r.MokaEmployeeNo]; dup {
			continue
		}
		out[r.MokaEmployeeNo] = toEmployee(r)
	}
	return out, nil
}

// MapByNamesAnyTenant 跨租户按姓名批量查询（按姓名录入路径用）：一次 WhereIn，按 id 升序分组返回全部同名行。
func (s *EmployeeService) MapByNamesAnyTenant(ctx context.Context, names []string) (map[string][]*empcap.Employee, error) {
	if len(names) == 0 {
		return map[string][]*empcap.Employee{}, nil
	}
	var rows []*employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		WhereIn("name", names).
		WhereNull("deleted_at").
		OrderAsc("id").
		Scan(&rows); err != nil {
		return nil, err
	}
	out := make(map[string][]*empcap.Employee, len(names))
	for _, r := range rows {
		if r == nil {
			continue
		}
		out[r.Name] = append(out[r.Name], toEmployee(r))
	}
	return out, nil
}

// MapByEmpIDs 批量按 EmpID 查询（列表/导出回显姓名用）：一次 WhereIn 解析全部 EmpID。
func (s *EmployeeService) MapByEmpIDs(ctx context.Context, empIDs []int64) (map[int64]*empcap.Employee, error) {
	if len(empIDs) == 0 {
		return map[int64]*empcap.Employee{}, nil
	}
	var rows []*employeeRow
	if err := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
		WhereIn("id", empIDs).
		WhereNull("deleted_at").
		Scan(&rows); err != nil {
		return nil, err
	}
	out := make(map[int64]*empcap.Employee, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if _, dup := out[r.Id]; dup {
			continue
		}
		out[r.Id] = toEmployee(r)
	}
	return out, nil
}

// toEmployee 把测试行转为 empcap 读模型（部门不在 eval-360 断言范围）。
func toEmployee(row *employeeRow) *empcap.Employee {
	return &empcap.Employee{
		EmpID:      row.Id,
		TenantID:   row.TenantId,
		EmployeeNo: row.MokaEmployeeNo,
		Name:       row.Name,
		Status:     row.Status,
	}
}

// errNotImplemented 显式标记测试实现未覆盖的能力，避免静默返回零值造成假通过。
func errNotImplemented(method string) error {
	return fmt.Errorf("testkit.EmployeeService.%s 未实现（eval-360 集成测试未覆盖该能力）", method)
}
