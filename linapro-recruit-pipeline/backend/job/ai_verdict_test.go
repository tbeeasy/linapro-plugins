// ai_verdict_test.go 验证 RunAIVerdict 的负向排除判定逻辑。
// 核心修复点（FB-4）：非空且不在排除列表中的判定值触发 MoveApplicationStage；
// 排除列表中的判定（「建议淘汰」「无匹配类型」「谨慎考虑」）直接出队，不推进。
// 判定字段名和排除值列表均来自 Config，不再硬编码。
//
// 测试自包含且顺序无关：每个测试自行构造 fake 依赖，不依赖 HTTP server 或共享状态。
package job

import (
	"context"
	"testing"
	"time"

	"lina-plugin-linapro-recruit-pipeline/backend/config"
	"lina-plugin-linapro-recruit-pipeline/backend/state"
	lark "linapro-lark-sdk/larkbitable"
)

// --- fake 依赖 ---

// fakeBitable 实现 bitableClient，直接返回预设数据，不发起任何网络请求。
type fakeBitable struct {
	// records 按 recordID 索引，模拟 BatchGetByIDs 命中的行。
	records map[string]lark.Row
	// absent 模拟 BatchGetByIDs 返回的 absent_record_ids（飞书中已不存在的行）。
	absent []string
	// fieldTypes 模拟 ListFields 的返回值；为 nil 时返回空 map。
	fieldTypes map[string]int
}

// BatchGetByIDs 只返回 records 中命中传入 recordIDs 的行，并原样带出预设的 absent 列表，
// 贴近真实客户端「按 record_id 精准批量读取」的语义。fieldTypes 透传给读取函数，
// 供人员列按类型解码；替身返回预设 records，无需实际解码。
func (f *fakeBitable) BatchGetByIDs(_ context.Context, _ lark.Table, recordIDs []string, _ map[string]int) (map[string]lark.Row, []string, error) {
	out := make(map[string]lark.Row, len(recordIDs))
	for _, id := range recordIDs {
		if row, ok := f.records[id]; ok {
			out[id] = row
		}
	}
	return out, f.absent, nil
}

func (f *fakeBitable) ListFields(_ context.Context, _ lark.Table) (map[string]int, error) {
	if f.fieldTypes == nil {
		return map[string]int{}, nil
	}
	return f.fieldTypes, nil
}

func (f *fakeBitable) BatchUpdate(_ context.Context, _ lark.Table, _ []lark.UpdateOp, _ map[string]int) error {
	return nil
}

// fakeMoka 实现 mokaStageClient，记录 MoveApplicationStage 是否被调用及传入的 applicationID。
type fakeMoka struct {
	called bool
	appID  int64
}

func (m *fakeMoka) MoveApplicationStage(_ context.Context, applicationID int64, _ int64) error {
	m.called = true
	m.appID = applicationID
	return nil
}

// --- 辅助函数 ---

// buildTestConfig 构造测试用 Config。
func buildTestConfig(t *testing.T, mokaURL, verdictField string, excluded []string) *config.Config {
	t.Helper()
	return &config.Config{
		LarkAppID:               "test-app-id",
		LarkAppSecret:           "test-app-secret",
		BitableAppToken:         "tok",
		CandidateBitableTableID: "tbl",
		MokaAPIKey:              "k",
		MokaBaseURL:             mokaURL,
		DeptScreeningStageID:    105020,
		AIWaitMinutes:           1,
		AIVerdictField:          verdictField,
		AIVerdictExcluded:       excluded,
		FieldMapping:            map[string]string{"name": "候选人"},
	}
}

// buildTestStore 尝试连接本地 Redis 并写入测试数据；Redis 不可用时返回 nil。
func buildTestStore(t *testing.T, recordID string, appID int64, analyzingAtMs int64) *state.Store {
	t.Helper()
	redisAddr := "127.0.0.1:6379"
	st, err := state.New(state.RedisConfig{
		Address:  redisAddr,
		TTLHours: 1,
	})
	if err != nil {
		return nil
	}
	ctx := context.Background()
	if aErr := st.AddPending(ctx, recordID, appID, analyzingAtMs); aErr != nil {
		_ = st.Close()
		return nil
	}
	t.Cleanup(func() {
		_ = st.Remove(ctx, recordID)
		_ = st.Close()
	})
	return st
}

