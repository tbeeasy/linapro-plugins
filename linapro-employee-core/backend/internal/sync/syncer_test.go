package sync

import (
	"context"
	"errors"
	"maps"
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// -- 伪实现 --------------------------------------------------------------------

type fakeMokaFetcher struct {
	employees []MokaEmployee
	err       error
}

func (f *fakeMokaFetcher) ListAllEmployees(_ context.Context) ([]MokaEmployee, error) {
	return f.employees, f.err
}

// fakeLarkFetcher 是一个飞书企业（一个 lark_app_id）的通讯录来源。
type fakeLarkFetcher struct {
	appID string
	users []LarkUser
	err   error
}

func (f *fakeLarkFetcher) AppID() string { return f.appID }
func (f *fakeLarkFetcher) ListUsers(_ context.Context) ([]LarkUser, error) {
	return f.users, f.err
}

// empKey 按 tenant+EmpID 标识员工行（离职断言用）。
type empKey struct {
	tenantID int
	empID    int64
}

// identityKey 标识一行 lark_identity。
type identityKey struct {
	appID  string
	openID string
}

// identityRow 记录一行 lark_identity 的落库内容，供测试断言 id_source。
// 注：assoc 行不落 user_id——assoc 通道可返对方租户空间 user_id，但当前共享租户链路
// 下它无任何消费方（写人员字段用 open_id、登录/反查仅 own，见 D17），故不采集不落库。
type identityRow struct {
	empID    int64
	idSource string
}

// userIDKey 标识一行 lark_identity 的企业级 user_id 反查键。
type userIDKey struct {
	appID  string
	userID string
}

// fakeEmpSvc 为 sync 包测试实现 empcap.Service，无需数据库。
type fakeEmpSvc struct {
	rows        map[string]*empcap.Employee // 以 noKey(tenant, empNo) 为键
	identities  map[identityKey]identityRow // lark_identity 行（按 open_id 键）
	userIDToEmp map[userIDKey]int64         // lark_identity 的 user_id 反查（仅 own）
	nextID      int64
	departed    []empKey
	// 离职对账调用计数与故障注入：验证对账走一次批量回查而非逐条查询。
	anyTenantGetCalls   int
	anyTenantBatchCalls int
	anyTenantBatchErr   error
}

var _ empcap.Service = (*fakeEmpSvc)(nil)

func newFakeEmpSvc() *fakeEmpSvc {
	return &fakeEmpSvc{
		rows:        map[string]*empcap.Employee{},
		identities:  map[identityKey]identityRow{},
		userIDToEmp: map[userIDKey]int64{},
	}
}

func noKey(tenantID int, empNo string) string { return "no:" + istr(tenantID) + ":" + empNo }

func istr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func (s *fakeEmpSvc) UpsertEmployee(_ context.Context, tenantID int, p empcap.Profile) (int64, error) {
	if p.EmployeeNo == "" {
		return 0, errors.New("fakeEmpSvc: EmployeeNo required")
	}
	k := noKey(tenantID, p.EmployeeNo)
	if e, ok := s.rows[k]; ok {
		e.Name = p.Name
		// 与真实实现一致：Moka 报在职时恢复在职，报离职时不改状态。
		if !p.Departed {
			e.Status = empcap.StatusActive
		}
		return e.EmpID, nil
	}
	s.nextID++
	status := empcap.StatusActive
	if p.Departed {
		status = empcap.StatusDeparted
	}
	e := &empcap.Employee{EmpID: s.nextID, TenantID: tenantID, EmployeeNo: p.EmployeeNo, Name: p.Name, Status: status}
	s.rows[k] = e
	return s.nextID, nil
}

func (s *fakeEmpSvc) MarkDeparted(_ context.Context, tenantID int, empID int64) error {
	s.departed = append(s.departed, empKey{tenantID, empID})
	for _, e := range s.rows {
		if e.TenantID == tenantID && e.EmpID == empID {
			e.Status = empcap.StatusDeparted
		}
	}
	return nil
}

func (s *fakeEmpSvc) GetByID(_ context.Context, empID int64) (*empcap.Employee, error) {
	for _, e := range s.rows {
		if e.EmpID == empID {
			return e, nil
		}
	}
	return nil, empcap.ErrEmployeeNotFound
}

func (s *fakeEmpSvc) GetByEmployeeNo(_ context.Context, tenantID int, empNo string) (*empcap.Employee, error) {
	if e, ok := s.rows[noKey(tenantID, empNo)]; ok {
		return e, nil
	}
	return nil, empcap.ErrEmployeeNotFound
}

func (s *fakeEmpSvc) GetByEmployeeNoAnyTenant(_ context.Context, empNo string) (*empcap.Employee, error) {
	s.anyTenantGetCalls++
	return s.findAnyTenant(empNo)
}

// findAnyTenant 是跨租户按工号查找的内部实现，供单条与批量 fake 共用（不计数）。
func (s *fakeEmpSvc) findAnyTenant(empNo string) (*empcap.Employee, error) {
	for _, e := range s.rows {
		if e.EmployeeNo == empNo {
			return e, nil
		}
	}
	return nil, empcap.ErrEmployeeNotFound
}

func (s *fakeEmpSvc) GetByName(_ context.Context, tenantID int, name string) (*empcap.Employee, error) {
	for _, e := range s.rows {
		if e.TenantID == tenantID && e.Name == name {
			return e, nil
		}
	}
	return nil, empcap.ErrEmployeeNotFound
}

func (s *fakeEmpSvc) ListActiveByTenant(_ context.Context, tenantID int) ([]*empcap.Employee, error) {
	var out []*empcap.Employee
	for _, e := range s.rows {
		if e.TenantID == tenantID && e.Status == empcap.StatusActive {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeEmpSvc) ResolveEmpIDByLarkUserID(_ context.Context, appID, userID string) (int64, error) {
	if userID == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	if id, ok := s.userIDToEmp[userIDKey{appID, userID}]; ok {
		return id, nil
	}
	return 0, empcap.ErrEmployeeNotFound
}

// ResolveEmpIDByLarkIdentity 仅匹配 id_source='own' 的行（与真实实现一致），
// assoc 行不参与登录解析。
func (s *fakeEmpSvc) ResolveEmpIDByLarkIdentity(_ context.Context, appID, openID string) (int64, error) {
	if row, ok := s.identities[identityKey{appID, openID}]; ok && row.idSource == empcap.IDSourceOwn {
		return row.empID, nil
	}
	return 0, empcap.ErrEmployeeNotFound
}

func (s *fakeEmpSvc) UpsertLarkIdentity(_ context.Context, appID, openID, userID, idSource string, empID int64) error {
	if idSource == "" {
		idSource = empcap.IDSourceOwn
	}
	s.identities[identityKey{appID, openID}] = identityRow{empID: empID, idSource: idSource}
	// 仅 own 行且带 user_id 时才登记 user_id 反查（assoc 行不落 user_id、不参与反查）。
	if userID != "" && idSource == empcap.IDSourceOwn {
		s.userIDToEmp[userIDKey{appID, userID}] = empID
	}
	return nil
}
func (s *fakeEmpSvc) MapLarkOpenIDsByEmployeeNo(_ context.Context, larkAppID string) (map[string][]string, error) {
	out := map[string][]string{}
	for k, row := range s.identities {
		if k.appID != larkAppID {
			continue
		}
		emp, err := s.GetByID(context.Background(), row.empID)
		if err != nil || emp.EmployeeNo == "" {
			continue
		}
		out[emp.EmployeeNo] = append(out[emp.EmployeeNo], k.openID)
	}
	return out, nil
}

func (s *fakeEmpSvc) MapOwnLarkOpenIDsByEmployeeNo(_ context.Context, larkAppID string) (map[string]string, error) {
	out := map[string]string{}
	for k, row := range s.identities {
		if k.appID != larkAppID || row.idSource != empcap.IDSourceOwn {
			continue
		}
		emp, err := s.GetByID(context.Background(), row.empID)
		if err != nil || emp.EmployeeNo == "" {
			continue
		}
		if _, exists := out[emp.EmployeeNo]; !exists {
			out[emp.EmployeeNo] = k.openID
		}
	}
	return out, nil
}

// MapOwnLarkUserIDsByEmployeeNo 是 user_id 版本（fake：由 open_id 映射推导同值，sync 不用）。
func (s *fakeEmpSvc) MapOwnLarkUserIDsByEmployeeNo(_ context.Context, larkAppID string) (empcap.LarkUserIDIndex, error) {
	ownByNo, err := s.MapOwnLarkOpenIDsByEmployeeNo(context.Background(), larkAppID)
	if err != nil {
		return empcap.LarkUserIDIndex{}, err
	}
	idx := empcap.LarkUserIDIndex{ByEmployeeNo: make(map[string]string, len(ownByNo))}
	maps.Copy(idx.ByEmployeeNo, ownByNo)
	return idx, nil
}

// MapByEmployeeNos 批量按工号查询（fake：委托 GetByEmployeeNo 逐条兜底，sync 不用）。
func (s *fakeEmpSvc) MapByEmployeeNos(_ context.Context, tenantID int, employeeNos []string) (map[string]*empcap.Employee, error) {
	out := map[string]*empcap.Employee{}
	for _, no := range employeeNos {
		if no == "" {
			continue
		}
		if emp, err := s.GetByEmployeeNo(context.Background(), tenantID, no); err == nil && emp != nil {
			out[no] = emp
		}
	}
	return out, nil
}

// MapByEmployeeNosAnyTenant 跨租户批量按工号查询（离职对账使用；计数并支持故障注入）。
func (s *fakeEmpSvc) MapByEmployeeNosAnyTenant(_ context.Context, employeeNos []string) (map[string]*empcap.Employee, error) {
	s.anyTenantBatchCalls++
	if s.anyTenantBatchErr != nil {
		return nil, s.anyTenantBatchErr
	}
	out := map[string]*empcap.Employee{}
	for _, no := range employeeNos {
		if no == "" {
			continue
		}
		if emp, err := s.findAnyTenant(no); err == nil && emp != nil {
			out[no] = emp
		}
	}
	return out, nil
}

// MapByNamesAnyTenant 按姓名批量查询（fake：sync 不用，返回空映射）。
func (s *fakeEmpSvc) MapByNamesAnyTenant(context.Context, []string) (map[string][]*empcap.Employee, error) {
	return map[string][]*empcap.Employee{}, nil
}

// MapByEmpIDs 批量按 EmpID 查询（fake：委托 GetByID 逐条兜底，sync 不用）。
func (s *fakeEmpSvc) MapByEmpIDs(_ context.Context, empIDs []int64) (map[int64]*empcap.Employee, error) {
	out := map[int64]*empcap.Employee{}
	for _, id := range empIDs {
		if emp, err := s.GetByID(context.Background(), id); err == nil && emp != nil {
			out[id] = emp
		}
	}
	return out, nil
}

// -- 测试 --------------------------------------------------------------------

// TestRunFullSync_CrossEnterpriseSamePerson 验证同一个人出现在两个
// 飞书企业时会为每个应用生成一行 lark_identity，且都指向同一 emp_id。
func TestRunFullSync_CrossEnterpriseSamePerson(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "E001", Realname: "Alice", Department: "广州星嘉/公共部门", Status: MokaStatusActive},
	}}
	fFetcher := &fakeLarkFetcher{appID: "cli_f", users: []LarkUser{{Name: "Alice", OpenID: "ou_f", EmployeeNo: "E001"}}}
	gFetcher := &fakeLarkFetcher{appID: "cli_g", users: []LarkUser{{Name: "Alice", OpenID: "ou_g", EmployeeNo: "E001"}}}
	syncer := NewSyncer(svc, moka, []LarkAppFetcher{fFetcher, gFetcher})
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if sum.Backfilled != 2 {
		t.Fatalf("want backfilled=2 (one per app), got %+v", sum)
	}
	idF, _ := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_f")
	idG, _ := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_g", "ou_g")
	if idF == 0 || idF != idG {
		t.Fatalf("want same emp_id across apps, got f=%d g=%d", idF, idG)
	}
}

