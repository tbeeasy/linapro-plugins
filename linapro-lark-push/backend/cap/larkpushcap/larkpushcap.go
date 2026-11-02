// Package larkpushcap 是 linapro-lark-push 的公开门面。其它插件通过 go.mod 的
// `replace` 导入它，以「工号 + 文本 / 卡片」请求投递飞书消息，无需了解飞书应用与企业概念。
//
// 消息形态有三种：纯文本（SendText）、文件 / 图片（SendFile），二者进程内与对外 HTTP 共用；
// 交互卡片（SendCard）仅进程内，卡片内容由调用方按推送应用构造，本插件只负责寻址与投递。
//
// 收件人可以是人（工号 / user_id，按企业自动扇出），也可以是群聊（chat_id，只经一个推送应用
// 投递一次，见 Recipient）。
//
// 推送应用按调用方分组（配置 plugin.linapro-lark-push.push_groups.<分组名>）：每个请求必须
// 携带 Group（进程内调用方取自己的插件 ID），只用该分组的推送应用投递，接收方看到的就是
// 该调用方自己的机器人。分组缺失或未配置时失败关闭，绝不回退为全部推送应用。
//
// 接缝是一个纯 Go 包加一个进程内绑定的 Sender（与 empcap、extidcap 对齐）：
// 拥有方插件在初始化时绑定实现，消费方每次调用懒取当前绑定。未绑定时返回明确的
// 「能力未就绪」错误，绝不静默成功或丢弃消息。
//
// 本包只定义契约与绑定，不导入 lina-core 之外的宿主实现，也不含任何发送逻辑。
package larkpushcap

import (
	"context"
	"sync"

	"github.com/gogf/gf/v2/errors/gerror"
)

// OwnerPluginID 是拥有并绑定推送实现的插件。
const OwnerPluginID = "linapro-lark-push"

// RecipientKind 标识收件人的寻址方式。
type RecipientKind string

const (
	// RecipientKindEmployeeNo 以工号为锚点：系统按「此人在哪些企业有身份」自动扇出。
	// 这是常规用法——调用方通常只知道工号。
	RecipientKindEmployeeNo RecipientKind = "employee_no"
	// RecipientKindUserID 直接给出企业级 user_id：调用方已经持有该标识时使用，
	// 跳过工号解析，但仍按各推送应用投递（user_id 按企业签发，跨企业不可混用）。
	RecipientKindUserID RecipientKind = "user_id"
	// RecipientKindChatID 是群聊：chat_id 归属单个企业、且只有已在群内的机器人才能发，因此
	// 不扇出，只经 PushAppID 指定（分组仅一个推送应用时可省略）的那个推送应用投递一次。
	RecipientKindChatID RecipientKind = "chat_id"
)

// Recipient 是一个收件人。三种写法互斥：
//   - 人：EmployeeNo 与 UserID 二选一，同时给出时以 EmployeeNo 为准（工号是更稳定、更易得的
//     寻址锚点）；
//   - 群聊：只给 ChatID，并按需给 PushAppID。ChatID 与工号 / user_id 同时给出属于参数错误。
type Recipient struct {
	// EmployeeNo 是 Moka 工号。
	EmployeeNo string
	// UserID 是飞书企业级 user_id。
	UserID string
	// ChatID 是飞书群聊 chat_id（oc_ 开头），仅群聊收件人使用。
	ChatID string
	// PushAppID 指定群聊由请求分组内哪个推送应用发送：分组只有一个推送应用时可省略，
	// 有多个时必填。只对群聊有意义，给人类收件人填写属于参数错误。
	PushAppID string
}

// Kind 返回该收件人的寻址方式；全部为空，或群聊与人的字段混用时返回空串
// （由服务层判为参数错误）。
func (r Recipient) Kind() RecipientKind {
	switch {
	case r.ChatID != "":
		if r.EmployeeNo != "" || r.UserID != "" {
			return ""
		}
		return RecipientKindChatID
	case r.EmployeeNo != "":
		return RecipientKindEmployeeNo
	case r.UserID != "":
		return RecipientKindUserID
	default:
		return ""
	}
}

// SendTextRequest 是一次文本投递请求。
//
// 请求里**没有企业参数**：企业由身份行推导，调用方不需要、也不应该知道某人在哪几个
// 企业有身份。已确认业务语义为「他在几家企业有身份就发几条」，双发可接受。
type SendTextRequest struct {
	// Group 是推送分组，必填：进程内调用方填自己的插件 ID（如 linapro-eval-360），
	// 只用 push_groups 中该分组的推送应用投递。为空返回参数错误。
	Group string
	// Recipients 是收件人列表，不可为空。
	Recipients []Recipient
	// Text 是消息正文（纯文本，上限见 larkim.MaxTextRunes）。
	Text string
	// BizSource 是调用方标识（如插件 ID），仅用于日志对账，不参与选路。
	BizSource string
	// BizRef 是可选业务单号（如测评项目 ID），仅用于日志追溯。
	BizRef string
}

