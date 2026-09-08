// Package backend 是 linapro-moka-report-sync 数据源插件的入口。
package backend

import (
	"context"
	"fmt"

	mokareportsync "lina-plugin-linapro-moka-report-sync"
	"lina-plugin-linapro-moka-report-sync/backend/internal/config"
	"lina-plugin-linapro-moka-report-sync/backend/internal/service"

	"lina-core/pkg/plugin/pluginhost"

	"github.com/gogf/gf/v2/os/glog"
)

const (
	pluginID = "linapro-moka-report-sync"
	jobName  = "linapro-moka-report-sync"
)

func init() {
	plugin := pluginhost.NewDeclarations(pluginID)
	plugin.Assets().UseEmbeddedFiles(mokareportsync.EmbeddedFiles)

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

// registerJobs 注册周期性的报表→多维表格同步任务。间隔读取一次用于构建
// cron 表达式；凭据与映射在每次执行时都重新读取，因此后端修改无需重启即可生效。
func registerJobs(ctx context.Context, registrar pluginhost.JobsRegistrar) error {
	minutes := config.IntervalMinutes(ctx, registrar.Services())
	pattern := fmt.Sprintf("0 */%d * * * *", minutes)

	return registrar.AddWithMetadata(
		ctx,
		pattern,
		jobName,
		"Moka 报表同步",
		"定时把 Moka 报表数据同步到飞书多维表格",
		func(jobCtx context.Context) error {
			if _, err := service.NewRunner(registrar.Services()).RunOnce(jobCtx); err != nil {
				glog.Errorf(jobCtx, "Moka报表同步失败: %+v", err)
				return err
			}

			glog.Info(jobCtx, "Moka报表同步完成")

			return nil
		},
	)
}
