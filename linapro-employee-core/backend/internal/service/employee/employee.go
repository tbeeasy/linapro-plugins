// Package employee 实现核心的员工写入网关与读取查询。
// 它是员工表和 lark_identity 映射表的唯一写入方，强制 EmpID 分配、
// (tenant_id, moka_employee_no) 去重键以及状态状态机（empcap.Service）。
package employee

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// Service 是 empcap.Service 的拥有方插件实现。
type Service struct{}

// 编译期断言：Service 满足共享契约。
var _ empcap.Service = (*Service)(nil)

// New 返回新的员工服务。
func New() *Service { return &Service{} }

// UpsertEmployee 是 Moka 同步的权威档案覆盖路径。
// 它按 (tenantID, profile.EmployeeNo) 查找；找到则覆盖档案，否则新建行。
// profile.EmployeeNo 必须非空（同步是唯一写入方）。档案更新与部门替换
// 在同一事务中执行。
//
// 在职状态：profile.Departed=false 时把已存在的员工恢复为在职（覆盖重新入职、
// 误标离职被 Moka 纠正等场景）；profile.Departed=true 时不改动已存在员工的状态，
// 置离职统一由 MarkDeparted 负责。新建行按 profile.Departed 直接落在职或离职。
func (s *Service) UpsertEmployee(ctx context.Context, tenantID int, profile empcap.Profile) (int64, error) {
	if profile.EmployeeNo == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	existing, err := s.findLiveByEmployeeNo(ctx, tenantID, profile.EmployeeNo)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return s.create(ctx, tenantID, profile)
	}
	if err = dao.Employee.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 覆盖 Name：Moka 是姓名的权威来源，姓名可能变更（纠正录入错误、
		// 法定改名、曾用名规范化等），且姓名还是飞书回填的 JOIN 键，本地
		// 不跟随更新会导致匹配失效。不写 MokaEmployeeNo：它是本次查询键，
		// 库中值必然已等于 profile.EmployeeNo，再写属恒等空操作。
		updateData := do.Employee{
			Name: profile.Name,
		}
		// Moka 报在职则恢复在职；否则不写 Status（DO 字段为 nil 时 ORM 忽略），
		// 离职交给 MarkDeparted，避免在本路径里静默改离职。
		if !profile.Departed {
			updateData.Status = empcap.StatusActive
		}
		if _, uerr := dao.Employee.Ctx(ctx).
			Where(do.Employee{Id: existing.Id}).
			Data(updateData).
			Update(); uerr != nil {
			return uerr
		}
		return s.replaceDepts(ctx, tenantID, existing.Id, profile.Departments)
	}); err != nil {
		return 0, err
	}
	return existing.Id, nil
}

// MarkDeparted 将某员工状态置为离职。
func (s *Service) MarkDeparted(ctx context.Context, tenantID int, empID int64) error {
	_, err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, Id: empID}).
		Data(do.Employee{Status: empcap.StatusDeparted}).
		Update()
	return err
}

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

// GetByName 按 (tenantID, 姓名) 返回一名员工。调用方依赖每个租户内
// 姓名唯一这一业务不变式；无匹配时返回 ErrEmployeeNotFound，
// 数据库失败时包装非空错误。
func (s *Service) GetByName(ctx context.Context, tenantID int, name string) (*empcap.Employee, error) {
	var e *entity.Employee
	if err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, Name: name}).
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

// ResolveEmpIDByLarkIdentity 通过 lark_identity 表返回由
// (larkAppID, openID) 映射的 EmpID。未注册时返回 ErrEmployeeNotFound，
// 使登录按失败关闭处理（绝不自动开通）。
func (s *Service) ResolveEmpIDByLarkIdentity(ctx context.Context, larkAppID, openID string) (int64, error) {
	var row *entity.LarkIdentity
	// 仅匹配 id_source='own'：assoc 行是关联组织视角身份、仅供写人员字段采集，
	// 绝不作登录凭据。加此过滤后，即便同一 (lark_app_id, open_id) 以 assoc 登记，
	// 登录仍查不到、返回 ErrEmployeeNotFound（fail-closed）。
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: larkAppID, LarkOpenId: openID, IdSource: empcap.IDSourceOwn}).
		Scan(&row); err != nil {
		return 0, err
	}
	if row == nil {
		return 0, empcap.ErrEmployeeNotFound
	}
	return row.EmpId, nil
}

