// employee_query.go 实现按单键读取员工：EmpID、租户内工号、跨租户工号、租户内姓名，
// 以及同步任务使用的租户内在职列表。
//
// 单行读取统一经 toEmpcap 装配部门；需要批量时改用 employee_batch.go 的
// MapBy* 系列，避免在循环里逐条调用本文件的单条方法造成 N+1。
package employee

import (
	"context"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// GetByID 按内部 EmpID 返回一名员工（含部门）。
func (s *Service) GetByID(ctx context.Context, empID int64) (*empcap.Employee, error) {
	var e *entity.Employee
	if err := dao.Employee.Ctx(ctx).Where(do.Employee{Id: empID}).Scan(&e); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return s.toEmpcap(ctx, e)
}

// GetByEmployeeNo 按 (tenantID, Moka 员工工号) 返回一名员工。
// 无存活行匹配时返回 ErrEmployeeNotFound。
func (s *Service) GetByEmployeeNo(ctx context.Context, tenantID int, employeeNo string) (*empcap.Employee, error) {
	e, err := s.findLiveByEmployeeNo(ctx, tenantID, employeeNo)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return s.toEmpcap(ctx, e)
}

// GetByEmployeeNoAnyTenant 按 Moka 工号跨租户返回一名员工。Moka 工号全公司
// 唯一，故无需租户即可定位。无存活行匹配时返回 ErrEmployeeNotFound。
func (s *Service) GetByEmployeeNoAnyTenant(ctx context.Context, employeeNo string) (*empcap.Employee, error) {
	var e *entity.Employee
	if err := dao.Employee.Ctx(ctx).
		Where(do.Employee{MokaEmployeeNo: employeeNo}).
		Scan(&e); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return s.toEmpcap(ctx, e)
}

// GetByName 按 (tenantID, 姓名) 返回一名员工。无匹配时返回 ErrEmployeeNotFound。
//
// 姓名在租户内并不唯一（同名员工合法存在，见 idx_..._emp_name 的非唯一索引），
// 因此同名时按 EmpID 升序取第一行以保证结果稳定。需要区分重名的调用方必须
// 改用 MapByNamesAnyTenant 并自行判重，不得依赖本方法返回「唯一那个人」。
func (s *Service) GetByName(ctx context.Context, tenantID int, name string) (*empcap.Employee, error) {
	var e *entity.Employee
	if err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, Name: name}).
		OrderAsc(dao.Employee.Columns().Id).
		Scan(&e); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	return s.toEmpcap(ctx, e)
}

// ListActiveByTenant 返回某租户下所有在职员工。供同步任务的
// 离职对账阶段查找过期的在职行，而无需在 sync 包内直接调用 DAO。
func (s *Service) ListActiveByTenant(ctx context.Context, tenantID int) ([]*empcap.Employee, error) {
	var rows []*entity.Employee
	if err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, Status: empcap.StatusActive}).
		Scan(&rows); err != nil {
		return nil, err
	}
	return s.toEmpcapBatch(ctx, rows)
}

// findLiveByEmployeeNo 是写入网关与租户内查询共用的按工号查找，
// 返回 nil 表示无存活行（不做错误包装，由调用方决定语义）。
func (s *Service) findLiveByEmployeeNo(ctx context.Context, tenantID int, employeeNo string) (*entity.Employee, error) {
	var e *entity.Employee
	err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, MokaEmployeeNo: employeeNo}).
		Scan(&e)
	return e, err
}
