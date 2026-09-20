// Package backend 是 linapro-recruit-pipeline 源插件的入口。
// 它负责注册：
//   - 候选人轮询定时任务（需求1.1~1.4：从 Moka 拉取初筛阶段候选人、简历/附件/归属者写表入队）
//   - 每分钟一次的 AI 判定定时任务（需求1.5：ai_verdict 扫描推进）
//   - 面试取消状态同步定时任务（需求2）
//   - 报表评分回写定时任务（需求3）
package backend

import (
	"context"
	"fmt"
	"lina-core/pkg/logger"
	"sync"
	"time"

	recruitpipeline "lina-plugin-linapro-recruit-pipeline"
	"lina-plugin-linapro-recruit-pipeline/backend/config"
	"lina-plugin-linapro-recruit-pipeline/backend/job"
	"lina-plugin-linapro-recruit-pipeline/backend/state"
	lark "linapro-lark-sdk/larkbitable"

	mokabackend "lina-plugin-linapro-moka-recruit/backend/moka"

	"lina-core/pkg/plugin/capability"
	"lina-core/pkg/plugin/pluginhost"
)

const pluginID = "linapro-recruit-pipeline"

var (
	mokaClientMu sync.Mutex
	sharedMoka   *mokabackend.Client

	redisStoreMu sync.Mutex
	sharedRedis  *state.Store
)

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(recruitpipeline.EmbeddedFiles)

	// 四个定时任务全部注册。
	if err := plugin.Jobs().RegisterJobs(
		pluginhost.ExtensionPointJobsRegister,
		pluginhost.CallbackExecutionModeBlocking,
		registerJobs,
	); err != nil {
		panic(err)
	}

	if err := pluginhost.RegisterSourcePlugin(plugin); err != nil {
		panic(err)
	}
}

// registerJobs 注册四个流水线定时任务。
func registerJobs(ctx context.Context, registrar pluginhost.JobsRegistrar) error {
	services := registrar.Services()
	candidatePollInterval := config.CandidatePollIntervalMinutes(ctx, services)
	candidatePollTimeout := time.Duration(config.CandidatePollTimeoutMinutes(ctx, services)) * time.Minute
	interviewSyncInterval := config.InterviewSyncIntervalMinutes(ctx, services)
	reportSyncInterval := config.ReportSyncIntervalMinutes(ctx, services)

	// 需求1.1 候选人轮询 —— 拉取初筛阶段候选人。
	if err := registrar.AddWithMetadata(ctx,
		fmt.Sprintf("0 */%d * * * *", candidatePollInterval),
		"linapro-recruit-pipeline-candidate-poll",
		"候选人轮询",
		"定时从 Moka 拉取初筛阶段候选人，写入 Bitable 并入 Redis 待 AI 判定队列",
		func(jobCtx context.Context) error {
			// 候选人轮询需串行下载简历、上传飞书附件、写 Bitable，积压时远超宿主 5 分钟默认 deadline。
			// 用 WithoutCancel 解绑宿主 deadline（保留日志/追踪 value），再套插件自控的更长超时。
			runCtx, cancel := context.WithTimeout(context.WithoutCancel(jobCtx), candidatePollTimeout)
			defer cancel()
			return runWithConfig(runCtx, services, func(cfg *config.Config) error {
				rs, rsErr := getRedisStore(runCtx, services)
				if rsErr != nil {
					logger.Warningf(runCtx, "recruit-pipeline: candidate-poll: Redis 不可用, 禁用 AI 待判定队列: %v", rsErr)
					// Redis 不可用时仍尝试写 Bitable，仅跳过入队
				}
				if err := job.RunCandidatePoll(runCtx, cfg, mokaClient(cfg), rs); err != nil {
					logger.Errorf(runCtx, "RunCandidatePoll 执行失败: %v", err)
					return err
				}

				logger.Info(runCtx, "RunCandidatePoll 执行完成")

				return nil
			})
		},
	); err != nil {
		return err
	}

	// 需求1.5 AI 判定扫描 —— 每分钟一次。
	if err := registrar.AddWithMetadata(ctx,
		"0 */1 0 * 0 *",
		"linapro-recruit-pipeline-ai-verdict",
		"招聘 AI 判定",
		"扫描 Redis pending 队列，读取 ai_verdict 并推进面试阶段",
		func(jobCtx context.Context) error {
			return runWithConfig(jobCtx, services, func(cfg *config.Config) error {
				rs, rsErr := getRedisStore(jobCtx, services)
				if rsErr != nil {
					logger.Warningf(jobCtx, "recruit-pipeline: ai-verdict: Redis 不可用, 本轮跳过: %v", rsErr)
					return nil
				}
				lc := lark.NewClient(cfg.LarkAppID, cfg.LarkAppSecret)
				if err := job.RunAIVerdict(jobCtx, cfg, mokaClient(cfg), rs, lc); err != nil {
					logger.Errorf(jobCtx, "RunAIVerdict 执行失败: %v", err)
					return err
				}

				logger.Info(jobCtx, "RunAIVerdict 执行完成")

				return nil
			})
		},
	); err != nil {
		return err
	}

	// 需求2 面试取消状态同步。
	if err := registrar.AddWithMetadata(ctx,
		fmt.Sprintf("0 */%d * * * *", interviewSyncInterval),
		"linapro-recruit-pipeline-interview-sync",
		"面试取消状态同步",
		"定时拉取面试阶段候选人面试信息，回写应约状态/到场状态到 Bitable",
		func(jobCtx context.Context) error {
			return runWithConfig(jobCtx, services, func(cfg *config.Config) error {
				if err := job.RunInterviewSync(jobCtx, cfg, mokaClient(cfg)); err != nil {
					logger.Errorf(jobCtx, "RunInterviewSync 执行失败: %v", err)
					return err
				}

				logger.Info(jobCtx, "RunInterviewSync 执行完成")

				return nil
			})
		},
	); err != nil {
		return err
	}

	// 需求3 报表评分回写。
	if err := registrar.AddWithMetadata(ctx,
		fmt.Sprintf("0 */%d * * * *", reportSyncInterval),
		"linapro-recruit-pipeline-report-sync",
		"报表评分回写",
		"定时从招聘报表按 applicationId 回写评分列，未命中则新建记录",
		func(jobCtx context.Context) error {
			return runWithConfig(jobCtx, services, func(cfg *config.Config) error {
				if err := job.RunReportSync(jobCtx, cfg, mokaClient(cfg)); err != nil {
					logger.Errorf(jobCtx, "RunReportSync 执行失败: %v", err)
					return err
				}

				logger.Info(jobCtx, "RunReportSync 执行完成")

				return nil
			})
		},
	); err != nil {
		return err
	}

	return nil
}

