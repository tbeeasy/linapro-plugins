// employee_integration_test.go 针对真实 PostgreSQL 数据库验证员工写入网关：
//
//   - UpsertEmployee 新增 + 按 (tenant_id, moka_employee_no) 去重
//   - UpsertEmployee 档案覆盖
//   - 多部门 / 主部门映射与替换
//   - GetByEmployeeNo 与 GetByName 查询
//   - lark_identity upsert + 解析（含跨企业同一 emp_id）
//   - 批量读接口的部门一次装配（查询次数断言，防 N+1）
//   - UpsertEmployee 恢复在职 / 不改离职状态
//   - 空部门不清空既有部门；lark_identity 原子 upsert（含并发）
//
// 只有真实数据库才能证明 (tenant_id, moka_employee_no) 存活唯一索引的
// 去重、部门替换、事务写入以及 lark_identity 主键 upsert。
//
// 约定（见 linapro-extlogin-core 身份测试）：每个测试都配置本地 PostgreSQL
// 数据库、确保表存在、用唯一租户 id 隔离自身并清理自己的行。
package employee

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	_ "lina-core/pkg/dbdriver"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// tenantSeq 为每个测试分配唯一租户 id，使测试即使面对共享本地数据库
// 也保持独立与无序。较高的基数避免与真实/种子租户冲突。
var tenantSeq int64 = 900000

func nextTenant() int { return int(atomic.AddInt64(&tenantSeq, 1)) }

func uniqueEmployeeNo(prefix string, tenant int) string {
	return fmt.Sprintf("EMP-%s-%d", prefix, tenant)
}

