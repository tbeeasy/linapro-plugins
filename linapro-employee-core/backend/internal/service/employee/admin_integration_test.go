// admin_integration_test.go 针对真实 PostgreSQL 验证后台「通讯录管理」所需的
// 读取与有限写入：
//
//   - ListPage 的关键词（姓名 / 工号）、租户、在职状态、飞书绑定过滤与分页
//   - ListPage 的 own 身份装配只有一次查询（防 N+1），且 assoc 身份不算绑定
//   - GetDetail 返回全部身份（含 assoc）与部门
//   - BindLarkIdentity 新建 / 本人重复绑定幂等 / 他人已占用拒绝
//   - UnbindLarkIdentity 只删本人名下身份，非本人拒绝
//   - SetSysUser 占用拒绝、0 取消关联
//
// 沿用 employee_integration_test.go 的约定：本地 PostgreSQL、唯一租户隔离、
// 测试自身建表并在 Cleanup 中清理。
package employee

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// adminSeed 描述一个后台列表测试用的员工种子。
type adminSeed struct {
	// key 用于拼出唯一姓名与工号，便于关键词检索。
	key    string
	name   string
	status int
	// identities 是要写入的飞书身份（app、open_id、来源）。
	identities []struct{ app, openID, source string }
	sysUserID  int
}

// seedAdminEmployees 写入一组员工与其飞书身份，返回 EmpID（按种子顺序）。
func seedAdminEmployees(t *testing.T, ctx context.Context, tenant int, svc *Service, seeds []adminSeed) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(seeds))
	for _, sd := range seeds {
		id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{
			Name:       sd.name,
			EmployeeNo: fmt.Sprintf("%s-%d", sd.key, tenant),
		})
		if err != nil {
			t.Fatalf("seed %s: %v", sd.key, err)
		}
		if sd.status == empcap.StatusDeparted {
			if err = svc.MarkDeparted(ctx, tenant, id); err != nil {
				t.Fatalf("mark departed %s: %v", sd.key, err)
			}
		}
		for _, ident := range sd.identities {
			if err = svc.UpsertLarkIdentity(ctx, ident.app, ident.openID, "", ident.source, id); err != nil {
				t.Fatalf("seed identity %s/%s: %v", sd.key, ident.openID, err)
			}
		}
		if sd.sysUserID != 0 {
			if err = svc.SetSysUser(ctx, id, sd.sysUserID); err != nil {
				t.Fatalf("seed sys user %s: %v", sd.key, err)
			}
		}
		ids = append(ids, id)
	}
	return ids
}