// ResolveEmpIDByLarkUserID 通过 lark_identity 表按 (larkAppID, userID) 双键
// 返回映射的 EmpID，供考勤等只带 user_id 的数据源反查员工。同一企业配多个
// app 时同人会有多行且 user_id 相同，均指向同一 emp_id，取任意一行即可。
// 未命中或该行 user_id 为空时返回 ErrEmployeeNotFound。
func (s *Service) ResolveEmpIDByLarkUserID(ctx context.Context, larkAppID, userID string) (int64, error) {
	if userID == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	var row *entity.LarkIdentity
	// 同样加 id_source='own' 过滤作双保险：assoc 行 lark_user_id 恒空、天然不命中，
	// 但显式过滤可防未来接口返 user_id 时 assoc 行泄漏进考勤反查。
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: larkAppID, LarkUserId: userID, IdSource: empcap.IDSourceOwn}).
		Scan(&row); err != nil {
		return 0, err
	}
	if row == nil {
		return 0, empcap.ErrEmployeeNotFound
	}
	return row.EmpId, nil
}

// UpsertLarkIdentity 写入 (larkAppID, openID) → empID 映射，并一并落库
// userID（企业级 user_id，供考勤反查）。键已存在时更新目标 empID、userID 与
// idSource，否则插入新行。
//
// 以主键 (lark_app_id, lark_open_id) 为冲突键做单条原子 upsert（GoFrame
// Save + OnConflict，由驱动生成对应方言），一次数据库往返；并发执行的两次同步
// 不会因「先查后插」的竞态撞主键。冲突更新时 created_at 保持首次写入值。
func (s *Service) UpsertLarkIdentity(ctx context.Context, larkAppID, openID, userID, idSource string, empID int64) error {
	if idSource == "" {
		idSource = empcap.IDSourceOwn
	}
	cols := dao.LarkIdentity.Columns()
	_, err := dao.LarkIdentity.Ctx(ctx).
		Data(do.LarkIdentity{
			LarkAppId:  larkAppID,
			LarkOpenId: openID,
			LarkUserId: userID,
			IdSource:   idSource,
			EmpId:      empID,
		}).
		OnConflict(cols.LarkAppId, cols.LarkOpenId).
		Save()
	return err
}

// MapLarkOpenIDsByEmployeeNo 以 Moka 员工工号（employee.moka_employee_no）为查找键：
// 一次查询 JOIN employee 与 lark_identity，返回「工号 → 该 app 作用域下全部 open_id」映射，
// 按 larkAppID 过滤 lark_identity，跨租户、不限在职、不按 id_source 过滤（own + assoc 都取，
// 双租户同人多个 open_id 全返回）。工号为精确锚点，无需姓名归一——空工号的员工行直接跳过。
//
// 供 recruit-pipeline / report-sync 写人员字段（消费方经门面 LarkOpenIDResolverByEmployeeNos
// 复用该批量结果，无 N+1）。注意：open_id 按应用作用域签发，必须按写表格的 larkAppID 过滤，
// 写侧以 user_id_type=open_id 识别人员字段；不过滤会混入其它应用作用域的 id，写入即错。
func (s *Service) MapLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string][]string, error) {
	// 只取工号与 open_id 两列；按 emp_id 关联，按 lark_app_id 过滤（限定写表格应用作用域）。
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []struct {
		EmployeeNo string `orm:"moka_employee_no"`
		OpenID     string `orm:"lark_open_id"`
	}
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkOpenId+" AS lark_open_id").
		Scan(&rows)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(rows))
	seen := make(map[string]map[string]struct{}, len(rows)) // 工号 → open_id 去重集
	for _, r := range rows {
		if r.EmployeeNo == "" || r.OpenID == "" {
			continue
		}
		if seen[r.EmployeeNo] == nil {
			seen[r.EmployeeNo] = make(map[string]struct{})
		}
		if _, dup := seen[r.EmployeeNo][r.OpenID]; dup {
			continue
		}
		seen[r.EmployeeNo][r.OpenID] = struct{}{}
		out[r.EmployeeNo] = append(out[r.EmployeeNo], r.OpenID)
	}
	return out, nil
}