// TestRunFullSync_DeptNotInMap 验证部门未映射的员工
// 会被跳过并告警。
func TestRunFullSync_DeptNotInMap(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "E003", Realname: "Carol", Department: "未知公司/某部门", Status: MokaStatusActive},
	}}
	syncer := NewSyncer(svc, moka, nil)
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if sum.Upserted != 0 {
		t.Fatalf("want upserted=0 for unmapped dept, got %d", sum.Upserted)
	}
	if sum.Warnings < 1 {
		t.Fatalf("want at least 1 warning for unmapped dept, got %+v", sum)
	}
}

// TestRunFullSync_DualTenantOwnAssoc 验证双租户同人在 own 与 assoc 通道各被枚举到时，
// 两个 open_id 各写一行 lark_identity（id_source 分别 own/assoc）、均指向同一 emp_id，
// 不判歧义跳过（D15/D20 去折叠）；且 assoc 行不参与登录解析（仅 own 命中）。
func TestRunFullSync_DualTenantOwnAssoc(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "E004", Realname: "Dave", Department: "广州星嘉/公共部门", Status: MokaStatusActive},
	}}
	// 同一 app 内同名 Dave 的两行：own open_id 与 assoc open_id，均属同一自然人。
	// 注：assoc 行不落 user_id——assoc 通道虽可返对方租户空间 user_id，但在当前
	// 共享租户链路下无消费方（写人员字段用 open_id、反查仅 own），故此处不填充。
	larkF := &fakeLarkFetcher{appID: "cli_f", users: []LarkUser{
		{Name: "Dave", OpenID: "ou_dave_own", UserID: "uid_dave", EmployeeNo: "E004", Source: empcap.IDSourceOwn},
		{Name: "Dave", OpenID: "ou_dave_assoc", EmployeeNo: "E004", Source: empcap.IDSourceAssoc},
	}}
	syncer := NewSyncer(svc, moka, []LarkAppFetcher{larkF})
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	// 两个 open_id 各写一行，不判歧义跳过。
	if sum.Backfilled != 2 {
		t.Fatalf("want backfilled=2 (own + assoc), got %+v", sum)
	}
	emp, err := svc.GetByEmployeeNo(ctx, 1, "E004")
	if err != nil {
		t.Fatalf("GetByEmployeeNo: %v", err)
	}
	ownRow, ok := svc.identities[identityKey{"cli_f", "ou_dave_own"}]
	if !ok || ownRow.idSource != empcap.IDSourceOwn || ownRow.empID != emp.EmpID {
		t.Fatalf("want own row -> emp %d id_source=own, got %+v ok=%v", emp.EmpID, ownRow, ok)
	}
	assocRow, ok := svc.identities[identityKey{"cli_f", "ou_dave_assoc"}]
	if !ok || assocRow.idSource != empcap.IDSourceAssoc || assocRow.empID != emp.EmpID {
		t.Fatalf("want assoc row -> emp %d id_source=assoc, got %+v ok=%v", emp.EmpID, assocRow, ok)
	}
	// 登录仅 own 命中；assoc 行不参与登录解析（fail-closed）。
	if id, err := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_dave_own"); err != nil || id != emp.EmpID {
		t.Fatalf("want own open_id resolves to emp %d, got %d err %v", emp.EmpID, id, err)
	}
	if _, err := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_dave_assoc"); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("want assoc open_id NOT to resolve for login, got err %v", err)
	}
}