// TestListPageFilters 验证后台列表的四类过滤与分页：关键词命中姓名与工号两条路径、
// 租户隔离、在职状态、飞书绑定（只计 own），以及 total 反映过滤后的总数。
func TestListPageFilters(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	otherTenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	cleanupTenant(t, ctx, otherTenant)
	svc := New()

	appID := fmt.Sprintf("cli_admin_%d", tenant)
	seeds := []adminSeed{
		{key: "alpha", name: "AdminAlpha" + fmt.Sprint(tenant), status: empcap.StatusActive,
			identities: []struct{ app, openID, source string }{{appID, fmt.Sprintf("ou_a_%d", tenant), empcap.IDSourceOwn}}},
		{key: "bravo", name: "AdminBravo" + fmt.Sprint(tenant), status: empcap.StatusActive,
			// 只有 assoc 身份：应显示为未绑定飞书。
			identities: []struct{ app, openID, source string }{{appID, fmt.Sprintf("ou_b_%d", tenant), empcap.IDSourceAssoc}}},
		{key: "charlie", name: "AdminCharlie" + fmt.Sprint(tenant), status: empcap.StatusDeparted},
	}
	ids := seedAdminEmployees(t, ctx, tenant, svc, seeds)
	// 另一租户的员工不应出现在本租户过滤结果里。
	otherID, err := svc.UpsertEmployee(ctx, otherTenant, empcap.Profile{
		Name: "AdminAlpha" + fmt.Sprint(otherTenant), EmployeeNo: fmt.Sprintf("alpha-%d", otherTenant),
	})
	if err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	_ = otherID

	// 关键词命中姓名。
	page, err := svc.ListPage(ctx, ListFilter{Keyword: "AdminBravo" + fmt.Sprint(tenant)})
	if err != nil {
		t.Fatalf("ListPage by name: %v", err)
	}
	if page.Total != 1 || len(page.Rows) != 1 || page.Rows[0].EmpID != ids[1] {
		t.Fatalf("按姓名检索应命中 bravo 一名, got total=%d rows=%+v", page.Total, page.Rows)
	}

	// 关键词命中工号（工号里含 key）。
	page, err = svc.ListPage(ctx, ListFilter{Keyword: fmt.Sprintf("charlie-%d", tenant)})
	if err != nil {
		t.Fatalf("ListPage by employee no: %v", err)
	}
	if page.Total != 1 || page.Rows[0].EmpID != ids[2] {
		t.Fatalf("按工号检索应命中 charlie, got total=%d rows=%+v", page.Total, page.Rows)
	}

	// 租户过滤：只返回本租户种子，不含另一租户的同名员工。
	page, err = svc.ListPage(ctx, ListFilter{TenantID: tenant, Keyword: "AdminAlpha" + fmt.Sprint(tenant)})
	if err != nil {
		t.Fatalf("ListPage by tenant: %v", err)
	}
	if page.Total != 1 || page.Rows[0].EmpID != ids[0] {
		t.Fatalf("租户过滤应只命中本租户 alpha, got total=%d rows=%+v", page.Total, page.Rows)
	}

	// 在职状态过滤。
	departed := empcap.StatusDeparted
	page, err = svc.ListPage(ctx, ListFilter{TenantID: tenant, Status: &departed})
	if err != nil {
		t.Fatalf("ListPage by status: %v", err)
	}
	if page.Total != 1 || page.Rows[0].EmpID != ids[2] {
		t.Fatalf("离职过滤应只命中 charlie, got total=%d rows=%+v", page.Total, page.Rows)
	}

	// 飞书绑定过滤：own 算绑定，assoc 不算。
	bound := true
	page, err = svc.ListPage(ctx, ListFilter{TenantID: tenant, LarkBound: &bound})
	if err != nil {
		t.Fatalf("ListPage bound: %v", err)
	}
	if page.Total != 1 || page.Rows[0].EmpID != ids[0] {
		t.Fatalf("已绑定过滤应只命中 alpha, got total=%d rows=%+v", page.Total, page.Rows)
	}
	unbound := false
	page, err = svc.ListPage(ctx, ListFilter{TenantID: tenant, LarkBound: &unbound})
	if err != nil {
		t.Fatalf("ListPage unbound: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("未绑定过滤应命中 bravo 与 charlie 两名, got total=%d rows=%+v", page.Total, page.Rows)
	}

	// 分页：total 是过滤后的总数，Rows 只是一页。
	page, err = svc.ListPage(ctx, ListFilter{TenantID: tenant, PageNum: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListPage paging: %v", err)
	}
	if page.Total != 3 || len(page.Rows) != 2 {
		t.Fatalf("分页应为 total=3 一页 2 行, got total=%d len=%d", page.Total, len(page.Rows))
	}
	if page.Rows[0].EmpID <= page.Rows[1].EmpID {
		t.Fatalf("列表应按 EmpID 倒序, got %d then %d", page.Rows[0].EmpID, page.Rows[1].EmpID)
	}
}

// TestListPageLoadsIdentitiesOnce 验证列表的飞书身份装配恒为一次查询（防 N+1），
// 且只装配 own 身份。
func TestListPageLoadsIdentitiesOnce(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	appID := fmt.Sprintf("cli_once_%d", tenant)
	seeds := []adminSeed{
		{key: "one", name: "OnceOne" + fmt.Sprint(tenant), status: empcap.StatusActive,
			identities: []struct{ app, openID, source string }{
				{appID, fmt.Sprintf("ou_one_own_%d", tenant), empcap.IDSourceOwn},
				{appID, fmt.Sprintf("ou_one_assoc_%d", tenant), empcap.IDSourceAssoc},
			}},
		{key: "two", name: "OnceTwo" + fmt.Sprint(tenant), status: empcap.StatusActive,
			identities: []struct{ app, openID, source string }{{appID, fmt.Sprintf("ou_two_%d", tenant), empcap.IDSourceOwn}}},
		{key: "three", name: "OnceThree" + fmt.Sprint(tenant), status: empcap.StatusActive},
	}
	seedAdminEmployees(t, ctx, tenant, svc, seeds)

	var page *AdminPage
	sqls, err := gdb.CatchSQL(ctx, func(ctx context.Context) error {
		var runErr error
		page, runErr = svc.ListPage(ctx, ListFilter{TenantID: tenant})
		return runErr
	})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if n := countTableQueries(sqls, dao.LarkIdentity.Table()); n != 1 {
		t.Fatalf("want exactly 1 lark_identity query, got %d: %v", n, sqls)
	}
	if page.Total != 3 {
		t.Fatalf("want 3 rows, got %d", page.Total)
	}
	for _, row := range page.Rows {
		for _, ident := range row.Identities {
			if ident.IDSource != empcap.IDSourceOwn {
				t.Fatalf("列表行 %d 只应装配 own 身份, got %+v", row.EmpID, ident)
			}
		}
	}
}

// TestGetDetailReturnsAllIdentities 验证详情返回全部身份（含 assoc）与部门，
// 员工不存在时返回 ErrEmployeeNotFound。
func TestGetDetailReturnsAllIdentities(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	appID := fmt.Sprintf("cli_detail_%d", tenant)
	no := uniqueEmployeeNo("detail", tenant)
	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{
		Name: "DetailTarget", EmployeeNo: no,
		Departments: []empcap.Department{{LarkDepartmentID: "od-detail", IsPrimary: true}},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err = svc.UpsertLarkIdentity(ctx, appID, fmt.Sprintf("ou_detail_own_%d", tenant), "uid-own", empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("seed own identity: %v", err)
	}
	if err = svc.UpsertLarkIdentity(ctx, appID, fmt.Sprintf("ou_detail_assoc_%d", tenant), "", empcap.IDSourceAssoc, id); err != nil {
		t.Fatalf("seed assoc identity: %v", err)
	}

	detail, err := svc.GetDetail(ctx, id)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.EmpID != id || detail.EmployeeNo != no || detail.Name != "DetailTarget" {
		t.Fatalf("详情基本字段不符: %+v", detail.AdminRow)
	}
	if len(detail.Identities) != 2 {
		t.Fatalf("详情应返回 own + assoc 两条身份, got %+v", detail.Identities)
	}
	if len(detail.Departments) != 1 || detail.Departments[0].LarkDepartmentID != "od-detail" {
		t.Fatalf("详情部门不符: %+v", detail.Departments)
	}

	if _, err = svc.GetDetail(ctx, 99999999); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("不存在的员工应返回 ErrEmployeeNotFound, got %v", err)
	}
}

// TestBindLarkIdentityOwnership 验证手工绑定的归属规则：新建成功、本人重复绑定
// 幂等并刷新 user_id、他人已占用时拒绝且不改原行。
func TestBindLarkIdentityOwnership(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	appID := fmt.Sprintf("cli_bind_%d", tenant)
	openID := fmt.Sprintf("ou_bind_%d", tenant)
	a, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "BindA", EmployeeNo: uniqueEmployeeNo("bind-a", tenant)})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "BindB", EmployeeNo: uniqueEmployeeNo("bind-b", tenant)})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}

	// 新建绑定。
	if err = svc.BindLarkIdentity(ctx, a, appID, openID, "uid-1"); err != nil {
		t.Fatalf("bind new: %v", err)
	}
	if got := identitySource(t, ctx, appID, openID); got != empcap.IDSourceOwn {
		t.Fatalf("手工绑定应写 id_source=%q, got %q", empcap.IDSourceOwn, got)
	}

	// 本人重复绑定：幂等，并刷新 user_id。
	if err = svc.BindLarkIdentity(ctx, a, appID, openID, "uid-2"); err != nil {
		t.Fatalf("rebind same emp should be idempotent: %v", err)
	}
	if got := identityUserID(t, ctx, appID, openID); got != "uid-2" {
		t.Fatalf("重复绑定应刷新 user_id, got %q", got)
	}

	// 他人已占用：拒绝且不改原归属。
	if err = svc.BindLarkIdentity(ctx, b, appID, openID, "uid-3"); !errors.Is(err, empcap.ErrLarkIdentityTaken) {
		t.Fatalf("绑定他人身份应返回 ErrLarkIdentityTaken, got %v", err)
	}
	if got := identityEmpID(t, ctx, appID, openID); got != a {
		t.Fatalf("被拒后原归属不应变化, want %d got %d", a, got)
	}

	// 不存在的员工：拒绝。
	if err = svc.BindLarkIdentity(ctx, 99999999, appID, "ou_ghost", ""); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("不存在的员工应返回 ErrEmployeeNotFound, got %v", err)
	}
	// 空参数：拒绝。
	if err = svc.BindLarkIdentity(ctx, a, "", openID, ""); !errors.Is(err, empcap.ErrInvalidParameter) {
		t.Fatalf("缺应用应返回 ErrInvalidParameter, got %v", err)
	}
}

