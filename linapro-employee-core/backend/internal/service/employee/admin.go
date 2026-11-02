// admin.go 实现后台「通讯录管理 → 员工列表」所需的读取与有限写入：
// 分页列表、详情、飞书身份的手工绑定/解绑、关联后台账号。
//
// 这些方法只服务于本插件的后台 HTTP 接口，不经 empcap 暴露——empcap 面向的是
// 其它插件的进程内消费，而这里是管理员按权限点访问的全量花名册。
//
// 刻意不提供新增与删除员工：员工行由 Moka 全量同步写入与维护（见 design D25），
// 手工新增没有 Moka 工号锚点、手工删除下一轮又会被同步回来。可写的只有同步
// 从不触碰的两类字段——飞书身份映射与 sys_user_id。

package employee

import (
	"context"
	"strings"
	"time"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// 列表分页的默认值与上限。
const (
	defaultPageSize = 10
	maxPageSize     = 100
)

// ListFilter 是后台员工列表的查询条件。零值表示不过滤。
type ListFilter struct {
	PageNum  int
	PageSize int
	// Keyword 按姓名或工号模糊匹配。
	Keyword string
	// TenantID 大于 0 时只返回该租户的员工。
	TenantID int
	// Status 非 nil 时按在职状态过滤（1=在职 0=离职）。
	Status *int
	// LarkBound 非 nil 时按是否有 own 飞书身份过滤。
	LarkBound *bool
}

// AdminRow 是列表行的读模型。飞书身份只含 own，供列表展示绑定状态。
type AdminRow struct {
	EmpID      int64
	TenantID   int
	Name       string
	EmployeeNo string
	Status     int
	SysUserID  int
	CreatedAt  *int64
	UpdatedAt  *int64
	// Identities 只含 id_source='own' 的身份；列表不展示 assoc。
	Identities []LarkIdentityView
}

// AdminPage 是一页员工列表。
type AdminPage struct {
	Rows  []*AdminRow
	Total int
}

// AdminDetail 是详情读模型：列表字段 + 全部身份（含 assoc）+ 部门。
type AdminDetail struct {
	AdminRow
	// Identities 含该员工名下全部身份（own 与 assoc），按应用名排序。
	Identities []LarkIdentityView
	// Departments 是飞书原始部门归属；部门数据当前预留，通常为空。
	Departments []empcap.Department
}

// LarkIdentityView 是一条飞书身份映射的展示投影。
type LarkIdentityView struct {
	LarkAppID string
	OpenID    string
	UserID    string
	IDSource  string
	CreatedAt *int64
}

// ListPage 分页查询员工。过滤与分页在数据库完成；当前页的 own 身份一次
// WhereIn 批量装配，数据库访问次数不随页大小增长。
func (s *Service) ListPage(ctx context.Context, f ListFilter) (*AdminPage, error) {
	cols := dao.Employee.Columns()
	pageNum, pageSize := normalizePaging(f.PageNum, f.PageSize)

	model := dao.Employee.Ctx(ctx)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		// 姓名与工号任一命中即算匹配，括进一个分组避免与其它条件串成错误的 AND 优先级。
		model = model.Where(
			model.Builder().WhereLike(cols.Name, like).WhereOrLike(cols.MokaEmployeeNo, like),
		)
	}
	if f.TenantID > 0 {
		model = model.Where(cols.TenantId, f.TenantID)
	}
	if f.Status != nil {
		model = model.Where(cols.Status, *f.Status)
	}
	if f.LarkBound != nil {
		// EXISTS 子查询把「是否绑定飞书」下推到数据库，避免先取全量再在内存里过滤。
		// 只计 own 身份——与登录口径一致，只有 own 身份能真正用来登录。
		exists := "EXISTS (SELECT 1 FROM " + dao.LarkIdentity.Table() + " li WHERE li." +
			dao.LarkIdentity.Columns().EmpId + " = " + dao.Employee.Table() + "." + cols.Id +
			" AND li." + dao.LarkIdentity.Columns().IdSource + " = ?)"
		if *f.LarkBound {
			model = model.Where(exists, empcap.IDSourceOwn)
		} else {
			model = model.Where("NOT "+exists, empcap.IDSourceOwn)
		}
	}

	total, err := model.Count()
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return &AdminPage{Rows: []*AdminRow{}, Total: 0}, nil
	}

	var rows []*entity.Employee
	if err = model.Page(pageNum, pageSize).OrderDesc(cols.Id).Scan(&rows); err != nil {
		return nil, err
	}
	out, err := s.adminRows(ctx, rows, empcap.IDSourceOwn)
	if err != nil {
		return nil, err
	}
	return &AdminPage{Rows: out, Total: total}, nil
}

