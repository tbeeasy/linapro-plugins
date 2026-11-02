// Package testkit 为 linapro-eval-360 的集成测试提供共享环境自举：
// 真实 PostgreSQL 连接、生产 schema 建表、唯一租户分配与租户级清理。
//
// 本包不含任何断言，只提供「让跨服务全链路集成测试可独立、无序运行」的公共能力，
// 供 11.2 集成测试与后续同类测试复用（见 .agents/rules/testing.md 的单测自包含要求）。
package testkit

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	_ "lina-core/pkg/dbdriver"
)

// envDatabaseDSN 是集成测试使用的数据库连接串环境变量。
// 未设置时回退到仓库约定的本地开发库（与 hack/config.yaml 一致）。
const envDatabaseDSN = "LINA_TEST_DATABASE_DSN"

// defaultDatabaseDSN 是本地开发库的连接串回退值。
const defaultDatabaseDSN = "pgsql:postgres:postgres@tcp(127.0.0.1:5432)/linapro?sslmode=disable"

// Setup 把包测试指向真实数据库、确保生产 schema 已建表，并注册关闭与配置还原的清理。
// 每个调用 Setup 的测试都会得到独立、无序安全的数据库环境。
func Setup(t *testing.T) context.Context {
	t.Helper()

	dsn := strings.TrimSpace(getenv(envDatabaseDSN))
	if dsn == "" {
		dsn = defaultDatabaseDSN
	}

	originalConfig := gdb.GetAllConfig()
	if err := gdb.SetConfig(gdb.Config{
		gdb.DefaultGroupName: gdb.ConfigGroup{{Link: dsn}},
	}); err != nil {
		t.Fatalf("配置集成测试数据库失败：%v", err)
	}

	ctx := context.Background()
	db := g.DB()
	schema := productionSchema(t)
	if _, err := db.Exec(ctx, schema); err != nil {
		t.Fatalf("建表失败（schema 与 manifest/sql/001-linapro-eval-360-schema.sql 同源）：%v", err)
	}
	if err := ensureEmployeeTables(ctx); err != nil {
		t.Fatalf("建员工表失败：%v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(ctx); err != nil {
			t.Errorf("关闭集成测试数据库失败：%v", err)
		}
		if err := gdb.SetConfig(originalConfig); err != nil {
			t.Errorf("还原数据库配置失败：%v", err)
		}
	})
	return ctx
}

// productionSchema 读取随插件交付的生产建表脚本（单一事实来源，避免测试 DDL 漂移）。
// 测试二进制的工作目录是包目录，故按 runtime.Caller 定位插件根下的 manifest/sql。
func productionSchema(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码路径")
	}
	// <plugin-root>/backend/internal/service/testkit/testkit.go → <plugin-root>
	pluginRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	schemaPath := filepath.Join(pluginRoot, "manifest", "sql", "001-linapro-eval-360-schema.sql")
	data, err := readFile(schemaPath)
	if err != nil {
		t.Fatalf("读取生产 schema 失败（%s）：%v", schemaPath, err)
	}
	// 建表脚本不含事务包裹，直接执行即可；pg 驱动按分号切分多条语句。
	return data
}

// ensureEmployeeTables 确保 empcap 员工表存在（工号解析依赖真实员工行）。
// 与 linapro-employee-core 集成测试保持同一 DDL 契约。
func ensureEmployeeTables(ctx context.Context) error {
	_, err := g.DB().Exec(ctx, `
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
CREATE TABLE IF NOT EXISTS plugin_linapro_employee_core_lark_identity (
    "lark_app_id"  VARCHAR(128) NOT NULL,
    "lark_open_id" VARCHAR(191) NOT NULL,
    "lark_user_id" VARCHAR(128) NOT NULL DEFAULT '',
    "id_source"    VARCHAR(16)  NOT NULL DEFAULT 'own',
    "emp_id"       BIGINT NOT NULL,
    "created_at"   TIMESTAMPTZ,
    PRIMARY KEY ("lark_app_id", "lark_open_id")
);
`)
	return err
}

// Sheet 是单张工作表的解析结果（表头 + 数据行）。
type Sheet struct {
	Header []string
	Rows   [][]string
}

// ReadXLSXSheet 解析 xlsx 字节流为单张工作表的内存结构：
// 前导列与题目列一律按字符串比对（贴合导出断言：列结构 + 单元格文案）。
func ReadXLSXSheet(data []byte) (*Sheet, error) {
	parsed, err := parseXLSX(data)
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("xlsx 无工作表")
	}
	return parsed[0], nil
}

// expect 断言辅助（本包不引 testify，保持依赖最小）。
func expectEqual(t *testing.T, label string, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s 期望 %q，实际 %q", label, want, got)
	}
}

// Equal 断言两个字符串相等（导出测试的常用断言）。
func Equal(t *testing.T, label, got, want string) { expectEqual(t, label, got, want) }