// TestUnbindLarkIdentityOnlyOwnRows 验证解绑只删该员工名下的身份：非本人员下的
// 身份返回 ErrLarkIdentityNotFound 且不删除。
func TestUnbindLarkIdentityOnlyOwnRows(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	appID := fmt.Sprintf("cli_unbind_%d", tenant)
	openID := fmt.Sprintf("ou_unbind_%d", tenant)
	a, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "UnbindA", EmployeeNo: uniqueEmployeeNo("unbind-a", tenant)})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "UnbindB", EmployeeNo: uniqueEmployeeNo("unbind-b", tenant)})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err = svc.BindLarkIdentity(ctx, a, appID, openID, "uid-a"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// 非本人：拒绝，且原文不删。
	if err = svc.UnbindLarkIdentity(ctx, b, appID, openID); !errors.Is(err, empcap.ErrLarkIdentityNotFound) {
		t.Fatalf("解绑他人身份应返回 ErrLarkIdentityNotFound, got %v", err)
	}
	if got := identityEmpID(t, ctx, appID, openID); got != a {
		t.Fatalf("被拒后原映射应保留, want %d got %d", a, got)
	}

	// 本人：删除成功，再解绑返回未找到。
	if err = svc.UnbindLarkIdentity(ctx, a, appID, openID); err != nil {
		t.Fatalf("unbind own: %v", err)
	}
	if got := identityEmpID(t, ctx, appID, openID); got != 0 {
		t.Fatalf("解绑后不应再有该映射, got emp_id=%d", got)
	}
	if err = svc.UnbindLarkIdentity(ctx, a, appID, openID); !errors.Is(err, empcap.ErrLarkIdentityNotFound) {
		t.Fatalf("重复解绑应返回 ErrLarkIdentityNotFound, got %v", err)
	}
}

