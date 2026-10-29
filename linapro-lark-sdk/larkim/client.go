// Package larkim 封装飞书 IM 出站消息发送能力：向指定接收者
// （open_id / user_id / email / chat_id）发送交互卡片（msg_type=interactive，见 card.go）、
// 纯文本（msg_type=text，见 text.go），以及文件 / 图片（先上传再发送，见 file.go）。
//
// 它复用同库 larkhttp 的 429 限流退避重试，不重复实现 token 管理——
// tenant_access_token 的获取与刷新由官方 SDK 经注入的 larkhttp.Client 自动处理
// （SDK 的 token 请求与业务请求走同一个 HttpClient，故 token 刷新被限流时同样退避重试）。
//
// 边界约束：
//   - 只做出站发送，MUST NOT 实现事件订阅或回调接收；
//   - 依赖方向为 larkim → larkhttp（同库内），MUST NOT 引入 lina-core。
//
// 注：larkhttp 当前源码层 import 了 lina-core/pkg/logger（用于 429 退避日志），
// 该 import 经 go.work 解析；larkim 自身不新增任何 lina-core 引用。
package larkim

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	larkclient "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"linapro-lark-sdk/larkhttp"
	"linapro-lark-sdk/utils"
)

// 接收者 ID 类型常量，对应飞书 receive_id_type 查询参数。
const (
	// ReceiveIDTypeOpenID 以应用内 open_id 标识用户。360 测评推送默认使用它，
	// 因为 empcap 返回的 own 身份 open_id 按应用作用域签发，必须经该应用凭据投递。
	ReceiveIDTypeOpenID = "open_id"
	// ReceiveIDTypeUserID 以租户内 user_id 标识用户。
	ReceiveIDTypeUserID = "user_id"
	// ReceiveIDTypeEmail 以用户邮箱标识用户。
	ReceiveIDTypeEmail = "email"
	// ReceiveIDTypeChatID 以群 ID 标识群聊。
	ReceiveIDTypeChatID = "chat_id"
)

// msgKindInteractive / msgKindText 是错误信息里对消息种类的称呼，用于让失败日志
// 一眼看出是哪类消息发失败了。抽 createMessage 后共用一条发送路径，错误文本仍按
// 消息种类区分，既有卡片调用的错误信息保持原样。
const (
	msgKindInteractive = "交互卡片"
	msgKindText        = "文本"
	msgKindFile        = "文件"
	msgKindImage       = "图片"
)

// messageCreator 是飞书「发送消息」接口的最小接缝：官方 SDK 的 Im.V1.Message
// 结构化满足它。抽成接口只为让单测能注入假实现，生产路径始终走官方 SDK。
type messageCreator interface {
	Create(ctx context.Context, req *larkimv1.CreateMessageReq, options ...larkcore.RequestOptionFunc) (*larkimv1.CreateMessageResp, error)
}

// Client 使用飞书 app 凭据发送 IM 消息，复用 larkhttp 的限流退避。
// 官方 SDK 依据 appID/appSecret 自动管理 tenant access token。
type Client struct {
	client *larkclient.Client
	// messages 是发送消息接缝，nil 时使用 c.client.Im.V1.Message（见 messagesOrDefault）。
	messages messageCreator
	// files 是上传文件接缝，nil 时使用 c.client.Im.V1.File（见 file.go 的 filesOrDefault）。
	files fileCreator
	// images 是上传图片接缝，nil 时使用 c.client.Im.V1.Image（见 file.go 的 imagesOrDefault）。
	images imageCreator
}

// NewClient 依据飞书 app 凭证构造 Client，注入 larkhttp 的 429 退避 HttpClient。
func NewClient(appID, appSecret string) *Client {
	c := larkclient.NewClient(appID, appSecret, larkclient.WithHttpClient(larkhttp.NewClient()))
	return &Client{client: c, messages: c.Im.V1.Message, files: c.Im.V1.File, images: c.Im.V1.Image}
}

// messagesOrDefault 返回发送消息接缝；未显式注入时回退到官方 SDK 实现。
func (c *Client) messagesOrDefault() messageCreator {
	if c.messages != nil {
		return c.messages
	}
	return c.client.Im.V1.Message
}

// createMessage 是消息发送的共用请求构造与结果解析路径：各类消息（交互卡片、文本、
// 文件、图片）只在 msg_type 与 content 上不同，其余（接收者校验、限流退避、错误映射）完全一致。
// kind 仅用于错误信息里的消息种类称呼，不参与请求构造。
//
// 成功返回飞书 message_id；失败返回带 req_id/code/msg 的 gerror。
func (c *Client) createMessage(ctx context.Context, receiveIDType, receiveID, msgType, content, kind string) (string, error) {
	if receiveIDType == "" || receiveID == "" {
		return "", gerror.New("larkim: receiveIDType 与 receiveID 均不能为空")
	}
	req := larkimv1.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkimv1.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	resp, err := c.messagesOrDefault().Create(ctx, req)
	if err != nil {
		return "", gerror.Wrapf(err, "larkim: 发送%s请求失败", kind)
	}
	if !resp.Success() {
		// 返回结构化错误：调用方需要 code/req_id 写日志，又不该解析字符串。
		return "", newFeishuError(kind, resp.Code, resp.Msg, resp.RequestId(), utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", gerror.Newf("larkim: 发送%s未返回 message_id", kind)
	}
	return larkcore.StringValue(resp.Data.MessageId), nil
}