// PushApp 是推送应用的公开投影（不含密钥），供调用方按企业准备消息内容。
type PushApp struct {
	// AppID 是推送应用 App ID（实际发送消息的机器人所属应用）。
	AppID string
	// Name 是配置里的展示名。
	Name string
	// IdentityAppID 是同企业身份（登录 / 同步）应用的 App ID。消息里的链接若要引导员工
	// 按企业免登，应携带它而不是 AppID——员工端换票使用的是身份应用。
	IdentityAppID string
}

// CardBuilder 按「推送应用 × 收件人」构造交互卡片的 content JSON
// （如 larkim.BuildTemplateCard 的产物）。群聊收件人的 r.ChatID 非空，调用方可据此
// 给群发不同的卡片。
//
// 按推送应用逐个构造，是因为飞书卡片模板归属具体应用、跨企业不可复用，链接参数也可能
// 按企业区分。返回 error 时该条投递记为失败，不影响同批其余投递。
type CardBuilder func(app PushApp, r Recipient) (string, error)

// SendCardRequest 是一次交互卡片投递请求。分组、寻址与扇出语义同 SendTextRequest。
type SendCardRequest struct {
	// Group 是推送分组，必填，语义同 SendTextRequest.Group。
	Group string
	// Recipients 是收件人列表，不可为空。
	Recipients []Recipient
	// Build 构造每条投递的卡片内容，不可为空。
	Build CardBuilder
	// BizSource 是调用方标识（如插件 ID），仅用于日志对账，不参与选路。
	BizSource string
	// BizRef 是可选业务单号（如测评项目 ID），仅用于日志追溯。
	BizRef string
	// DryRun 为 true 时只做寻址与卡片构造、不调用飞书：可投递的明细记为
	// DeliveryStatusAddressable，寻址失败与构造失败照常记各自状态。调用方据此在
	// 「全员可达才继续」的业务里先干跑一遍，避免发出半批卡片后才发现有人不可达。
	DryRun bool
}

// SendFileRequest 是一次文件投递请求。分组、寻址与扇出语义同 SendTextRequest。
//
// 内容识别为飞书支持的图片且不超过 10 MB 时以图片消息发送，其余以文件消息发送（上限 30 MB）。
// 飞书 file_key / image_key 归属上传它的应用，因此本插件对每个推送应用最多上传一次，且只在
// 该应用至少有一条可投递明细时才上传。
type SendFileRequest struct {
	// Group 是推送分组，必填，语义同 SendTextRequest.Group。
	Group string
	// Recipients 是收件人列表，不可为空。
	Recipients []Recipient
	// FileName 是接收方看到的文件名，同时用于推导飞书 file_type，不可为空。
	FileName string
	// Content 是文件内容，不可为空。
	Content []byte
	// BizSource 是调用方标识（如插件 ID），仅用于日志对账，不参与选路。
	BizSource string
	// BizRef 是可选业务单号，仅用于日志追溯。
	BizRef string
}

// DeliveryStatus 是一次投递的结果状态。
type DeliveryStatus string

const (
	// DeliveryStatusSent 已送达飞书（拿到 message_id）。
	DeliveryStatusSent DeliveryStatus = "sent"
	// DeliveryStatusAddressable 干跑（SendCardRequest.DryRun）结果：已解析出 user_id 且卡片
	// 构造成功，正式投递时会调用飞书。它不是「已送达」，Succeeded 返回 false。
	DeliveryStatusAddressable DeliveryStatus = "addressable"
	// DeliveryStatusNoIdentity 该员工在此推送应用对应的企业没有身份；工号写错或不存在也落在此状态。
	DeliveryStatusNoIdentity DeliveryStatus = "no_identity"
	// DeliveryStatusUserIDMissing 此人在该企业有 own 身份但 lark_user_id 为空：
	// 身份应用缺「获取用户 user_id」权限，或该行早于授权、等待下次同步回填。只按本人判定。
	DeliveryStatusUserIDMissing DeliveryStatus = "user_id_missing"
	// DeliveryStatusCollision 同一工号在该企业对应多个飞书账号，拒绝发送（避免送错人）。
	DeliveryStatusCollision DeliveryStatus = "collision"
	// DeliveryStatusFailed 飞书侧拒绝或网络失败。
	DeliveryStatusFailed DeliveryStatus = "failed"
)

// Delivery 是一条「收件人 × 推送应用」的投递结果。
type Delivery struct {
	// EmployeeNo 是该收件人的工号（纯 user_id 寻址或群聊时为空）。
	EmployeeNo string
	// ChatID 是群聊收件人的 chat_id（人类收件人为空）。
	ChatID string
	// UserID 是本次投递使用的企业级 user_id（未解析出时为空）。
	UserID string
	// PushAppID 是实际使用的推送应用 App ID（凭据来源）。
	PushAppID string
	// PushAppName 是推送应用的配置名，仅用于日志与展示。
	PushAppName string
	// Status 是结果状态。
	Status DeliveryStatus
	// MessageID 是飞书 message_id（仅 Status=sent 时非空）。
	MessageID string
	// FeishuCode 是飞书错误码（仅失败时非空）。
	FeishuCode int
	// FeishuMsg 是飞书错误描述。
	FeishuMsg string
	// FeishuReqID 是飞书请求 ID（失败时用于向飞书侧追查）。
	FeishuReqID string
	// Error 是失败原因的人类可读描述（含本地错误）。
	Error string
}

