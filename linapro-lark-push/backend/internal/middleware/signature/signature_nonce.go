// 本文件实现 nonce 防重放：用宿主插件作用域缓存的原子 Incr 完成「查 + 占位」一步操作。

package signature

import (
	"context"
	"strings"

	"lina-core/pkg/plugin/capability/cachecap"
)

// claimNonce 判定并登记一个 nonce，返回它是否为首次使用。
//
// 用 Incr 而不是「Get 判空 + Set」：后者在并发重放时两个请求都会读到空值而同时放行。
// 计数器的语义正好表达「用过一次」——返回 1 说明本次是第一次写入，≥2 说明之前已用过。
// 宿主确实没有 SetNX，Incr 是唯一原子的占位原语（memory 后端在互斥锁内读改写，
// coordkv 后端走 kvStore.IncrBy，两者都是原子的）。
//
// 缓存后端随 cluster.enabled 切换：单机为进程内内存、集群为共享 KV，因此同一请求重放到
// 另一实例同样会被识别。缓存不可用时返回错误，由调用方失败关闭。
func claimNonce(
	ctx context.Context,
	cache cachecap.Service,
	appKey string,
	nonce string,
) (bool, error) {
	item, err := cache.Incr(ctx, nonceNamespace, nonceCacheKey(appKey, nonce), 1, nonceTTL)
	if err != nil {
		return false, err
	}
	return item != nil && item.IntValue == 1, nil
}

// nonceCacheKey 拼装去重键：app_key 分隔不同调用方的 nonce 空间，
// 避免两个调用方偶然用同一个随机串时互相干扰。
func nonceCacheKey(appKey string, nonce string) string {
	return strings.TrimSpace(appKey) + ":" + strings.TrimSpace(nonce)
}
