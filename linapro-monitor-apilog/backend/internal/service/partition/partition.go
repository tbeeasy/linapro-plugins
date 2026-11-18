// Package partition 负责按月分区的创建与补齐。
//
// 设计约束（见 design D9）：
//   - 只做「创建新分区」，绝不 DROP / DETACH 已有分区——那是删除动作，与「记录只增不删」冲突；
//   - 先查再建：分区齐全时不发任何 DDL，不在父表上申请锁；
//   - 每条 DDL 在同一事务内先设 lock_timeout：拿不到锁就放弃、等次日重试，
//     宁可本次建不成，也不能让后续 INSERT 排在 DDL 后面一起等。
//
// 「当前月 + 未来 6 个月」的边界算法只有这一份实现：安装 SQL 里的初始分区与每日补齐任务
// 必须一致，否则会出现某个月永远缺分区。
package partition

import (
	"context"
	"time"
)

// 分区表的表名、命名约定与补齐窗口。
const (
	// TableName 是父表名。
	TableName = "plugin_linapro_monitor_apilog"
	// partitionPrefix 是分区名前缀，后接 YYYYMM。
	partitionPrefix = TableName + "_p"
	// aheadMonths 是提前创建的分区月数：当前月 + 未来 6 个月。
	aheadMonths = 6
	// lockTimeout 是建分区 DDL 等待父表锁的上限。
	lockTimeout = "5s"
	// partitionRangeLayout 是分区边界字面量的格式（带显式时区偏移）。
	partitionRangeLayout = "2006-01-02 15:04:05-07:00"
)

// Spec 描述一个应当存在的月度分区。
type Spec struct {
	// Name 是分区表名，形如 plugin_linapro_monitor_apilog_p202611。
	Name string
	// From 是范围下界（含）。
	From time.Time
	// To 是范围上界（不含）。
	To time.Time
}

// Service 定义分区补齐能力。
type Service interface {
	// Ensure 确保「当前月 + 未来 6 个月」的分区都存在，返回本次实际创建的分区名。
	// 全部存在时不执行任何 DDL，也不在父表上申请锁。
	Ensure(ctx context.Context) (created []string, err error)
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的实现。
type serviceImpl struct {
	// queryer 读系统目录，取已存在的分区名。抽成函数便于单测注入。
	queryer func(ctx context.Context) (map[string]struct{}, error)
	// executer 在事务内执行一条 DDL。抽成函数便于单测断言「没有发 DDL」。
	executer func(ctx context.Context, statement string) error
	// now 提供当前时间，便于单测固定月份边界。
	now func() time.Time
}

// New 构造分区补齐服务。
func New() Service {
	return &serviceImpl{
		queryer:  listExistingPartitions,
		executer: executePartitionDDL,
		now:      time.Now,
	}
}