// TestRunFullSync_DepartedMokaStatus 验证 Moka 中 Status == MokaStatusDeparted
// 的员工被标记为离职。
func TestRunFullSync_DepartedMokaStatus(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	if _, err := svc.UpsertEmployee(ctx, 1, empcap.Profile{Name: "Eve", EmployeeNo: "E005"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "E005", Realname: "Eve", Department: "广州星嘉/公共部门", Status: MokaStatusDeparted},
	}}
	syncer := NewSyncer(svc, moka, nil)
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if sum.Departed < 1 {
		t.Fatalf("want at least 1 departed, got %+v", sum)
	}
	if len(svc.departed) == 0 {
		t.Fatal("want MarkDeparted called at least once")
	}
}

// TestRunFullSync_ReactivatesReturningEmployee 验证此前被标离职的员工在 Moka
// 重新报在职后，同步会把他恢复为在职（此前只覆盖姓名，导致永久被 EmpAuth 拦截）。
func TestRunFullSync_ReactivatesReturningEmployee(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	id, err := svc.UpsertEmployee(ctx, 1, empcap.Profile{Name: "Grace", EmployeeNo: "GZ000020"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err = svc.MarkDeparted(ctx, 1, id); err != nil {
		t.Fatalf("seed departed: %v", err)
	}

	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ000020", Realname: "Grace", Department: "广州星嘉/公共部门", Status: MokaStatusActive},
	}}
	if _, err = NewSyncer(svc, moka, nil).RunFullSync(ctx, map[string]int{"广州星嘉": 1}); err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	got, err := svc.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Active() {
		t.Fatalf("want employee reactivated, got status %d", got.Status)
	}
}

