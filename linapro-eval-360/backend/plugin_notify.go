// plugin_notify.go 装配第 8 节的飞书推送编排 Sender，注入项目服务。
//
// 把 notify.Sender 的构造与 plugin.go 主装配文件分离：notify 依赖宿主
// hostconfigcap（读卡片模板与 todo H5 URL）、empcap（EmpID → 工号 / 姓名）与
// linapro-lark-push 的 larkpushcap（推送应用清单与卡片投递）。这些前置在
// registerRoutes 内已可拿到，构造一个进程内 Sender 注入 project.New。
package backend

import (
	"context"

	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-eval-360/backend/internal/notify"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
)

// newNotifySender 构造推送编排 Sender。在 registerRoutes 内调用一次：
//   - emp 用 empcap.BoundEmployeeService()（employee-core 已 init 绑定）；
//   - hostConfig 来自宿主 capability.Services.HostConfig()；
//   - pusher 用 larkPusher：每次调用懒取 larkpushcap 当前绑定，不在装配期固化实例
//     （插件注册顺序不影响；lark-push 未启用时投递返回「能力未就绪」）。
//
// 任一前置（empcap 未绑定 / hostConfig 不可用）返回 nil Sender：
// project.New 容忍 nil sender（一键发送仅完成发布、不推送），
// 不阻断插件启动——推送能力随 employee-core 启用而就绪。
func newNotifySender(_ context.Context, hostConfig hostconfigcap.Service) notify.Sender {
	emp := empBoundLookup()
	if emp == nil {
		// employee-core 未启用 → 无法把评价者解析为工号，跳过推送（不阻断路由注册）。
		return nil
	}
	if hostConfig == nil {
		// 宿主配置能力不可用 → 无法读卡片模板与待办链接，跳过推送。
		return nil
	}
	return notify.New(emp, hostConfig, larkPusher{})
}

// larkPusher 把 larkpushcap 的包级便捷函数适配为 notify.Pusher：每次调用懒取当前绑定，
// 未绑定时返回 larkpushcap.ErrSenderUnbound（notify 映射为「推送能力未就绪」）。
type larkPusher struct{}

// ListPushApps 转发到 larkpushcap.ListPushApps。
func (larkPusher) ListPushApps(ctx context.Context, group string) ([]larkpushcap.PushApp, error) {
	return larkpushcap.ListPushApps(ctx, group)
}

// SendCard 转发到 larkpushcap.SendCard。
func (larkPusher) SendCard(ctx context.Context, req larkpushcap.SendCardRequest) (larkpushcap.SendResult, error) {
	return larkpushcap.SendCard(ctx, req)
}

// empBoundLookup 返回 empcap 绑定的员工服务（满足 notify.EmpLookup），
// 未绑定时返回 nil。employee-core 与本插件同进程编译，其 init() 先于本插件
// 绑定；未安装/未启用时 BoundEmployeeService 返回 nil，此处据此降级。
func empBoundLookup() notify.EmpLookup {
	svc := empcap.BoundEmployeeService()
	if svc == nil {
		return nil
	}
	return svc
}
