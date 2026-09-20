// Package state 管理 AI 判定流水线的持久化待判定候选人状态。
// 状态存于 Redis（Set + Hash），因此可跨任务重启存活且可被枚举 —— 这是宿主提供的
// Cache 能力做不到的。
//
// 连接池：New 构建一个持有底层 go-redis 连接池的 *gredis.Redis。返回的 *Store
// 必须保存在长生命周期单例中（见 plugin.go）——切勿每次请求都调用 New。
package state

import (
	"context"
	"fmt"
	"strconv"
	"time"

	// 空白导入在 init 阶段把 go-redis 适配器注册到 gredis。
	// 没有它，gredis.New 调用会返回 "redis adapter is not set"。
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gredis"
)

const (
	// pendingSetKey 是存放所有等待 AI 判定的 record ID 的 Redis Set。
	pendingSetKey = "recruit:pending"
	// candHashPrefix 是单个候选人的 Hash 键前缀：recruit:cand:<recordID>
	candHashPrefix = "recruit:cand:"

	hashFieldAppID       = "applicationId"
	hashFieldAnalyzingAt = "analyzing_at"
)

// Store 是对 gredis.Redis 实例的薄业务语义封装。
// 所有 Redis 键布局都封装在此处；调用方只处理 recordID、applicationId 和时间戳。
type Store struct {
	r   *gredis.Redis
	ttl time.Duration
}

// New 根据给定 RedisConfig 创建 Store。底层连接池由 go-redis 在首次使用时惰性建立。
// 配置缺少地址时返回错误。
func New(cfg RedisConfig) (*Store, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("state: 缺少 redis.address 配置")
	}
	ttlHours := cfg.TTLHours
	if ttlHours <= 0 {
		ttlHours = defaultTTLHours
	}
	r, err := gredis.New(&gredis.Config{
		Address: cfg.Address,
		Pass:    cfg.Pass,
		Db:      cfg.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("state: 创建 Redis 客户端失败: %w", err)
	}
	return &Store{r: r, ttl: time.Duration(ttlHours) * time.Hour}, nil
}

// Close 释放底层连接池。在进程关闭或凭证变化后重建单例时调用。
func (s *Store) Close() error {
	return s.r.Close(context.Background())
}

// AddPending 将候选人登记为等待 AI 判定。它把 applicationId 与开始分析时间戳写入
// 以 recordID 为键的 Hash，把 recordID 加入 pending Set，并对两个键都设置安全网 TTL。
func (s *Store) AddPending(ctx context.Context, recordID string, appID, analyzingAtMs int64) error {
	hashKey := candHashPrefix + recordID
	ttlSecs := int64(s.ttl.Seconds())

	if _, err := s.r.GroupHash().HSet(ctx, hashKey, map[string]any{
		hashFieldAppID:       strconv.FormatInt(appID, 10),
		hashFieldAnalyzingAt: strconv.FormatInt(analyzingAtMs, 10),
	}); err != nil {
		return fmt.Errorf("state: HSet 写入失败 %s: %w", hashKey, err)
	}
	if _, err := s.r.GroupGeneric().Expire(ctx, hashKey, ttlSecs); err != nil {
		return fmt.Errorf("state: 设置键过期时间失败 %s: %w", hashKey, err)
	}
	if _, err := s.r.GroupSet().SAdd(ctx, pendingSetKey, recordID); err != nil {
		return fmt.Errorf("state: 加入 pending 集合失败: %w", err)
	}
	if _, err := s.r.GroupGeneric().Expire(ctx, pendingSetKey, ttlSecs); err != nil {
		return fmt.Errorf("state: 设置 pending 集合过期时间失败: %w", err)
	}
	return nil
}

// ListPending 返回当前 pending Set 中的所有 record ID。
func (s *Store) ListPending(ctx context.Context) ([]string, error) {
	vars, err := s.r.GroupSet().SMembers(ctx, pendingSetKey)
	if err != nil {
		return nil, fmt.Errorf("state: 读取 pending 集合失败: %w", err)
	}
	ids := make([]string, 0, len(vars))
	for _, v := range vars {
		ids = append(ids, v.String())
	}
	return ids, nil
}

// GetPending 返回给定 recordID 存储的 applicationId 与开始分析时间戳。
// 键不存在（已过期或已删除）时 ok 为 false。
func (s *Store) GetPending(ctx context.Context, recordID string) (appID, analyzingAtMs int64, ok bool, err error) {
	hashKey := candHashPrefix + recordID
	raw, err := s.r.GroupHash().HGetAll(ctx, hashKey)
	if err != nil {
		return 0, 0, false, fmt.Errorf("state: HGetAll 读取失败 %s: %w", hashKey, err)
	}
	if raw == nil || raw.IsNil() {
		return 0, 0, false, nil
	}
	m := raw.MapStrStr()
	if len(m) == 0 {
		return 0, 0, false, nil
	}
	appID, _ = strconv.ParseInt(m[hashFieldAppID], 10, 64)
	analyzingAtMs, _ = strconv.ParseInt(m[hashFieldAnalyzingAt], 10, 64)
	if appID == 0 {
		return 0, 0, false, nil
	}
	return appID, analyzingAtMs, true, nil
}

// Remove 从 pending Set 删除候选人并删除其 Hash。
func (s *Store) Remove(ctx context.Context, recordID string) error {
	hashKey := candHashPrefix + recordID
	if _, err := s.r.GroupSet().SRem(ctx, pendingSetKey, recordID); err != nil {
		return fmt.Errorf("state: 移出 pending 集合失败 %s: %w", recordID, err)
	}
	if _, err := s.r.GroupGeneric().Del(ctx, hashKey); err != nil {
		return fmt.Errorf("state: 删除键失败 %s: %w", hashKey, err)
	}
	return nil
}