// Succeeded 报告该条投递是否成功。
func (d Delivery) Succeeded() bool { return d.Status == DeliveryStatusSent }

// SendResult 是一次请求的汇总结果。
type SendResult struct {
	// Deliveries 是逐收件人 × 逐推送应用的明细。
	Deliveries []Delivery
	// BlankUserIDEmployees 是本次涉及的各企业里「有身份但 user_id 为空」的员工数之和（企业级统计，
	// 不限于本次收件人），供调用方发现「身份应用缺权限」这类部署缺陷。
	BlankUserIDEmployees int
	// Collisions 是本次涉及的各企业里「同一工号对应多个飞书账号」的员工数之和（企业级统计）。
	Collisions int
}

// AllSucceeded 报告是否所有明细都成功。收件人为空时返回 false（空请求本身就是失败）。
func (r SendResult) AllSucceeded() bool {
	if len(r.Deliveries) == 0 {
		return false
	}
	for _, d := range r.Deliveries {
		if !d.Succeeded() {
			return false
		}
	}
	return true
}

// Sender 是推送实现。由拥有方插件在初始化时绑定；消费方不应自行实现。
type Sender interface {
	// SendText 执行一次投递，返回逐条明细。业务失败（某收件人找不到、飞书拒绝）
	// 通过明细体现而不是返回 error；返回 error 仅表示「无法完成本次请求」
	// （参数非法含分组为空、该分组配置非法、该分组无可用推送应用、能力未就绪）。
	SendText(ctx context.Context, req SendTextRequest) (SendResult, error)
	// SendCard 投递交互卡片，寻址、扇出与错误语义同 SendText；卡片内容由 req.Build
	// 按「推送应用 × 收件人」构造，构造失败只记该条明细。req.DryRun 为 true 时不调用飞书，
	// 可投递的明细记为 DeliveryStatusAddressable。
	SendCard(ctx context.Context, req SendCardRequest) (SendResult, error)
	// SendFile 投递文件或图片，寻址、扇出与错误语义同 SendText；文件为空、超过 30 MB 或文件名
	// 为空返回参数错误。某推送应用上传失败只把该应用下的明细记为失败。
	SendFile(ctx context.Context, req SendFileRequest) (SendResult, error)
	// ListPushApps 返回 group 分组内参与投递的推送应用（已过配置校验、凭据齐备，不含密钥）。
	// group 为空返回参数错误；该分组配置非法时返回错误；分组未配置时返回空切片。
	// 调用方据此做发送前的按应用预检。
	ListPushApps(ctx context.Context, group string) ([]PushApp, error)
	// Status 返回能力自检信息（按分组列出各推送应用凭据齐备性、身份应用有效性、
	// user_id 覆盖情况），供部署验收与告警使用。
	Status(ctx context.Context) StatusReport
}

var (
	bindMu      sync.RWMutex
	boundSender Sender
)

// BindSender 装配拥有方插件的实现。传入 nil 会清空绑定。
func BindSender(s Sender) {
	bindMu.Lock()
	defer bindMu.Unlock()
	boundSender = s
}

// BoundSender 返回绑定的实现，未绑定时返回 nil。
func BoundSender() Sender {
	bindMu.RLock()
	defer bindMu.RUnlock()
	return boundSender
}

// ErrSenderUnbound 在插件未启用或装配失败时返回；调用方据此降级，
// 绝不当成「已发送」。
var ErrSenderUnbound = gerror.New("larkpushcap: 飞书推送能力未就绪（linapro-lark-push 未启用或装配失败）")

// SendText 是包级便捷函数：懒取当前绑定并投递。未绑定时返回 ErrSenderUnbound。
func SendText(ctx context.Context, req SendTextRequest) (SendResult, error) {
	s := BoundSender()
	if s == nil {
		return SendResult{}, ErrSenderUnbound
	}
	return s.SendText(ctx, req)
}

// SendCard 是包级便捷函数：懒取当前绑定并投递卡片。未绑定时返回 ErrSenderUnbound。
func SendCard(ctx context.Context, req SendCardRequest) (SendResult, error) {
	s := BoundSender()
	if s == nil {
		return SendResult{}, ErrSenderUnbound
	}
	return s.SendCard(ctx, req)
}

// SendFile 是包级便捷函数：懒取当前绑定并投递文件。未绑定时返回 ErrSenderUnbound。
func SendFile(ctx context.Context, req SendFileRequest) (SendResult, error) {
	s := BoundSender()
	if s == nil {
		return SendResult{}, ErrSenderUnbound
	}
	return s.SendFile(ctx, req)
}

// ListPushApps 是包级便捷函数：懒取当前绑定并列出 group 分组的推送应用。
// 未绑定时返回 ErrSenderUnbound。
func ListPushApps(ctx context.Context, group string) ([]PushApp, error) {
	s := BoundSender()
	if s == nil {
		return nil, ErrSenderUnbound
	}
	return s.ListPushApps(ctx, group)
}
