// Package job 实现 linapro-recruit-pipeline 插件的 AI 判定定时任务。
// 从 Redis 读取待判定候选人的 recordID，检查 AI 等待窗口是否到期，
// 再读取对应 Bitable 行的 AI 判定字段并决定推进或出队。
// 判定字段名和排除值列表均来自 Config，可通过 sys_config 运营期调整，无需重启进程。
package job

import (
	"context"
	"fmt"
	"lina-core/pkg/logger"
	"slices"
	"strings"
	"time"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	"lina-plugin-linapro-recruit-pipeline/backend/state"
	lark "linapro-lark-sdk/larkbitable"
)

// bitableClient 是 RunAIVerdict 依赖的 Bitable 操作子集，
// 便于测试中以 fake struct 替代真实的 lark.Client。
type bitableClient interface {
	BatchGetByIDs(ctx context.Context, t lark.Table, recordIDs []string, fieldTypes map[string]int) (map[string]lark.Row, []string, error)
	ListFields(ctx context.Context, t lark.Table) (map[string]int, error)
	BatchUpdate(ctx context.Context, t lark.Table, updates []lark.UpdateOp, fieldTypes map[string]int) error
}

// mokaStageClient 是 RunAIVerdict 依赖的 Moka 推进操作子集。
type mokaStageClient interface {
	MoveApplicationStage(ctx context.Context, applicationID int64, stageID int64) error
}

// RunAIVerdict 遍历 Redis pending 集合中的所有 recordID，跳过尚未到达等待窗口的项，
// 对其余项读取 Bitable 中的 AI 判定字段：
// 非空且不在排除值列表（建议淘汰/无匹配类型/谨慎考虑等）中的判定，
// 调 MoveApplicationStage 推进到用人部门筛选阶段；
// 排除值列表中的判定直接出队，不推进、不淘汰。
// 判定字段名和排除值列表均由 cfg 提供，可通过 sys_config 覆盖。
// larkClient 由调用方注入，便于测试中替换为替身。
func RunAIVerdict(ctx context.Context, cfg *config.Config, mokaClient mokaStageClient, store *state.Store, larkClient bitableClient) error {
	pendingIDs, err := store.ListPending(ctx)
	if err != nil {
		return fmt.Errorf("ai-verdict: 读取 pending 集合失败: %w", err)
	}
	if len(pendingIDs) == 0 {
		logger.Debugf(ctx, "ai-verdict: 无待扫描的 AI 判定记录, 本轮跳过")
		return nil
	}

	logger.Infof(ctx, "ai-verdict: 开始扫描, pending=%d, ai_verdict_field=%q, dept_stage_id=%d, wait_minutes=%d",
		len(pendingIDs), cfg.AIVerdictField, cfg.DeptScreeningStageID, cfg.AIWaitMinutes)
	logger.Debugf(ctx, "ai-verdict: 待扫描的 recordID 列表=%v", pendingIDs)

	table := lark.Table{AppToken: cfg.BitableAppToken, TableID: cfg.CandidateBitableTableID}

	// 先取字段类型再批量读取：cellToString 按列类型解码（人员列回读姓名、富文本列拼接），
	// 必须把 ListFields 的结果传给读取函数，否则人员单元格会被按键名嗅探误判。
	fieldTypes, err := larkClient.ListFields(ctx, table)
	if err != nil {
		return fmt.Errorf("ai-verdict: 读取字段类型失败: %w", err)
	}

	// 按 pending recordID 精准批量读取，只拉待判定行，避免全表扫描随历史候选人累积而线性膨胀。
	byID, absentIDs, err := larkClient.BatchGetByIDs(ctx, table, pendingIDs, fieldTypes)
	if err != nil {
		return fmt.Errorf("ai-verdict: 按 recordID 批量读取失败: %w", err)
	}
	// absent 集合驱动「行已删除→出队」清理，替代原先遍历全表索引判断 exists 的做法。
	absent := make(map[string]struct{}, len(absentIDs))
	for _, id := range absentIDs {
		absent[id] = struct{}{}
	}

	logger.Debugf(ctx, "ai-verdict: 批量读取 Bitable 命中 %d 条, 缺失 %d 条, 字段 %d 个", len(byID), len(absentIDs), len(fieldTypes))

	nowMs := time.Now().UTC().UnixMilli()
	thresholdMs := int64(cfg.AIWaitMinutes) * 60 * 1000

	var advanced, excluded int

	for _, recordID := range pendingIDs {
		appID, analyzingAtMs, ok, gErr := store.GetPending(ctx, recordID)
		if gErr != nil {
			logger.Errorf(ctx, "ai-verdict: GetPending recordID=%s: %v", recordID, gErr)
			continue
		}
		if !ok {
			// Hash 键已过期或已被删除，清理 Set 中的残留项。
			if rErr := store.Remove(ctx, recordID); rErr != nil {
				logger.Errorf(ctx, "ai-verdict: Remove evicted recordID=%s: %v", recordID, rErr)
			}
			continue
		}
		if nowMs-analyzingAtMs < thresholdMs {
			// 等待窗口尚未到期，本轮跳过，下次再检查。
			continue
		}

		row, exists := byID[recordID]
		if !exists {
			if _, gone := absent[recordID]; gone {
				// Bitable 中该行已被删除（落入 batch_get 的 absent_record_ids），从 pending 队列移除。
				logger.Warningf(ctx, "ai-verdict: recordID=%s 已从 Bitable 删除, 移出 pending 队列", recordID)
				if rErr := store.Remove(ctx, recordID); rErr != nil {
					logger.Errorf(ctx, "ai-verdict: Remove missing recordID=%s: %v", recordID, rErr)
				}
				continue
			}
			// 既未命中也不在 absent（如高级权限禁止访问），本轮保守保留，下轮再看。
			logger.Warningf(ctx, "ai-verdict: recordID=%s 本轮未取到且不在 absent, 保留队列下轮重试", recordID)
			continue
		}

		verdict := strings.TrimSpace(row[cfg.AIVerdictField])
		if verdict == "" {
			// AI 尚未输出结果，保留队列项，下轮再检查。
			continue
		}

		if !slices.Contains(cfg.AIVerdictExcluded, verdict) {
			// 判定值不在排除列表中，视为通过，推进到用人部门筛选阶段。
			logger.Infof(ctx, "ai-verdict: 推进候选人 applicationId=%d stageId=%d", appID, cfg.DeptScreeningStageID)
			if mErr := mokaClient.MoveApplicationStage(ctx, appID, cfg.DeptScreeningStageID); mErr != nil {
				logger.Errorf(ctx, "ai-verdict: MoveApplicationStage applicationId=%d stageId=%d: %v", appID, cfg.DeptScreeningStageID, mErr)
				// 推进失败，保留队列项，下轮重试。
				continue
			}
			advanced++
		} else {
			logger.Infof(ctx, "ai-verdict: 判定 %q 命中排除列表, applicationId=%d 出队不推进", verdict, appID)
			excluded++
		}

		// 任意非空判定（已推进或已排除出队）均表示本条处理完毕，从队列移除。
		if rErr := store.Remove(ctx, recordID); rErr != nil {
			logger.Errorf(ctx, "ai-verdict: Remove recordID=%s after verdict: %v", recordID, rErr)
		}
	}

	logger.Infof(ctx, "ai-verdict: 本轮处理完成, 推进=%d, 排除出队=%d", advanced, excluded)

	return nil
}