// TestRunFullSync_DepartedEmptyDept 验证离职员工部门为空（无法从部门推导租户）时，
// 仍能按工号跨租户回查、用其在职时落库的 tenant_id 标记离职，且不产生告警。
func TestRunFullSync_DepartedEmptyDept(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	// 先在租户 7 建档（模拟该员工此前在职时已落库）。
	if _, err := svc.UpsertEmployee(ctx, 7, empcap.Profile{Name: "Frank", EmployeeNo: "GZ000010"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Moka 报离职，部门为空——现有 dept_tenant_map 无法命中。
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ000010", Realname: "Frank", Department: "", Status: MokaStatusDeparted},
	}}
	syncer := NewSyncer(svc, moka, nil)
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if sum.Departed != 1 {
		t.Fatalf("want 1 departed, got %+v", sum)
	}
	if sum.Warnings != 0 {
		t.Fatalf("want 0 warnings for departed empty-dept employee, got %d", sum.Warnings)
	}
	if len(svc.departed) != 1 || svc.departed[0].tenantID != 7 {
		t.Fatalf("want MarkDeparted on tenant 7, got %+v", svc.departed)
	}
}

// TestRunFullSync_DepartedBatchLookup 验证离职对账按工号一次批量回查：
// 多名离职员工只调一次 MapByEmployeeNosAnyTenant、不逐条调 GetByEmployeeNoAnyTenant；
// 本地不存在的工号与已离职员工被跳过，只有在职→离职的员工计入 Departed。
func TestRunFullSync_DepartedBatchLookup(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	idA, err := svc.UpsertEmployee(ctx, 3, empcap.Profile{Name: "A", EmployeeNo: "GZ000031"})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err = svc.UpsertEmployee(ctx, 4, empcap.Profile{Name: "B", EmployeeNo: "GZ000032"}); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	idC, err := svc.UpsertEmployee(ctx, 3, empcap.Profile{Name: "C", EmployeeNo: "GZ000033"})
	if err != nil {
		t.Fatalf("seed C: %v", err)
	}
	if err = svc.MarkDeparted(ctx, 3, idC); err != nil {
		t.Fatalf("seed C departed: %v", err)
	}
	svc.departed = nil // 只统计本轮同步产生的标记

	// 全部部门为空（无法映射租户），只走离职对账；GZ000039 本地不存在。
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ000031", Realname: "A", Status: MokaStatusDeparted},
		{EmployeeNo: "GZ000032", Realname: "B", Status: MokaStatusDeparted},
		{EmployeeNo: "GZ000033", Realname: "C", Status: MokaStatusDeparted},
		{EmployeeNo: "GZ000039", Realname: "Ghost", Status: MokaStatusDeparted},
	}}
	sum, err := NewSyncer(svc, moka, nil).RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if svc.anyTenantBatchCalls != 1 || svc.anyTenantGetCalls != 0 {
		t.Fatalf("want 1 batch lookup and 0 per-row lookups, got batch=%d per-row=%d",
			svc.anyTenantBatchCalls, svc.anyTenantGetCalls)
	}
	if sum.Departed != 2 || len(svc.departed) != 2 {
		t.Fatalf("want 2 newly departed (A, B), got sum=%+v marks=%+v", sum, svc.departed)
	}
	if svc.departed[0] != (empKey{3, idA}) {
		t.Fatalf("want A marked on its stored tenant 3 first, got %+v", svc.departed)
	}
}