// GetDetail 返回一名员工的详情（含全部飞书身份与部门）。
// 员工不存在时返回 ErrEmployeeNotFound。
func (s *Service) GetDetail(ctx context.Context, empID int64) (*AdminDetail, error) {
	var e *entity.Employee
	if err := dao.Employee.Ctx(ctx).Where(do.Employee{Id: empID}).Scan(&e); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, empcap.ErrEmployeeNotFound
	}
	rows, err := s.adminRows(ctx, []*entity.Employee{e}, "")
	if err != nil {
		return nil, err
	}
	detail := &AdminDetail{AdminRow: *rows[0], Identities: rows[0].Identities}

	var depts []*entity.EmployeeDept
	if err = dao.EmployeeDept.Ctx(ctx).
		Where(do.EmployeeDept{EmpId: empID}).
		OrderAsc(dao.EmployeeDept.Columns().Id).
		Scan(&depts); err != nil {
		return nil, err
	}
	for _, d := range depts {
		detail.Departments = append(detail.Departments, empcap.Department{
			LarkDepartmentID: d.LarkDepartmentId,
			IsPrimary:        d.IsPrimary == 1,
		})
	}
	return detail, nil
}

// BindLarkIdentity 手工绑定一条 own 飞书身份。
//
// 绑定前由调用方（控制器）确认该 open_id 在目标应用通讯录中确实存在并取回
// userID；本方法只负责归属判定与落库。归属规则：
//   - 该 (appID, openID) 不存在 → 插入，id_source='own'；
//   - 已属于该员工 → 幂等成功，顺带刷新 user_id；
//   - 已属于其他员工 → ErrLarkIdentityTaken，绝不静默抢占。
//
// 注意同步回填对同一主键做 upsert、以工号匹配为准（D13）：若之后该飞书成员
// 的工号能匹配到别的员工，同步会改写这行的归属。
func (s *Service) BindLarkIdentity(ctx context.Context, empID int64, larkAppID, openID, userID string) error {
	if larkAppID == "" || openID == "" {
		return empcap.ErrInvalidParameter
	}
	if _, err := s.GetDetail(ctx, empID); err != nil {
		return err
	}
	return s.bindIdentity(ctx, empID, larkAppID, openID, userID)
}

// bindIdentity 是 BindLarkIdentity 的落库部分，抽出来便于复用「先查归属再写」的逻辑。
func (s *Service) bindIdentity(ctx context.Context, empID int64, larkAppID, openID, userID string) error {
	var existing *entity.LarkIdentity
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: larkAppID, LarkOpenId: openID}).
		Scan(&existing); err != nil {
		return err
	}
	if existing != nil && existing.EmpId != empID {
		return empcap.ErrLarkIdentityTaken
	}
	cols := dao.LarkIdentity.Columns()
	if _, err := dao.LarkIdentity.Ctx(ctx).
		Data(do.LarkIdentity{
			LarkAppId:  larkAppID,
			LarkOpenId: openID,
			LarkUserId: userID,
			IdSource:   empcap.IDSourceOwn,
			EmpId:      empID,
		}).
		OnConflict(cols.LarkAppId, cols.LarkOpenId).
		Save(); err != nil {
		return err
	}
	return nil
}