// --- 测试用例 ---

// TestRunAIVerdict_ExcludedVerdictSkipsMove 验证排除列表中的判定值直接出队，
// 不触发 MoveApplicationStage。取三个默认排除值分别测试。
func TestRunAIVerdict_ExcludedVerdictSkipsMove(t *testing.T) {
	excluded := []string{"建议淘汰", "无匹配类型", "谨慎考虑"}
	for _, ev := range excluded {
		t.Run(ev, func(t *testing.T) {
			// 用已过期的 analyzingAt（比阈值早足够多）确保本轮不跳过等待窗口。
			pastMs := time.Now().UTC().Add(-30 * time.Minute).UnixMilli()
			redisStore := buildTestStore(t, "rec1", 111, pastMs)
			if redisStore == nil {
				t.Skip("Redis 不可用，跳过集成测试")
			}

			moka := &fakeMoka{}
			bitable := &fakeBitable{
				records: map[string]lark.Row{
					"rec1": {"AI评估结论": ev},
				},
			}
			cfg := buildTestConfig(t, "http://unused", "AI评估结论", excluded)

			if err := RunAIVerdict(context.Background(), cfg, moka, redisStore, bitable); err != nil {
				t.Fatalf("RunAIVerdict: %v", err)
			}
			if moka.called {
				t.Errorf("排除值 %q 不应触发 MoveApplicationStage", ev)
			}
		})
	}
}

// TestRunAIVerdict_PassVerdictTriggersMove 验证不在排除列表中的非空判定值
// 触发 MoveApplicationStage（负向排除逻辑的核心路径）。
func TestRunAIVerdict_PassVerdictTriggersMove(t *testing.T) {
	pastMs := time.Now().UTC().Add(-30 * time.Minute).UnixMilli()
	redisStore := buildTestStore(t, "rec1", 999, pastMs)
	if redisStore == nil {
		t.Skip("Redis 不可用，跳过集成测试")
	}

	excluded := []string{"建议淘汰", "无匹配类型", "谨慎考虑"}
	moka := &fakeMoka{}
	bitable := &fakeBitable{
		records: map[string]lark.Row{
			"rec1": {"AI评估结论": "建议录用"},
		},
	}
	cfg := buildTestConfig(t, "http://unused", "AI评估结论", excluded)

	if err := RunAIVerdict(context.Background(), cfg, moka, redisStore, bitable); err != nil {
		t.Fatalf("RunAIVerdict: %v", err)
	}
	if moka.appID != 999 {
		t.Errorf("MoveApplicationStage 应以 applicationId=999 调用，got=%d", moka.appID)
	}
}

// TestRunAIVerdict_CustomFieldAndExcluded 验证字段名和排除值可通过 Config 覆盖，
// 默认值（"AI评估结论" / 三个排除项）不再硬编码。
func TestRunAIVerdict_CustomFieldAndExcluded(t *testing.T) {
	pastMs := time.Now().UTC().Add(-30 * time.Minute).UnixMilli()
	redisStore := buildTestStore(t, "rec1", 123, pastMs)
	if redisStore == nil {
		t.Skip("Redis 不可用，跳过集成测试")
	}

	// 自定义字段名 "智能评分" 和自定义排除值 "不合适"——命中排除列表，不应推进。
	moka := &fakeMoka{}
	bitable := &fakeBitable{
		records: map[string]lark.Row{
			"rec1": {"智能评分": "不合适"},
		},
	}
	cfg := buildTestConfig(t, "http://unused", "智能评分", []string{"不合适"})

	if err := RunAIVerdict(context.Background(), cfg, moka, redisStore, bitable); err != nil {
		t.Fatalf("RunAIVerdict: %v", err)
	}
	if moka.called {
		t.Errorf("自定义排除值 %q 不应触发 MoveApplicationStage", "不合适")
	}
}