// TestRunFullSync_DepartedBatchLookupError 验证批量回查失败时本轮对账以 error 中止，
// 且不会逐条降级查询或误标任何人。
func TestRunFullSync_DepartedBatchLookupError(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	if _, err := svc.UpsertEmployee(ctx, 3, empcap.Profile{Name: "A", EmployeeNo: "GZ000041"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc.anyTenantBatchErr = errors.New("db down")
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ000041", Realname: "A", Status: MokaStatusDeparted},
	}}
	if _, err := NewSyncer(svc, moka, nil).RunFullSync(ctx, map[string]int{"广州星嘉": 1}); err == nil {
		t.Fatal("want RunFullSync to fail when departed batch lookup fails")
	}
	if len(svc.departed) != 0 || svc.anyTenantGetCalls != 0 {
		t.Fatalf("want no marks and no per-row fallback, got marks=%+v per-row=%d", svc.departed, svc.anyTenantGetCalls)
	}
}

// TestRunFullSync_EmptyDeptMap 验证空的部门映射会以错误中止。
func TestRunFullSync_EmptyDeptMap(t *testing.T) {
	syncer := NewSyncer(newFakeEmpSvc(), &fakeMokaFetcher{}, nil)
	if _, err := syncer.RunFullSync(context.Background(), nil); err == nil {
		t.Fatal("want error for empty dept map, got nil")
	}
}