// MapOwnLarkOpenIDsByEmployeeNo 是面向出站消息推送的只读寻址：以 Moka 工号为键，
// 返回「工号 → 该 app 作用域下 id_source='own' 的 open_id」映射。它与
// MapLarkOpenIDsByEmployeeNo 的区别在于只取 own 身份——assoc 为他企业视角身份、
// 无法经本应用投递消息，必须排除。按 larkAppID 作用域过滤、跨所有租户、不限在职。
// 同一 (lark_app_id, emp) 下 own 行至多一条，故返回单值 map；若异常出现多条 own 行
// 取首条非空。工号为空或无 own 映射的员工不入 map。供 linapro-eval-360 遍历各 app
// 取 own 映射、用该 app 凭据当场发送飞书卡片。无任何映射时返回空 map。
func (s *Service) MapOwnLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string]string, error) {
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []struct {
		EmployeeNo string `orm:"moka_employee_no"`
		OpenID     string `orm:"lark_open_id"`
	}
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Where("li."+idCols.IdSource, empcap.IDSourceOwn).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkOpenId+" AS lark_open_id").
		Scan(&rows)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.EmployeeNo == "" || r.OpenID == "" {
			continue
		}
		if _, exists := out[r.EmployeeNo]; !exists {
			// 同一工号仅取首条 own open_id（own 在 (app, emp) 下应唯一）。
			out[r.EmployeeNo] = r.OpenID
		}
	}
	return out, nil
}

// MapOwnLarkUserIDsByEmployeeNo 是 MapOwnLarkOpenIDsByEmployeeNo 的 user_id 版本：以 Moka
// 工号为键，返回该 app 作用域下 own 身份的寻址索引（语义见 empcap.LarkUserIDIndex）。按 larkAppID 作用域过滤、跨所有租户、不限在职。
// 供 linapro-lark-push 按推送应用的 identity_app_id 查表投递。
func (s *Service) MapOwnLarkUserIDsByEmployeeNo(ctx context.Context, larkAppID string) (empcap.LarkUserIDIndex, error) {
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []larkUserIDRow
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Where("li."+idCols.IdSource, empcap.IDSourceOwn).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkUserId+" AS lark_user_id").
		Scan(&rows)
	if err != nil {
		return empcap.LarkUserIDIndex{}, err
	}
	return buildLarkUserIDIndex(rows), nil
}

// larkUserIDRow 是「工号 + user_id」查询的中间行。
type larkUserIDRow struct {
	EmployeeNo string `orm:"moka_employee_no"`
	UserID     string `orm:"lark_user_id"`
}

