// Package state 在 Redis 中管理 recruit-pipeline 的持久化待判定候选人状态。
// 它与业务 Config 分离，使基础设施关注点（Redis 地址、凭证、TTL）不泄漏进业务逻辑。
package state

import (
	"context"

	"lina-core/pkg/plugin/capability/hostconfigcap"
)

const (
	keyRedisAddress  = "plugin.linapro-recruit-pipeline.redis.address"
	keyRedisPass     = "plugin.linapro-recruit-pipeline.redis.pass"
	keyRedisDB       = "plugin.linapro-recruit-pipeline.redis.db"
	keyRedisTTLHours = "plugin.linapro-recruit-pipeline.redis.ttl_hours"

	defaultTTLHours = 72
)

// RedisConfig 保存状态存储的 Redis 连接参数。
// 它从插件的静态 host 配置加载，而非业务 sys_config，因此可在业务配置之前（独立于业务配置）读取。
type RedisConfig struct {
	Address  string // Redis 地址，例如 "127.0.0.1:6379"
	Pass     string // Redis AUTH 密码；无需鉴权时为空串
	DB       int    // Redis 逻辑数据库索引
	TTLHours int    // pending 键的安全网 TTL（默认 72 小时）
}

// LoadRedisConfig 从 host 静态配置读取 Redis 连接配置。
// 所有字段都有安全默认值，最小化部署只需设置 redis.address。
func LoadRedisConfig(ctx context.Context, hc hostconfigcap.Service) RedisConfig {
	cfg := RedisConfig{TTLHours: defaultTTLHours}
	if hc == nil {
		return cfg
	}
	cfg.Address, _ = hc.String(ctx, keyRedisAddress, "")
	cfg.Pass, _ = hc.String(ctx, keyRedisPass, "")
	cfg.DB, _ = hc.Int(ctx, keyRedisDB, 0)
	ttl, _ := hc.Int(ctx, keyRedisTTLHours, defaultTTLHours)
	if ttl > 0 {
		cfg.TTLHours = ttl
	}
	return cfg
}