// TestRunFullSync_EmployeeNoMatch 验证按工号优先匹配的场景：
// 飞书用户的 employee_no 与 Moka 员工工号精确匹配时，即使姓名不同也能正确建立身份映射。
func TestRunFullSync_EmployeeNoMatch(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ001", Realname: "张三", Department: "广州星嘉/公共部门", Status: MokaStatusActive},
	}}
	// 飞书通讯录中姓名不同（如飞书用英文名），但工号匹配
	larkF := &fakeLarkFetcher{appID: "cli_f", users: []LarkUser{
		{Name: "Sam Zhang", OpenID: "ou_sam", UserID: "uid_sam", EmployeeNo: "GZ001"},
	}}
	syncer := NewSyncer(svc, moka, []LarkAppFetcher{larkF})
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	if sum.Upserted != 1 || sum.Backfilled != 1 {
		t.Fatalf("want upserted=1 backfilled=1, got %+v", sum)
	}
	emp, err := svc.GetByEmployeeNo(ctx, 1, "GZ001")
	if err != nil {
		t.Fatalf("GetByEmployeeNo: %v", err)
	}
	empID, err := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_sam")
	if err != nil || empID != emp.EmpID {
		t.Fatalf("want lark_identity (cli_f, ou_sam)->%d, got %d err %v", emp.EmpID, empID, err)
	}
}

// TestRunFullSync_EmployeeNoNoCrossTenantConflict 验证按工号匹配无跨租户冲突问题：
// 工号全公司唯一，不需要冲突检测。
func TestRunFullSync_EmployeeNoNoCrossTenantConflict(t *testing.T) {
	ctx := context.Background()
	svc := newFakeEmpSvc()
	// 两个租户的员工，工号不同但姓名相同
	moka := &fakeMokaFetcher{employees: []MokaEmployee{
		{EmployeeNo: "GZ003", Realname: "王五", Department: "广州星嘉/公共部门", Status: MokaStatusActive},
		{EmployeeNo: "SZ003", Realname: "王五", Department: "深圳分公司/公共部门", Status: MokaStatusActive},
	}}
	// 飞书中两个同名用户，但工号不同
	larkF := &fakeLarkFetcher{appID: "cli_f", users: []LarkUser{
		{Name: "王五", OpenID: "ou_wang_gz", UserID: "uid_wang_gz", EmployeeNo: "GZ003"},
		{Name: "王五", OpenID: "ou_wang_sz", UserID: "uid_wang_sz", EmployeeNo: "SZ003"},
	}}
	syncer := NewSyncer(svc, moka, []LarkAppFetcher{larkF})
	sum, err := syncer.RunFullSync(ctx, map[string]int{"广州星嘉": 1, "深圳分公司": 2})
	if err != nil {
		t.Fatalf("RunFullSync: %v", err)
	}
	// 两个员工都应成功匹配，无冲突跳过
	if sum.Upserted != 2 || sum.Backfilled != 2 {
		t.Fatalf("want upserted=2 backfilled=2, got %+v", sum)
	}
	empGZ, err := svc.GetByEmployeeNo(ctx, 1, "GZ003")
	if err != nil {
		t.Fatalf("GetByEmployeeNo GZ003: %v", err)
	}
	empSZ, err := svc.GetByEmployeeNo(ctx, 2, "SZ003")
	if err != nil {
		t.Fatalf("GetByEmployeeNo SZ003: %v", err)
	}
	idGZ, err := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_wang_gz")
	if err != nil || idGZ != empGZ.EmpID {
		t.Fatalf("want ou_wang_gz->%d, got %d err %v", empGZ.EmpID, idGZ, err)
	}
	idSZ, err := svc.ResolveEmpIDByLarkIdentity(ctx, "cli_f", "ou_wang_sz")
	if err != nil || idSZ != empSZ.EmpID {
		t.Fatalf("want ou_wang_sz->%d, got %d err %v", empSZ.EmpID, idSZ, err)
	}
}