// UnbindLarkIdentity 解绑该员工名下的一条飞书身份（own 或 assoc 均可）。
// 只删除 emp_id 匹配的行：传入不属于该员工的身份返回 ErrLarkIdentityNotFound，
// 不会误删他人的映射。若该身份仍能按工号匹配，下一轮同步会重新绑定。
func (s *Service) UnbindLarkIdentity(ctx context.Context, empID int64, larkAppID, openID string) error {
	if larkAppID == "" || openID == "" {
		return empcap.ErrInvalidParameter
	}
	res, err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{EmpId: empID, LarkAppId: larkAppID, LarkOpenId: openID}).
		Delete()
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return empcap.ErrLarkIdentityNotFound
	}
	return nil
}

// SetSysUser 设置或取消员工的关联后台账号。sysUserID 为 0 表示取消关联。
// 一名后台账号最多关联一名员工：候选校验（可见性）由控制器经宿主 usercap 完成，
// 这里只做占用判定与写入——同步从不写该列，因此不会与同步冲突。
func (s *Service) SetSysUser(ctx context.Context, empID int64, sysUserID int) error {
	if sysUserID < 0 {
		return empcap.ErrInvalidParameter
	}
	if _, err := s.GetDetail(ctx, empID); err != nil {
		return err
	}
	cols := dao.Employee.Columns()
	if sysUserID > 0 {
		taken, err := dao.Employee.Ctx(ctx).
			Where(do.Employee{SysUserId: sysUserID}).
			WhereNot(cols.Id, empID).
			Count()
		if err != nil {
			return err
		}
		if taken > 0 {
			return empcap.ErrSysUserTaken
		}
	}
	_, err := dao.Employee.Ctx(ctx).
		Where(do.Employee{Id: empID}).
		Data(do.Employee{SysUserId: sysUserID}).
		Update()
	return err
}

// adminRows 把员工行装配为后台读模型。idSource 为空表示取全部身份，
// 否则只取该来源（列表传 own）。
func (s *Service) adminRows(ctx context.Context, rows []*entity.Employee, idSource string) ([]*AdminRow, error) {
	if len(rows) == 0 {
		return []*AdminRow{}, nil
	}
	empIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		empIDs = append(empIDs, r.Id)
	}

	model := dao.LarkIdentity.Ctx(ctx).WhereIn(dao.LarkIdentity.Columns().EmpId, empIDs)
	if idSource != "" {
		model = model.Where(dao.LarkIdentity.Columns().IdSource, idSource)
	}
	var identities []*entity.LarkIdentity
	if err := model.OrderAsc(dao.LarkIdentity.Columns().LarkAppId).Scan(&identities); err != nil {
		return nil, err
	}
	byEmp := make(map[int64][]LarkIdentityView, len(rows))
	for _, id := range identities {
		byEmp[id.EmpId] = append(byEmp[id.EmpId], LarkIdentityView{
			LarkAppID: id.LarkAppId,
			OpenID:    id.LarkOpenId,
			UserID:    id.LarkUserId,
			IDSource:  id.IdSource,
			CreatedAt: unixMilli(id.CreatedAt),
		})
	}

	out := make([]*AdminRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, &AdminRow{
			EmpID:      r.Id,
			TenantID:   r.TenantId,
			Name:       r.Name,
			EmployeeNo: r.MokaEmployeeNo,
			Status:     r.Status,
			SysUserID:  r.SysUserId,
			CreatedAt:  unixMilli(r.CreatedAt),
			UpdatedAt:  unixMilli(r.UpdatedAt),
			Identities: byEmp[r.Id],
		})
	}
	return out, nil
}

// normalizePaging 收敛分页参数：非正数取默认值，超过上限按上限截断。
func normalizePaging(pageNum, pageSize int) (int, int) {
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageNum, pageSize
}

// unixMilli 把可空时间转换为毫秒时间戳（nil → nil），与前端时间格式化约定一致。
func unixMilli(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}