// buildLarkUserIDIndex 把查询行按工号归入三个集合：抽成纯函数以便单测覆盖。
//
// 规则：
//   - 工号为空 → 忽略（无法按工号寻址）；
//   - user_id 为空 → 记入 BlankEmployeeNos；
//   - 同一工号出现第二个不同的 user_id → 移出 ByEmployeeNo、记入 CollidedEmployeeNos，之后同工号的行
//     不再让它回到 ByEmployeeNo；同一 user_id 的重复行（如多部门 JOIN）不算碰撞。
//
// 最后从 BlankEmployeeNos 去掉已可投递或已碰撞的工号，保证三个集合互不相交（同一工号既有空行又有
// 有效行时，以有效行为准）。
func buildLarkUserIDIndex(rows []larkUserIDRow) empcap.LarkUserIDIndex {
	idx := empcap.LarkUserIDIndex{
		ByEmployeeNo:        make(map[string]string, len(rows)),
		BlankEmployeeNos:    make(map[string]struct{}),
		CollidedEmployeeNos: make(map[string]struct{}),
	}
	for _, r := range rows {
		switch {
		case r.EmployeeNo == "":
			continue
		case r.UserID == "":
			idx.BlankEmployeeNos[r.EmployeeNo] = struct{}{}
			continue
		}
		if _, collided := idx.CollidedEmployeeNos[r.EmployeeNo]; collided {
			continue
		}
		prev, exists := idx.ByEmployeeNo[r.EmployeeNo]
		if !exists {
			idx.ByEmployeeNo[r.EmployeeNo] = r.UserID
			continue
		}
		if prev == r.UserID {
			continue
		}
		idx.CollidedEmployeeNos[r.EmployeeNo] = struct{}{}
		delete(idx.ByEmployeeNo, r.EmployeeNo)
	}
	for no := range idx.BlankEmployeeNos {
		_, addressable := idx.ByEmployeeNo[no]
		_, collided := idx.CollidedEmployeeNos[no]
		if addressable || collided {
			delete(idx.BlankEmployeeNos, no)
		}
	}
	return idx
}

func (s *Service) findLiveByEmployeeNo(ctx context.Context, tenantID int, employeeNo string) (*entity.Employee, error) {
	var e *entity.Employee
	err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, MokaEmployeeNo: employeeNo}).
		Scan(&e)
	return e, err
}

// create 在单个事务中插入新员工及其部门，从而在部分失败时
// 员工行绝不会被持久化而缺少其部门行。
func (s *Service) create(ctx context.Context, tenantID int, profile empcap.Profile) (int64, error) {
	var newID int64
	if err := dao.Employee.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		row := do.Employee{
			TenantId:       tenantID,
			Name:           profile.Name,
			MokaEmployeeNo: profile.EmployeeNo,
			Status:         profileStatus(profile),
		}
		id, err := dao.Employee.Ctx(ctx).Data(row).InsertAndGetId()
		if err != nil {
			return err
		}
		newID = id
		return s.replaceDepts(ctx, tenantID, id, profile.Departments)
	}); err != nil {
		return 0, err
	}
	return newID, nil
}

// replaceDepts 在调用方的事务内替换某员工的部门行
// （删除 + 批量插入共享该 ctx 事务）。
//
// depts 为空时直接返回、不删除既有部门行：当前同步数据源（Moka）不下发部门，
// 每轮都以空部门调用，若照删会让每名员工每轮多一次无意义的 DELETE，且会抹掉
// 将来由其他来源写入的部门。因此「空」语义为「本次不更新部门」，而非「清空部门」。
func (s *Service) replaceDepts(ctx context.Context, tenantID int, empID int64, depts []empcap.Department) error {
	if len(depts) == 0 {
		return nil
	}
	if _, err := dao.EmployeeDept.Ctx(ctx).Where(do.EmployeeDept{EmpId: empID}).Delete(); err != nil {
		return err
	}
	rows := make([]do.EmployeeDept, 0, len(depts))
	for _, d := range depts {
		rows = append(rows, do.EmployeeDept{
			TenantId:         tenantID,
			EmpId:            empID,
			LarkDepartmentId: d.LarkDepartmentID,
			IsPrimary:        boolToInt(d.IsPrimary),
		})
	}
	_, err := dao.EmployeeDept.Ctx(ctx).Data(rows).Insert()
	return err
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
			EmpID:       e.Id,
			TenantID:    e.TenantId,
			EmployeeNo:  e.MokaEmployeeNo,
			Name:        e.Name,
			Status:      e.Status,
			Departments: deptsByEmp[e.Id],
		})
	}
	return out, nil
}

// profileStatus 返回新建员工行应落的在职状态。
func profileStatus(p empcap.Profile) int {
	if p.Departed {
		return empcap.StatusDeparted
	}
	return empcap.StatusActive
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