// TestSetSysUserExclusive 验证关联后台账号的独占性与取消：同一账号不能关联第二名
// 员工，置 0 表示取消关联。
func TestSetSysUserExclusive(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	a, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "SysA", EmployeeNo: uniqueEmployeeNo("sys-a", tenant)})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "SysB", EmployeeNo: uniqueEmployeeNo("sys-b", tenant)})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}
	sysUserID := 700000 + tenant

	if err = svc.SetSysUser(ctx, a, sysUserID); err != nil {
		t.Fatalf("link sys user: %v", err)
	}
	if got := sysUserIDOf(t, ctx, a); got != sysUserID {
		t.Fatalf("sys_user_id 应为 %d, got %d", sysUserID, got)
	}
	// 同一账号关联另一名员工：拒绝。
	if err = svc.SetSysUser(ctx, b, sysUserID); !errors.Is(err, empcap.ErrSysUserTaken) {
		t.Fatalf("重复关联应返回 ErrSysUserTaken, got %v", err)
	}
	// 同一员工重复设置同一账号：幂等成功（排除自身后不计占用）。
	if err = svc.SetSysUser(ctx, a, sysUserID); err != nil {
		t.Fatalf("同一员工重复关联应幂等: %v", err)
	}
	// 取消关联。
	if err = svc.SetSysUser(ctx, a, 0); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if got := sysUserIDOf(t, ctx, a); got != 0 {
		t.Fatalf("取消关联后 sys_user_id 应为 0, got %d", got)
	}
}

// countTableQueries 统计 SQL 列表中命中某表的条数。
func countTableQueries(sqls []string, table string) int {
	n := 0
	for _, s := range sqls {
		if strings.Contains(s, table) {
			n++
		}
	}
	return n
}

// identitySource 返回某条身份映射的 id_source；行不存在时返回空串。
func identitySource(t *testing.T, ctx context.Context, appID, openID string) string {
	t.Helper()
	row := identityRow(t, ctx, appID, openID)
	if row == nil {
		return ""
	}
	return row.IdSource
}

// identityUserID 返回某条身份映射的 user_id；行不存在时返回空串。
func identityUserID(t *testing.T, ctx context.Context, appID, openID string) string {
	t.Helper()
	row := identityRow(t, ctx, appID, openID)
	if row == nil {
		return ""
	}
	return row.LarkUserId
}

// identityEmpID 返回某条身份映射归属的 EmpID；行不存在时返回 0。
func identityEmpID(t *testing.T, ctx context.Context, appID, openID string) int64 {
	t.Helper()
	row := identityRow(t, ctx, appID, openID)
	if row == nil {
		return 0
	}
	return row.EmpId
}

// identityRow 直接读库取一条身份映射，避免借助被测方法自证。
func identityRow(t *testing.T, ctx context.Context, appID, openID string) *entity.LarkIdentity {
	t.Helper()
	var row *entity.LarkIdentity
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: appID, LarkOpenId: openID}).
		Scan(&row); err != nil {
		t.Fatalf("load identity %s/%s: %v", appID, openID, err)
	}
	return row
}

// sysUserIDOf 返回某员工当前的 sys_user_id。
func sysUserIDOf(t *testing.T, ctx context.Context, empID int64) int {
	t.Helper()
	row, err := dao.Employee.Ctx(ctx).Where(do.Employee{Id: empID}).One()
	if err != nil {
		t.Fatalf("load employee %d: %v", empID, err)
	}
	if row.IsEmpty() {
		t.Fatalf("employee %d not found", empID)
	}
	return row[dao.Employee.Columns().SysUserId].Int()
}
