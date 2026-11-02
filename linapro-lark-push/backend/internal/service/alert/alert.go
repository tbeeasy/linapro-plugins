// Package alert 把 linapro-lark-push 自身的部署缺陷与数据缺陷推送到飞书告警群。
//
// 它与业务推送完全独立：凭据取自独立配置块 plugin.linapro-lark-push.alert，发送器由装配层
// 直接构造，不经过 send 组件、也不读 push_groups。原因是最需要告警的时刻恰恰是 push_groups
// 损坏或推送能力装配失败的时刻——若复用业务通道，告警会跟着一起哑掉。
//
// 覆盖边界（写在这里而不是只写在文档里，是因为它决定了后续故障排查的预期）：
//   - 只报告「插件自己还活着、且自己知道出了问题」的事件；
//   - 插件未启用、装配失败到进程级、进程已退出这三类故障无法自我报告，必须由外部探针轮询
//     /open/v1/status 的 503 发现；
//   - 签名类错误（缺头、时间戳超窗、签名不匹配、nonce 重放、app_key 未知）属于调用方侧问题，
//     会被扫描器反复触发，一律不告警。
package alert

import (
	"context"

	"lina-core/pkg/plugin/capability/cachecap"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// Kind 是告警事件种类，取值同时用作防抖键的一部分，必须保持稳定。
type Kind string

const (
	// KindAssemblyFailed 推送服务构造失败（装配层漏传依赖）。
	KindAssemblyFailed Kind = "assembly_failed"
	// KindNoPushApp 请求分组没有任何可用推送应用。
	KindNoPushApp Kind = "no_push_app"
	// KindConfigInvalid 分组配置结构非法。
	KindConfigInvalid Kind = "config_invalid"
	// KindEmpcapUnbound 员工能力未绑定，无法把工号解析为 user_id。
	KindEmpcapUnbound Kind = "empcap_unbound"
	// KindCredentialStoreUnavailable 签名凭据存储不可用。
	KindCredentialStoreUnavailable Kind = "credential_store_unavailable"
	// KindNonceStoreUnavailable nonce 去重存储不可用。
	KindNonceStoreUnavailable Kind = "nonce_store_unavailable"
	// KindBlankUserID 某企业整表的 lark_user_id 为空，通常是身份应用缺少获取 user_id 的权限。
	KindBlankUserID Kind = "blank_user_id"
	// KindCollision 工号在同一企业命中多个飞书账号，投递被拒绝。
	KindCollision Kind = "collision"
)

// Label 返回事件种类的中文描述，用于告警正文。
func (k Kind) Label() string {
	switch k {
	case KindAssemblyFailed:
		return "推送服务构造失败"
	case KindNoPushApp:
		return "推送分组没有可用推送应用"
	case KindConfigInvalid:
		return "推送分组配置非法"
	case KindEmpcapUnbound:
		return "员工能力未就绪，无法解析收件人"
	case KindCredentialStoreUnavailable:
		return "签名凭据存储不可用"
	case KindNonceStoreUnavailable:
		return "nonce 去重存储不可用"
	case KindBlankUserID:
		return "该企业员工缺少 lark_user_id"
	case KindCollision:
		return "工号对应多个飞书账号"
	default:
		return string(k)
	}
}

// Event 是一条待发送的告警。
type Event struct {
	// Kind 是事件种类，必填；空值（未常见）会被判为 Unknown 字符串直接发出。
	Kind Kind
	// Group 是推送分组名；与推送无关的告警（如装配失败）可以为空。
	Group string
	// PushAppID 是相关推送应用 app_id；可空。
	PushAppID string
	// Detail 是补充说明，直接进告警正文，不要放密钥或消息正文。
	Detail string
}

// MessageSender 是告警发送的最小接缝：production 传 larkim.Client，测试注入假实现。
type MessageSender interface {
	// SendText 向某个接收者发送一条文本，返回飞书 message_id。
	SendText(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
}

// ClientFactory 按告警应用凭据构造 MessageSender。由装配层传入，本组件不自行决定用哪个客户端。
type ClientFactory func(appID, appSecret string) MessageSender

// Service 是本插件对内的告警通道。
type Service interface {
	// Notify 在需要时向告警群推送一条事件。
	//
	// 调用方不需要（也无法）处理失败：发送错误在内部记 ERROR 日志，绝不回传、绝不递归告警。
	// 同一「种类 + 分组 + 推送应用」在防抖窗口内只发一条；告警配置留空时静默丢弃。
	// 该调用会同步等待一次飞书请求（通常被防抖挡住），因此只在已经失败的路径上调用。
	Notify(ctx context.Context, ev Event)
	// Enabled 报告告警通道是否已配置齐备（app_id / secret_key / chat_id 都存在）。
	// 供装配层在启动时提示「告警未配置」，不做任何发送。
	Enabled(ctx context.Context) bool
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现。
type serviceImpl struct {
	// hostConfig 用于读取 alert 配置；为 nil 时通道恒为未配置状态。
	hostConfig hostconfigcap.Service
	// cache 用于告警防抖；为 nil 或不可用时退化为「每次都发」，宁可重复也不漏。
	cache cachecap.Service
	// newClient 按告警应用凭据构造发送器。
	newClient ClientFactory
}

// New 构造告警通道。
//
// newClient 是必填依赖，缺失说明装配层漏传，直接返回错误；hostConfig 与 cache 允许为 nil：
// 前者表示宿主没暴露配置能力（告警自然不可用），后者表示无法防抖（退化为每次发送）。
func New(hostConfig hostconfigcap.Service, cache cachecap.Service, newClient ClientFactory) (Service, error) {
	if newClient == nil {
		return nil, ErrClientFactoryMissing
	}
	return &serviceImpl{hostConfig: hostConfig, cache: cache, newClient: newClient}, nil
}