// configureEmployeeTestDB 把包测试指向本地 PostgreSQL 数据库，
// 并确保插件拥有的表存在。DDL 与当前 schema 一致：
// employee 没有 lark_open_id，身份位于 lark_identity。
func configureEmployeeTestDB(t *testing.T, ctx context.Context) {
	t.Helper()
	originalConfig := gdb.GetAllConfig()
	if err := gdb.SetConfig(gdb.Config{
		gdb.DefaultGroupName: gdb.ConfigGroup{{Link: "pgsql:postgres:postgres@tcp(127.0.0.1:5432)/linapro?sslmode=disable"}},
	}); err != nil {
		t.Fatalf("configure employee test database failed: %v", err)
	}
	db := g.DB()
	if _, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_employee (
    "id"               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "tenant_id"        INT NOT NULL DEFAULT 0,
    "name"             VARCHAR(191) NOT NULL DEFAULT '',
    "moka_employee_no" VARCHAR(64)  NOT NULL DEFAULT '',
    "status"           SMALLINT NOT NULL DEFAULT 1,
    "sys_user_id"      INT NOT NULL DEFAULT 0,
    "created_at"       TIMESTAMPTZ,
    "updated_at"       TIMESTAMPTZ,
    "deleted_at"       TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_linapro_employee_core_emp_tenant_moka_no
    ON plugin_linapro_employee_core_employee ("tenant_id", "moka_employee_no")
    WHERE "deleted_at" IS NULL AND "moka_employee_no" <> '';
CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_employee_dept (
    "id"                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    "tenant_id"          INT NOT NULL DEFAULT 0,
    "emp_id"             BIGINT NOT NULL,
    "lark_department_id" VARCHAR(191) NOT NULL,
    "is_primary"         SMALLINT NOT NULL DEFAULT 0,
    "created_at"         TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_linapro_employee_core_dept_emp_dept
    ON plugin_linapro_employee_core_employee_dept ("emp_id", "lark_department_id");
CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_lark_identity (
    "lark_app_id"  VARCHAR(128) NOT NULL,
    "lark_open_id" VARCHAR(191) NOT NULL,
    "lark_user_id" VARCHAR(128) NOT NULL DEFAULT '',
    "id_source"    VARCHAR(16)  NOT NULL DEFAULT 'own',
    "emp_id"       BIGINT NOT NULL,
    "created_at"   TIMESTAMPTZ,
    PRIMARY KEY ("lark_app_id", "lark_open_id")
);
-- 兼容既有测试库：表可能由早前版本创建、缺少后续迁移列。CREATE TABLE IF NOT EXISTS
-- 不会补列，且 GoFrame 会静默丢弃引用不存在列的 Where 条件（导致 id_source 过滤失效），
-- 故显式 ADD COLUMN IF NOT EXISTS 对齐 003/004 迁移。
ALTER TABLE plugin_linapro_employee_core_lark_identity ADD COLUMN IF NOT EXISTS "lark_user_id" VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plugin_linapro_employee_core_lark_identity ADD COLUMN IF NOT EXISTS "id_source" VARCHAR(16) NOT NULL DEFAULT 'own';
`); err != nil {
		t.Fatalf("ensure employee test tables failed: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(ctx); err != nil {
			t.Errorf("close employee test database failed: %v", err)
		}
		if err := gdb.SetConfig(originalConfig); err != nil {
			t.Errorf("restore employee test database config failed: %v", err)
		}
	})
}

// cleanupTenant 删除某租户拥有的所有员工、部门与飞书身份行。
//
// lark_identity 以 emp_id 关联、没有 tenant_id 列，故先按该租户员工的 EmpID
// 删身份行再删员工。清理在「登记时立即执行一次」+「测试结束再执行一次」：
// nextTenant 每次进程启动都从 900000 起数，同一测试跨轮次会拿到相同租户号，
// 只清尾部的话上一轮残留的身份行会占用主键，让本轮绑定撞上「已属于其他员工」。
func cleanupTenant(t *testing.T, ctx context.Context, tenantID int) {
	t.Helper()
	purge := func() {
		empIDs, err := dao.Employee.Ctx(ctx).
			Unscoped().
			Where(do.Employee{TenantId: tenantID}).
			Fields(dao.Employee.Columns().Id).
			Array()
		if err != nil {
			t.Errorf("list employee ids for cleanup failed: %v", err)
		}
		for _, empID := range empIDs {
			if _, err := dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{EmpId: empID}).Delete(); err != nil {
				t.Errorf("cleanup lark identity rows failed: %v", err)
			}
		}
		// 再扫一遍孤儿身份行：早期版本的清理只删员工不删身份，留下的行指向
		// 已不存在的 EmpID，按租户找不回来，但会占住 (app_id, open_id) 主键。
		if _, err := dao.LarkIdentity.Ctx(ctx).
			Where("emp_id NOT IN (SELECT id FROM " + dao.Employee.Table() + ")").
			Delete(); err != nil {
			t.Errorf("cleanup orphan lark identity rows failed: %v", err)
		}
		if _, err := dao.EmployeeDept.Ctx(ctx).Where(do.EmployeeDept{TenantId: tenantID}).Delete(); err != nil {
			t.Errorf("cleanup department rows failed: %v", err)
		}
		if _, err := dao.Employee.Ctx(ctx).Unscoped().Where(do.Employee{TenantId: tenantID}).Delete(); err != nil {
			t.Errorf("cleanup employee rows failed: %v", err)
		}
	}
	purge()
	t.Cleanup(purge)
}

// countLiveByEmployeeNo 返回 (tenant, employeeNo) 对应的存活员工行数。
func countLiveByEmployeeNo(t *testing.T, ctx context.Context, tenantID int, employeeNo string) int {
	t.Helper()
	n, err := dao.Employee.Ctx(ctx).Where(do.Employee{TenantId: tenantID, MokaEmployeeNo: employeeNo}).Count()
	if err != nil {
		t.Fatalf("count employees by employee_no: %v", err)
	}
	return n
}

// TestUpsertDedupsByEmployeeNo 验证去重不变式：对相同 (tenant_id, employee_no)
// 的两次 UpsertEmployee 调用产生恰好一行存活记录，且第二次调用返回相同的
// EmpID 并覆盖档案 —— 从而验证存活 (tenant_id, moka_employee_no) 唯一索引。
func TestUpsertDedupsByEmployeeNo(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("dedup", tenant)
	svc := New()

	id1, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "First", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected a created EmpID on first upsert")
	}

	id2, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Updated", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("second upsert must reuse EmpID %d, got %d", id1, id2)
	}
	if n := countLiveByEmployeeNo(t, ctx, tenant, empNo); n != 1 {
		t.Fatalf("expected exactly one live row (unique index on employee_no), got %d", n)
	}

	got, err := svc.GetByID(ctx, id1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Updated" {
		t.Fatalf("expected upsert to overwrite name to Updated, got %q", got.Name)
	}
}

// TestUpsertRequiresEmployeeNo 验证空 employee_no 被拒绝 ——
// 同步是唯一写入方且总是提供 Moka 员工工号。
func TestUpsertRequiresEmployeeNo(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	if _, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "NoNo"}); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("expected ErrEmployeeNotFound for empty employee_no, got %v", err)
	}
}

// TestDepartmentsMappedWithPrimaryAndReplaced 验证多部门档案
// 以正确的主部门标志持久化，且随后使用不同部门集合的 upsert
// 会替换（而非合并）之前的行。
func TestDepartmentsMappedWithPrimaryAndReplaced(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("depts", tenant)
	svc := New()

	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{
		Name:       "Multi",
		EmployeeNo: empNo,
		Departments: []empcap.Department{
			{LarkDepartmentID: "dpt_secondary", IsPrimary: false},
			{LarkDepartmentID: "dpt_primary", IsPrimary: true},
		},
	})
	if err != nil {
		t.Fatalf("upsert with departments: %v", err)
	}

	got, err := svc.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Departments) != 2 {
		t.Fatalf("expected 2 departments, got %d", len(got.Departments))
	}
	primary := map[string]bool{}
	for _, d := range got.Departments {
		primary[d.LarkDepartmentID] = d.IsPrimary
	}
	if primary["dpt_primary"] != true || primary["dpt_secondary"] != false {
		t.Fatalf("primary flags mismatch: %+v", got.Departments)
	}

	// 后续同步覆盖部门：旧集合必须被替换。
	if _, err = svc.UpsertEmployee(ctx, tenant, empcap.Profile{
		Name:        "Multi",
		EmployeeNo:  empNo,
		Departments: []empcap.Department{{LarkDepartmentID: "dpt_only", IsPrimary: true}},
	}); err != nil {
		t.Fatalf("upsert replace departments: %v", err)
	}
	got, err = svc.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	if len(got.Departments) != 1 || got.Departments[0].LarkDepartmentID != "dpt_only" || !got.Departments[0].IsPrimary {
		t.Fatalf("expected departments replaced with one primary dpt_only, got %+v", got.Departments)
	}
}

// TestGetByEmployeeNoAndGetByName 验证 report-sync 和 recruit-pipeline
// 消费方使用的查询方法。
func TestGetByEmployeeNoAndGetByName(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("query", tenant)
	svc := New()

	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "QueryTarget", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	byNo, err := svc.GetByEmployeeNo(ctx, tenant, empNo)
	if err != nil {
		t.Fatalf("GetByEmployeeNo: %v", err)
	}
	if byNo.EmpID != id || byNo.EmployeeNo != empNo {
		t.Fatalf("GetByEmployeeNo returned unexpected row: %+v", byNo)
	}

	byName, err := svc.GetByName(ctx, tenant, "QueryTarget")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if byName.EmpID != id || byName.Name != "QueryTarget" {
		t.Fatalf("GetByName returned unexpected row: %+v", byName)
	}

	if _, err = svc.GetByEmployeeNo(ctx, tenant, "no-such-no"); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("GetByEmployeeNo miss: expected ErrEmployeeNotFound, got %v", err)
	}
	if _, err = svc.GetByName(ctx, tenant, "No Such Name"); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("GetByName miss: expected ErrEmployeeNotFound, got %v", err)
	}
}

// TestMapByEmployeeNosAndMapByEmpIDs 验证 eval-360 消费的两个批量只读查询：
// MapByEmployeeNos 按 (tenant, 工号集合) 一次性返回多员工；MapByEmpIDs 按
// EmpID 集合一次性返回多员工。两者都不排除离职员工，未命中的键不入 map，
// 空集合返回空 map（不报错）。
func TestMapByEmployeeNosAndMapByEmpIDs(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	noA := uniqueEmployeeNo("batch-a", tenant)
	noB := uniqueEmployeeNo("batch-b", tenant)
	idA, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "BatchA", EmployeeNo: noA})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}
	idB, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "BatchB", EmployeeNo: noB})
	if err != nil {
		t.Fatalf("seed B: %v", err)
	}

	// MapByEmployeeNos：命中两条，未命中工号不入 map。
	byNos, err := svc.MapByEmployeeNos(ctx, tenant, []string{noA, noB, "no-such-no"})
	if err != nil {
		t.Fatalf("MapByEmployeeNos: %v", err)
	}
	if len(byNos) != 2 {
		t.Fatalf("want 2 mapped by employee no, got %d", len(byNos))
	}
	if emp := byNos[noA]; emp == nil || emp.EmpID != idA || emp.Name != "BatchA" {
		t.Fatalf("MapByEmployeeNos[%s] = %+v, want emp %d BatchA", noA, emp, idA)
	}
	if emp := byNos[noB]; emp == nil || emp.EmpID != idB {
		t.Fatalf("MapByEmployeeNos[%s] = %+v, want emp %d", noB, emp, idB)
	}

	// MapByEmpIDs：命中两条，未命中 EmpID 不入 map。
	byIDs, err := svc.MapByEmpIDs(ctx, []int64{idA, idB, 999999999})
	if err != nil {
		t.Fatalf("MapByEmpIDs: %v", err)
	}
	if len(byIDs) != 2 {
		t.Fatalf("want 2 mapped by emp id, got %d", len(byIDs))
	}
	if emp := byIDs[idA]; emp == nil || emp.EmployeeNo != noA {
		t.Fatalf("MapByEmpIDs[%d] = %+v, want employee_no %s", idA, emp, noA)
	}
	if emp := byIDs[idB]; emp == nil || emp.EmployeeNo != noB {
		t.Fatalf("MapByEmpIDs[%d] = %+v, want employee_no %s", idB, emp, noB)
	}

	// 空集合直接返回空 map，不报错。
	emptyNos, err := svc.MapByEmployeeNos(ctx, tenant, nil)
	if err != nil || len(emptyNos) != 0 {
		t.Fatalf("empty MapByEmployeeNos want empty map no err, got %v (%v)", emptyNos, err)
	}
	emptyIDs, err := svc.MapByEmpIDs(ctx, nil)
	if err != nil || len(emptyIDs) != 0 {
		t.Fatalf("empty MapByEmpIDs want empty map no err, got %v (%v)", emptyIDs, err)
	}
}

// TestLarkIdentityUpsertAndResolve 验证 lark_identity 往返：已注册的
// (lark_app_id, open_id) 解析为其 emp_id；同一个人出现在两个飞书企业时
// 会映射两个身份到同一 emp_id；重复 upsert 更新目标；未注册组合返回
// ErrEmployeeNotFound。
func TestLarkIdentityUpsertAndResolve(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("identity", tenant)
	svc := New()

	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "IdTarget", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("seed employee: %v", err)
	}
	appF := fmt.Sprintf("cli_f_%d", tenant)
	appG := fmt.Sprintf("cli_g_%d", tenant)
	openF := fmt.Sprintf("ou_f_%d", tenant)
	openG := fmt.Sprintf("ou_g_%d", tenant)
	userF := fmt.Sprintf("uid_f_%d", tenant)
	userG := fmt.Sprintf("uid_g_%d", tenant)
	t.Cleanup(func() {
		_, _ = dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{EmpId: id}).Delete()
	})

	if err := svc.UpsertLarkIdentity(ctx, appF, openF, userF, empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("upsert identity F: %v", err)
	}
	if err := svc.UpsertLarkIdentity(ctx, appG, openG, userG, empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("upsert identity G: %v", err)
	}

	gotF, err := svc.ResolveEmpIDByLarkIdentity(ctx, appF, openF)
	if err != nil || gotF != id {
		t.Fatalf("resolve F: want %d, got %d err %v", id, gotF, err)
	}
	gotG, err := svc.ResolveEmpIDByLarkIdentity(ctx, appG, openG)
	if err != nil || gotG != id {
		t.Fatalf("resolve G: want %d, got %d err %v", id, gotG, err)
	}

	// 按 (app_id, user_id) 双键反查应命中同一 emp_id（考勤等按 user_id 推送的入口）。
	byUIDF, err := svc.ResolveEmpIDByLarkUserID(ctx, appF, userF)
	if err != nil || byUIDF != id {
		t.Fatalf("resolve by user_id F: want %d, got %d err %v", id, byUIDF, err)
	}

	// 对相同键重复 upsert 更新目标 emp_id 与 user_id。
	if err := svc.UpsertLarkIdentity(ctx, appF, openF, userF, empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("re-upsert identity F: %v", err)
	}
	n, err := dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{LarkAppId: appF, LarkOpenId: openF}).Count()
	if err != nil {
		t.Fatalf("count identity F: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one identity row for (appF, openF), got %d", n)
	}

	if _, err := svc.ResolveEmpIDByLarkIdentity(ctx, appF, "ou_missing"); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("resolve miss: expected ErrEmployeeNotFound, got %v", err)
	}

	// 空 user_id 反查按失败关闭处理（绝不误命中）。
	if _, err := svc.ResolveEmpIDByLarkUserID(ctx, appF, ""); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("resolve empty user_id: expected ErrEmployeeNotFound, got %v", err)
	}
}

// TestLarkIdentityIDSourceAndAppScope 验证 id_source 语义与按 app 作用域解析：
//   - own 行可登录/反查；assoc 行落库但不参与 ResolveEmpIDByLarkIdentity / ResolveEmpIDByLarkUserID
//     （fail-closed）；
//   - MapLarkOpenIDsByEmployeeNo 按 lark_app_id 过滤，返回该作用域下 own+assoc 全部 open_id，
//     不混入其它 app 作用域的 id。
func TestLarkIdentityIDSourceAndAppScope(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("idsrc", tenant)
	svc := New()

	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "SrcTarget", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("seed employee: %v", err)
	}
	appA := fmt.Sprintf("cli_a_%d", tenant)
	appB := fmt.Sprintf("cli_b_%d", tenant)
	ownOpen := fmt.Sprintf("ou_own_%d", tenant)
	assocOpen := fmt.Sprintf("ou_assoc_%d", tenant)
	otherOpen := fmt.Sprintf("ou_other_%d", tenant)
	t.Cleanup(func() {
		_, _ = dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{EmpId: id}).Delete()
	})

	// appA 下 own + assoc 两行；appB 下另一 own 行（用于验证 app 作用域过滤）。
	if err := svc.UpsertLarkIdentity(ctx, appA, ownOpen, "uid_own", empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("upsert own: %v", err)
	}
	if err := svc.UpsertLarkIdentity(ctx, appA, assocOpen, "", empcap.IDSourceAssoc, id); err != nil {
		t.Fatalf("upsert assoc: %v", err)
	}
	if err := svc.UpsertLarkIdentity(ctx, appB, otherOpen, "uid_other", empcap.IDSourceOwn, id); err != nil {
		t.Fatalf("upsert other app: %v", err)
	}

	// own 行可登录解析；assoc 行不参与登录（fail-closed）。
	if got, err := svc.ResolveEmpIDByLarkIdentity(ctx, appA, ownOpen); err != nil || got != id {
		t.Fatalf("resolve own: want %d, got %d err %v", id, got, err)
	}
	if _, err := svc.ResolveEmpIDByLarkIdentity(ctx, appA, assocOpen); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("resolve assoc for login: expected ErrEmployeeNotFound, got %v", err)
	}

	// MapLarkOpenIDsByEmployeeNo(appA) 返回 own + assoc 两个 open_id，不含 appB 的 otherOpen。
	byEmpNo, err := svc.MapLarkOpenIDsByEmployeeNo(ctx, appA)
	if err != nil {
		t.Fatalf("MapLarkOpenIDsByEmployeeNo appA: %v", err)
	}
	got := map[string]bool{}
	for _, oid := range byEmpNo[empNo] {
		got[oid] = true
	}
	if !got[ownOpen] || !got[assocOpen] {
		t.Fatalf("want appA scope to include own+assoc open_id, got %v", byEmpNo[empNo])
	}
	if got[otherOpen] {
		t.Fatalf("appA scope must NOT include appB open_id %q, got %v", otherOpen, byEmpNo[empNo])
	}
	if len(byEmpNo[empNo]) != 2 {
		t.Fatalf("want exactly 2 open_ids in appA scope, got %v", byEmpNo[empNo])
	}

	// MapOwnLarkOpenIDsByEmployeeNo(appA) 仅返回 own 的 open_id，排除 assoc 与其它 app 作用域。
	ownByNo, err := svc.MapOwnLarkOpenIDsByEmployeeNo(ctx, appA)
	if err != nil {
		t.Fatalf("MapOwnLarkOpenIDsByEmployeeNo appA: %v", err)
	}
	if got := ownByNo[empNo]; got != ownOpen {
		t.Fatalf("want own-only open_id %q for appA, got %q (full=%v)", ownOpen, got, ownByNo)
	}
	// appA 的 own 映射绝不含 assoc 与其它 app 的 open_id。
	for _, oid := range ownByNo {
		if oid == assocOpen || oid == otherOpen {
			t.Fatalf("own-only mapping MUST NOT return assoc/other-app open_id, got %v", ownByNo)
		}
	}
	// appB 的 own 映射只命中 appB 的 otherOpen，不混入 appA 的。
	ownByNoB, err := svc.MapOwnLarkOpenIDsByEmployeeNo(ctx, appB)
	if err != nil {
		t.Fatalf("MapOwnLarkOpenIDsByEmployeeNo appB: %v", err)
	}
	if got := ownByNoB[empNo]; got != otherOpen {
		t.Fatalf("want appB own-only open_id %q, got %q", otherOpen, got)
	}
}

// countDeptQueries 统计 SQL 列表中访问部门表的语句条数。
func countDeptQueries(sqls []string) int {
	n := 0
	for _, s := range sqls {
		if strings.Contains(s, dao.EmployeeDept.Table()) {
			n++
		}
	}
	return n
}

// TestBatchQueriesLoadDepartmentsOnce 验证批量读接口的部门装配不随行数增长：
// MapByEmpIDs / MapByEmployeeNos / MapByEmployeeNosAnyTenant / ListActiveByTenant
// 对 3 名员工各只查 1 次部门表（此前每行 1 次，即 N+1），且部门按员工正确分组。
func TestBatchQueriesLoadDepartmentsOnce(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	seeds := []struct {
		no    string
		depts []empcap.Department
	}{
		{uniqueEmployeeNo("dept-a", tenant), []empcap.Department{{LarkDepartmentID: "d_a1", IsPrimary: true}, {LarkDepartmentID: "d_a2"}}},
		{uniqueEmployeeNo("dept-b", tenant), []empcap.Department{{LarkDepartmentID: "d_b1", IsPrimary: true}}},
		{uniqueEmployeeNo("dept-c", tenant), nil},
	}
	var (
		ids []int64
		nos []string
	)
	for _, sd := range seeds {
		id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: sd.no, EmployeeNo: sd.no, Departments: sd.depts})
		if err != nil {
			t.Fatalf("seed %s: %v", sd.no, err)
		}
		ids = append(ids, id)
		nos = append(nos, sd.no)
	}

	// 先预热一次，避免首次查询时 ORM 的表结构探测语句计入统计。
	if _, err := svc.MapByEmpIDs(ctx, ids); err != nil {
		t.Fatalf("warm up: %v", err)
	}

	assertDepts := func(name string, got *empcap.Employee, want []empcap.Department) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s: employee missing", name)
		}
		if len(got.Departments) != len(want) {
			t.Fatalf("%s: emp %d want %d departments, got %+v", name, got.EmpID, len(want), got.Departments)
		}
		for i := range want {
			if got.Departments[i] != want[i] {
				t.Fatalf("%s: emp %d department[%d] = %+v, want %+v", name, got.EmpID, i, got.Departments[i], want[i])
			}
		}
	}

	cases := []struct {
		name string
		run  func(ctx context.Context) (map[int64]*empcap.Employee, error)
	}{
		{"MapByEmpIDs", func(ctx context.Context) (map[int64]*empcap.Employee, error) {
			return svc.MapByEmpIDs(ctx, ids)
		}},
		{"MapByEmployeeNos", func(ctx context.Context) (map[int64]*empcap.Employee, error) {
			m, err := svc.MapByEmployeeNos(ctx, tenant, nos)
			return byEmpID(m), err
		}},
		{"MapByEmployeeNosAnyTenant", func(ctx context.Context) (map[int64]*empcap.Employee, error) {
			m, err := svc.MapByEmployeeNosAnyTenant(ctx, nos)
			return byEmpID(m), err
		}},
		// 种子员工的姓名即工号，故可直接以 nos 作为姓名集合。
		{"MapByNamesAnyTenant", func(ctx context.Context) (map[int64]*empcap.Employee, error) {
			m, err := svc.MapByNamesAnyTenant(ctx, nos)
			out := map[int64]*empcap.Employee{}
			for _, group := range m {
				for _, e := range group {
					out[e.EmpID] = e
				}
			}
			return out, err
		}},
		{"ListActiveByTenant", func(ctx context.Context) (map[int64]*empcap.Employee, error) {
			list, err := svc.ListActiveByTenant(ctx, tenant)
			out := map[int64]*empcap.Employee{}
			for _, e := range list {
				out[e.EmpID] = e
			}
			return out, err
		}},
	}
	for _, c := range cases {
		var got map[int64]*empcap.Employee
		sqls, err := gdb.CatchSQL(ctx, func(ctx context.Context) error {
			var runErr error
			got, runErr = c.run(ctx)
			return runErr
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n := countDeptQueries(sqls); n != 1 {
			t.Fatalf("%s: want exactly 1 department query for %d employees, got %d: %v", c.name, len(ids), n, sqls)
		}
		if len(got) != len(ids) {
			t.Fatalf("%s: want %d employees, got %d", c.name, len(ids), len(got))
		}
		for i, sd := range seeds {
			assertDepts(c.name, got[ids[i]], sd.depts)
		}
	}
}

// byEmpID 把「工号 → 员工」映射转成「EmpID → 员工」，便于统一断言。
func byEmpID(m map[string]*empcap.Employee) map[int64]*empcap.Employee {
	out := make(map[int64]*empcap.Employee, len(m))
	for _, e := range m {
		out[e.EmpID] = e
	}
	return out
}

// TestMapByNamesAnyTenant 验证按姓名跨租户批量查询：同名员工（跨租户、含离职）全部返回且按 EmpID
// 升序，未命中的姓名不入 map，空输入返回空 map。
func TestMapByNamesAnyTenant(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenantA, tenantB := nextTenant(), nextTenant()
	cleanupTenant(t, ctx, tenantA)
	cleanupTenant(t, ctx, tenantB)
	svc := New()

	shared := fmt.Sprintf("同名-%d", tenantA)
	single := fmt.Sprintf("独名-%d", tenantA)
	seed := func(tenant int, name, prefix string, departed bool) int64 {
		t.Helper()
		id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{
			Name: name, EmployeeNo: uniqueEmployeeNo(prefix, tenant), Departed: departed,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", prefix, err)
		}
		return id
	}
	idA := seed(tenantA, shared, "name-a", false)
	idB := seed(tenantB, shared, "name-b", true)
	idC := seed(tenantA, single, "name-c", false)

	got, err := svc.MapByNamesAnyTenant(ctx, []string{shared, single, "不存在的姓名-" + shared})
	if err != nil {
		t.Fatalf("MapByNamesAnyTenant: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 names in map, got %d: %+v", len(got), got)
	}
	group := got[shared]
	if len(group) != 2 || group[0].EmpID != idA || group[1].EmpID != idB {
		t.Fatalf("shared name want [%d %d] ordered by EmpID, got %+v", idA, idB, group)
	}
	if group[1].Status != empcap.StatusDeparted {
		t.Fatalf("departed homonym must be returned with departed status, got %d", group[1].Status)
	}
	if len(got[single]) != 1 || got[single][0].EmpID != idC {
		t.Fatalf("single name want [%d], got %+v", idC, got[single])
	}

	empty, err := svc.MapByNamesAnyTenant(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty input want empty map, got %+v err=%v", empty, err)
	}
}

// TestUpsertRestoresActiveStatus 验证 UpsertEmployee 的在职状态语义：
// 离职员工在 Moka 报在职（Departed=false）时被恢复为在职；Departed=true 不改动
// 已存在员工的状态；新建时 Departed=true 直接落离职。
func TestUpsertRestoresActiveStatus(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	statusOf := func(id int64) int {
		t.Helper()
		e, err := svc.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("get %d: %v", id, err)
		}
		return e.Status
	}

	empNo := uniqueEmployeeNo("rehire", tenant)
	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Rehire", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err = svc.MarkDeparted(ctx, tenant, id); err != nil {
		t.Fatalf("mark departed: %v", err)
	}

	// Moka 仍报离职：不改动状态，保持离职。
	if _, err = svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Rehire", EmployeeNo: empNo, Departed: true}); err != nil {
		t.Fatalf("upsert departed: %v", err)
	}
	if got := statusOf(id); got != empcap.StatusDeparted {
		t.Fatalf("Departed=true must not change status, got %d", got)
	}

	// Moka 改报在职（重新入职 / 误标纠正）：恢复在职，EmpID 不变。
	id2, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Rehire", EmployeeNo: empNo})
	if err != nil {
		t.Fatalf("upsert active: %v", err)
	}
	if id2 != id {
		t.Fatalf("expected same EmpID %d, got %d", id, id2)
	}
	if got := statusOf(id); got != empcap.StatusActive {
		t.Fatalf("Departed=false must restore active, got %d", got)
	}

	// 新建时 Moka 已报离职：直接落离职，不经过在职中间态。
	newID, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Gone", EmployeeNo: uniqueEmployeeNo("gone", tenant), Departed: true})
	if err != nil {
		t.Fatalf("create departed: %v", err)
	}
	if got := statusOf(newID); got != empcap.StatusDeparted {
		t.Fatalf("create with Departed=true want departed, got %d", got)
	}
}

// TestUpsertWithoutDepartmentsKeepsExisting 验证 Profile.Departments 为空表示
// 「本次不更新部门」：同步（Moka 不下发部门）不会清空既有部门行。
func TestUpsertWithoutDepartmentsKeepsExisting(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	empNo := uniqueEmployeeNo("keepdept", tenant)
	svc := New()

	id, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{
		Name:        "Keep",
		EmployeeNo:  empNo,
		Departments: []empcap.Department{{LarkDepartmentID: "d_keep", IsPrimary: true}},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err = svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "Keep Renamed", EmployeeNo: empNo}); err != nil {
		t.Fatalf("upsert without departments: %v", err)
	}
	got, err := svc.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Keep Renamed" {
		t.Fatalf("want name overwritten, got %q", got.Name)
	}
	if len(got.Departments) != 1 || got.Departments[0].LarkDepartmentID != "d_keep" {
		t.Fatalf("want existing department kept, got %+v", got.Departments)
	}
}

// TestUpsertLarkIdentityAtomic 验证 lark_identity 单条原子 upsert：
// 冲突时更新 emp_id / user_id / id_source 且保留首次 created_at；
// 多个并发写同一键全部成功、最终恰好一行（此前「先 Count 再插入」会撞主键）。
func TestUpsertLarkIdentityAtomic(t *testing.T) {
	ctx := context.Background()
	configureEmployeeTestDB(t, ctx)
	tenant := nextTenant()
	cleanupTenant(t, ctx, tenant)
	svc := New()

	idA, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "AtomA", EmployeeNo: uniqueEmployeeNo("atom-a", tenant)})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}
	idB, err := svc.UpsertEmployee(ctx, tenant, empcap.Profile{Name: "AtomB", EmployeeNo: uniqueEmployeeNo("atom-b", tenant)})
	if err != nil {
		t.Fatalf("seed B: %v", err)
	}
	app := fmt.Sprintf("cli_atom_%d", tenant)
	t.Cleanup(func() {
		if _, err := dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{LarkAppId: app}).Delete(); err != nil {
			t.Errorf("cleanup identities: %v", err)
		}
	})
	load := func(openID string) *entity.LarkIdentity {
		t.Helper()
		var row *entity.LarkIdentity
		if err := dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{LarkAppId: app, LarkOpenId: openID}).Scan(&row); err != nil {
			t.Fatalf("load identity: %v", err)
		}
		if row == nil {
			t.Fatalf("identity (%s, %s) missing", app, openID)
		}
		return row
	}

	// 首次插入，再以不同的 emp / user_id / id_source 覆盖同一键。
	openID := fmt.Sprintf("ou_atom_%d", tenant)
	if err = svc.UpsertLarkIdentity(ctx, app, openID, "uid_1", empcap.IDSourceOwn, idA); err != nil {
		t.Fatalf("insert: %v", err)
	}
	first := load(openID)
	if first.CreatedAt == nil {
		t.Fatal("want created_at filled on insert")
	}
	if err = svc.UpsertLarkIdentity(ctx, app, openID, "", empcap.IDSourceAssoc, idB); err != nil {
		t.Fatalf("update on conflict: %v", err)
	}
	second := load(openID)
	if second.EmpId != idB || second.LarkUserId != "" || second.IdSource != empcap.IDSourceAssoc {
		t.Fatalf("want row updated to emp=%d user_id='' source=assoc, got %+v", idB, second)
	}
	if second.CreatedAt == nil || !second.CreatedAt.Equal(*first.CreatedAt) {
		t.Fatalf("want created_at preserved %v, got %v", first.CreatedAt, second.CreatedAt)
	}

	// 并发写同一个新键：多轮、每轮所有协程在同一时刻放行，全部成功且最终恰好一行。
	const (
		rounds  = 20
		workers = 16
	)
	for round := 0; round < rounds; round++ {
		raceOpenID := fmt.Sprintf("ou_race_%d_%d", tenant, round)
		var (
			start = make(chan struct{})
			errs  = make(chan error, workers)
			wg    sync.WaitGroup
		)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- svc.UpsertLarkIdentity(ctx, app, raceOpenID, "uid_race", empcap.IDSourceOwn, idA)
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatalf("round %d: concurrent upsert failed: %v", round, e)
			}
		}
		n, err := dao.LarkIdentity.Ctx(ctx).Where(do.LarkIdentity{LarkAppId: app, LarkOpenId: raceOpenID}).Count()
		if err != nil {
			t.Fatalf("round %d: count: %v", round, err)
		}
		if n != 1 {
			t.Fatalf("round %d: want exactly 1 row after concurrent upserts, got %d", round, n)
		}
	}
}