// runWithConfig 在每次任务执行时重新加载配置并调用 fn。
// 配置错误会被记录日志并视为非致命错误，避免单个坏周期阻止调度器重试。
func runWithConfig(ctx context.Context, services capability.Services, fn func(*config.Config) error) error {
	cfg, err := config.Load(ctx, services)
	if err != nil {
		logger.Warningf(ctx, "recruit-pipeline: 配置不可用, 本轮任务跳过: %v", err)
		return nil
	}
	return fn(cfg)
}

// mokaClient 返回给定配置下的共享 Moka BasicAuth 客户端。
// 凭证变化时客户端会被重建。
func mokaClient(cfg *config.Config) *mokabackend.Client {
	mokaClientMu.Lock()
	defer mokaClientMu.Unlock()
	if sharedMoka == nil {
		auth := mokabackend.NewBasicAuth(mokabackend.BasicCredential{
			APIKey: cfg.MokaAPIKey,
		})
		sharedMoka = mokabackend.NewClient(cfg.MokaBaseURL, auth)
	}
	return sharedMoka
}

// getRedisStore 返回共享的 Redis 状态存储，首次调用时初始化。
// 当 Redis 未配置或不可用时返回 nil + error；调用方应优雅降级而非硬失败。
func getRedisStore(ctx context.Context, services capability.Services) (*state.Store, error) {
	redisStoreMu.Lock()
	defer redisStoreMu.Unlock()
	if sharedRedis != nil {
		return sharedRedis, nil
	}
	if services == nil || services.HostConfig() == nil {
		return nil, fmt.Errorf("recruit-pipeline: 宿主服务不可用")
	}
	redisCfg := state.LoadRedisConfig(ctx, services.HostConfig())
	s, err := state.New(redisCfg)
	if err != nil {
		return nil, err
	}
	sharedRedis = s
	return s, nil
}
