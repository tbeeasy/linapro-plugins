// partition_ensure.go 实现分区目标计算、系统目录查询与带锁超时的建分区 DDL。

package partition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-core/pkg/logger"
	"lina-plugin-linapro-monitor-apilog/backend/internal/dao"
)

// desiredSpecs 返回「当前月 + 未来 6 个月」应存在的分区列表。
//
// 边界按 UTC 月初计算，左闭右开，跨年正确（12 月的上界是次年 1 月 1 日）。
func desiredSpecs(now time.Time) []Spec {
	base := now.UTC()
	firstOfMonth := time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, time.UTC)
	specs := make([]Spec, 0, aheadMonths+1)
	for offset := 0; offset <= aheadMonths; offset++ {
		from := firstOfMonth.AddDate(0, offset, 0)
		specs = append(specs, Spec{
			Name: fmt.Sprintf("%s%s", partitionPrefix, from.Format("200601")),
			From: from,
			To:   from.AddDate(0, 1, 0),
		})
	}
	return specs
}

// createStatement 生成一条建分区 DDL。
//
// IF NOT EXISTS 保证幂等，但不免锁——因此调用方必须先查再建，只在真缺时才执行它。
func (s Spec) createStatement() string {
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
		s.Name,
		TableName,
		s.From.Format(partitionRangeLayout),
		s.To.Format(partitionRangeLayout),
	)
}

// missingSpecs 从目标列表里剔除已经存在的分区，返回真正需要创建的那些。
func missingSpecs(desired []Spec, existing map[string]struct{}) []Spec {
	missing := make([]Spec, 0, len(desired))
	for _, spec := range desired {
		if _, ok := existing[spec.Name]; ok {
			continue
		}
		missing = append(missing, spec)
	}
	return missing
}

// Ensure 确保目标分区都存在。
func (s *serviceImpl) Ensure(ctx context.Context) ([]string, error) {
	desired := desiredSpecs(s.now())
	existing, err := s.queryer(ctx)
	if err != nil {
		return nil, err
	}
	missing := missingSpecs(desired, existing)
	if len(missing) == 0 {
		// 分区齐全：不发 DDL、不加锁。正常情况一个月才需要拿一次锁。
		return nil, nil
	}

	created := make([]string, 0, len(missing))
	for _, spec := range missing {
		if execErr := s.executer(ctx, spec.createStatement()); execErr != nil {
			return created, execErr
		}
		created = append(created, spec.Name)
	}
	return created, nil
}

// listExistingPartitions 查询父表下已存在的分区名。只读系统目录，不触碰父表锁。
func listExistingPartitions(ctx context.Context) (map[string]struct{}, error) {
	const query = `
SELECT c.relname AS partition_name
FROM pg_inherits i
JOIN pg_class c ON c.oid = i.inhrelid
JOIN pg_class p ON p.oid = i.inhparent
WHERE p.relname = ?`
	all, err := dao.Apilog.DB().GetAll(ctx, query, TableName)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]struct{}, len(all))
	for _, record := range all {
		existing[record["partition_name"].String()] = struct{}{}
	}
	return existing, nil
}

// executePartitionDDL 在一个事务内先设 lock_timeout，再执行 DDL。
//
// PG 14 的 CREATE TABLE ... PARTITION OF 要在父表上拿 ACCESS EXCLUSIVE 锁；若此时有长查询
// 占着父表，DDL 会排队，而排在 DDL 后面的所有 INSERT 也一起等（锁队列先来先得）。
// 设了 lock_timeout 后拿不到锁就放弃本次，次日自动重试。
func executePartitionDDL(ctx context.Context, statement string) error {
	// 闭包内的 ctx 携带事务，DB().Exec 经 ctx 自动落到同一连接上，SET LOCAL 才对 DDL 生效。
	return dao.Apilog.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		if _, err := dao.Apilog.DB().Exec(ctx, lockTimeoutStatement()); err != nil {
			return err
		}
		_, err := dao.Apilog.DB().Exec(ctx, statement)
		return err
	})
}

// lockTimeoutStatement 返回设置本地事务锁超时的语句。
func lockTimeoutStatement() string {
	return fmt.Sprintf("SET LOCAL lock_timeout = '%s'", lockTimeout)
}

// missingErrorKeyword 是 PostgreSQL「找不到该行对应的分区」错误的稳定片段，
// 完整报文形如 no partition of relation "..." found for row。判定与日志共用这一份。
const missingErrorKeyword = "no partition of relation"

// IsMissingError 识别写入时因目标月份分区不存在而失败的错误。
//
// 匹配整条错误链的文本而非 gerror.Current：上层每包一层，Current 只剩最外层文案，
// 数据库原始报文就看不到了。
func IsMissingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), missingErrorKeyword)
}

// LogFailure 记录分区补齐失败，并说明影响面，避免只留一句泛泛的 error。
func LogFailure(ctx context.Context, err error) {
	logger.Errorf(ctx, "linapro-monitor-apilog：分区补齐失败，受影响月份的日志将写入报「%s」，请检查：%v", missingErrorKeyword, err)
}
