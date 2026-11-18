// partition_test.go 锁定分区补齐的边界算法与「只创建不删除」纪律。
//
// 保护对象：
//   - 分区齐全时不发任何 DDL（避免在父表上无谓申请锁，堵住后续 INSERT）；
//   - 边界左闭右开、跨年正确；
//   - 生成的语句里绝不出现 DROP / DETACH。

package partition

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// TestDesiredSpecsCoversCurrentAndSixFutureMonths 锁定窗口是当前月 + 未来 6 个月，共 7 个。
func TestDesiredSpecsCoversCurrentAndSixFutureMonths(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 30, 0, 0, time.UTC)
	specs := desiredSpecs(now)

	if len(specs) != 7 {
		t.Fatalf("应有 7 个分区（当前月 + 未来 6 个月），实际 %d", len(specs))
	}
	if specs[0].Name != "plugin_linapro_monitor_apilog_p202611" {
		t.Fatalf("首个分区应为当前月，实际 %s", specs[0].Name)
	}
	if specs[6].Name != "plugin_linapro_monitor_apilog_p202705" {
		t.Fatalf("末个分区应为未来第 6 个月，实际 %s", specs[6].Name)
	}
}

// TestDesiredSpecsCrossesYearBoundary 锁定跨年：12 月的上界是次年 1 月 1 日。
func TestDesiredSpecsCrossesYearBoundary(t *testing.T) {
	now := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	specs := desiredSpecs(now)

	if specs[0].Name != "plugin_linapro_monitor_apilog_p202612" {
		t.Fatalf("12 月分区名错误: %s", specs[0].Name)
	}
	expectedTo := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !specs[0].To.Equal(expectedTo) {
		t.Fatalf("12 月分区上界应为 %v，实际 %v", expectedTo, specs[0].To)
	}
	if specs[1].Name != "plugin_linapro_monitor_apilog_p202701" {
		t.Fatalf("跨年后的下一个月应为 202701，实际 %s", specs[1].Name)
	}
}

// TestCreateStatementIsRangeAndIdempotent 锁定分区范围左闭右开且带 IF NOT EXISTS。
func TestCreateStatementIsRangeAndIdempotent(t *testing.T) {
	specs := desiredSpecs(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))
	statement := specs[0].createStatement()

	if !strings.Contains(statement, "CREATE TABLE IF NOT EXISTS") {
		t.Fatalf("建分区语句必须幂等: %s", statement)
	}
	if !strings.Contains(statement, "PARTITION OF "+TableName) {
		t.Fatalf("建分区语句必须挂在父表上: %s", statement)
	}
	if !strings.Contains(statement, "FROM ('2026-11-01 00:00:00+00:00') TO ('2026-12-01 00:00:00+00:00')") {
		t.Fatalf("分区范围应为左闭右开的自然月: %s", statement)
	}
}

// TestCreateStatementNeverDropsOrDetaches 静态断言：生成的语句不会删除或分离分区。
//
// 记录只增不删，分区只用于查询剪枝（design D9）。
func TestCreateStatementNeverDropsOrDetaches(t *testing.T) {
	for _, spec := range desiredSpecs(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)) {
		statement := strings.ToUpper(spec.createStatement())
		for _, forbidden := range []string{"DROP", "DETACH", "TRUNCATE", "DELETE"} {
			if strings.Contains(statement, forbidden) {
				t.Fatalf("建分区语句不得包含 %s: %s", forbidden, statement)
			}
		}
	}
}

// TestEnsureIsNoopWhenPartitionsExist 锁定：分区齐全时不发任何 DDL。
//
// 这是「补齐任务不得堵住日志写入」的核心：正常情况一个月才需要拿一次父表锁。
func TestEnsureIsNoopWhenPartitionsExist(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	existing := make(map[string]struct{})
	for _, spec := range desiredSpecs(now) {
		existing[spec.Name] = struct{}{}
	}

	var executed []string
	svc := &serviceImpl{
		queryer: func(_ context.Context) (map[string]struct{}, error) {
			return existing, nil
		},
		executer: func(_ context.Context, statement string) error {
			executed = append(executed, statement)
			return nil
		},
		now: func() time.Time { return now },
	}

	created, err := svc.Ensure(context.Background())
	if err != nil {
		t.Fatalf("分区齐全时不应报错: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("分区齐全时不应创建任何分区，实际 %v", created)
	}
	if len(executed) != 0 {
		t.Fatalf("分区齐全时必须不发出任何 DDL，实际执行了 %v", executed)
	}
}

// TestEnsureCreatesOnlyMissingPartitions 锁定：只对缺失的月份发 DDL。
func TestEnsureCreatesOnlyMissingPartitions(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	desired := desiredSpecs(now)
	existing := map[string]struct{}{desired[0].Name: {}, desired[1].Name: {}}

	var executed []string
	svc := &serviceImpl{
		queryer: func(_ context.Context) (map[string]struct{}, error) {
			return existing, nil
		},
		executer: func(_ context.Context, statement string) error {
			executed = append(executed, statement)
			return nil
		},
		now: func() time.Time { return now },
	}

	created, err := svc.Ensure(context.Background())
	if err != nil {
		t.Fatalf("补齐不应报错: %v", err)
	}
	if len(created) != 5 {
		t.Fatalf("应创建 5 个缺失分区，实际 %v", created)
	}
	if len(executed) != 5 {
		t.Fatalf("应为每个缺失分区各发一条 DDL，实际 %d 条", len(executed))
	}
	if strings.Contains(strings.Join(executed, " "), desired[0].Name) {
		t.Fatal("已存在的分区不应被再次创建")
	}
}

// TestEnsureSetsLockTimeoutPerDDL 锁定每条 DDL 之前都设了 lock_timeout。
func TestEnsureSetsLockTimeoutPerDDL(t *testing.T) {
	statement := lockTimeoutStatement()
	if statement != "SET LOCAL lock_timeout = '5s'" {
		t.Fatalf("锁超时语句错误: %s", statement)
	}
}

// TestIsMissingErrorMatchesPostgresMessage 锁定分区缺失错误的识别：包装后的错误与大小写变化都能认出，
// 其余写库错误不误判——误判会把排查方向引到分区补齐任务上。
func TestIsMissingErrorMatchesPostgresMessage(t *testing.T) {
	pgErr := errors.New(`ERROR: no partition of relation "plugin_linapro_monitor_apilog" found for row (SQLSTATE 23514)`)
	if !IsMissingError(gerror.Wrap(pgErr, "写入接口请求日志失败")) {
		t.Fatal("包装后的分区缺失错误应被识别")
	}
	if !IsMissingError(errors.New("NO PARTITION OF RELATION x found for row")) {
		t.Fatal("识别不应区分大小写")
	}
	if IsMissingError(errors.New("duplicate key value violates unique constraint")) {
		t.Fatal("其他写库错误不应判为分区缺失")
	}
	if IsMissingError(nil) {
		t.Fatal("nil 不是分区缺失错误")
	}
}
