// Package send 实现飞书消息（文本 / 交互卡片 / 文件）推送的编排：参数校验 → 读请求分组的配置 →
// 遍历该分组的推送应用 → 按 identity_app_id 查「工号 → 企业级 user_id」→ 逐收件人发送 →
// 汇总结果。群聊收件人不查表、不扇出，只在其指定的推送应用那一轮投递一次。
//
// 三条硬约束写在代码结构里而不是文档里：
//  1. 发送循环只遍历请求分组（push_groups.<group>）的条目，身份应用凭据从不进入发送路径——
//     「告警不能走考勤机器人」由此成为结构约束；
//  2. 分组必填且不回退：分组为空是参数错误，分组未配置是 CodeNoPushApp，绝不改用全部
//     推送应用——否则「忘配分组」会退化成「用别人的机器人发」；
//  3. 单条失败不中断，明细里逐条给出状态，由调用方自行决定重试。
package send

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// MessageSender 是消息发送的最小接缝：production 用 larkim.Client，测试注入假实现。
//
// 单独抽出来是为了让编排逻辑（扇出、跳过、碰撞、聚合、按应用上传）能在不访问飞书的前提下
// 测到，而不是为了替换 larkim。
type MessageSender interface {
	// SendText 向某个接收者发送一条文本，返回飞书 message_id。
	SendText(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
	// SendInteractiveCard 向某个接收者发送一张交互卡片，返回飞书 message_id。
	SendInteractiveCard(ctx context.Context, receiveIDType, receiveID, cardJSON string) (string, error)
	// UploadFile 上传文件，返回仅本应用可用的 file_key。
	UploadFile(ctx context.Context, fileType, fileName string, content []byte) (string, error)
	// UploadImage 上传用于消息的图片，返回仅本应用可用的 image_key。
	UploadImage(ctx context.Context, content []byte) (string, error)
	// SendFile 用 file_key 向某个接收者发送文件消息，返回飞书 message_id。
	SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) (string, error)
	// SendImage 用 image_key 向某个接收者发送图片消息，返回飞书 message_id。
	SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) (string, error)
}

// ClientFactory 按推送应用凭据构造 MessageSender。由装配层（plugin.go）传入真实实现，
// 测试传入假发送器，本组件不自行决定用哪个客户端。
type ClientFactory func(appID, appSecret string) MessageSender

// IndexPath 取「身份应用 → 工号/user_id 索引」的来源。由装配层传入 empcap 门面，
// 测试传入内存索引，本组件不直接依赖 empcap 的全局绑定。
type IndexPath func(ctx context.Context, identityAppID string) (empcap.LarkUserIDIndex, error)

// Service 是推送编排组件对外的默认接口，即门面 larkpushcap.Sender 的完整实现。
//
// 这里直接复用 owner 侧已发布的接口而不是重写一份等价方法集：调用方只认 larkpushcap.Sender，
// 多定义一层会形成两份需要同步维护的契约。
type Service interface {
	larkpushcap.Sender
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现。
type serviceImpl struct {
	// hostConfig 用于读取 lark 段；为 nil 时所有请求返回 CodeHostConfigMissing（宿主未暴露
	// 配置能力属预期的降级状态，不是装配错误）。
	hostConfig hostconfigcap.Service
	// newClient 按推送应用凭据构造发送器。
	newClient ClientFactory
	// indexPath 取「身份应用 → 工号映射」。
	indexPath IndexPath
	// alerter 报告部署与数据缺陷；为 nil 表示告警通道未配置（属预期降级，不告警）。
	alerter alert.Service
}

// New 构造推送编排服务。
//
// newClient 与 indexPath 是必填的运行期依赖，缺失说明装配层漏传，直接返回错误由上层决定
// 是否中止进程；hostConfig 允许为 nil——宿主未暴露配置能力时按「配置不可用」失败关闭，
// 由 Codes 中的 CodeHostConfigMissing 体现，这是规范定义的降级路径而非装配缺陷。
// alerter 允许为 nil：告警通道未配置时静默不告警，不影响推送。
func New(
	hostConfig hostconfigcap.Service,
	newClient ClientFactory,
	indexPath IndexPath,
	alerter alert.Service,
) (Service, error) {
	if newClient == nil {
		return nil, gerror.New("lina-plugin-linapro-lark-push: 构造推送服务失败：发送器工厂未注入")
	}
	if indexPath == nil {
		return nil, gerror.New("lina-plugin-linapro-lark-push: 构造推送服务失败：寻址来源未注入")
	}
	return &serviceImpl{hostConfig: hostConfig, newClient: newClient, indexPath: indexPath, alerter: alerter}, nil
}

// notify 在告警通道已配置时报告一个缺陷。alerter 为 nil 时什么都不做。
func (s *serviceImpl) notify(ctx context.Context, ev alert.Event) {
	if s.alerter != nil {
		s.alerter.Notify(ctx, ev)
	}
}
