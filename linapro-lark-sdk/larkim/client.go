// Package larkim 封装飞书 IM 出站消息发送能力：向指定接收者
// （open_id / user_id / email / chat_id）发送交互卡片（msg_type=interactive）。
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
	"encoding/json"

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

// msgTypeInteractive 表示交互卡片消息类型。
const msgTypeInteractive = "interactive"

// Client 使用飞书 app 凭据发送 IM 消息，复用 larkhttp 的限流退避。
// 官方 SDK 依据 appID/appSecret 自动管理 tenant access token。
type Client struct {
	client *larkclient.Client
}

// NewClient 依据飞书 app 凭证构造 Client，注入 larkhttp 的 429 退避 HttpClient。
func NewClient(appID, appSecret string) *Client {
	return &Client{
		client: larkclient.NewClient(appID, appSecret, larkclient.WithHttpClient(larkhttp.NewClient())),
	}
}

// SendInteractiveCard 向指定接收者发送一张交互卡片。
//
// receiveIDType 取 ReceiveIDTypeOpenID / ReceiveIDTypeUserID /
// ReceiveIDTypeEmail / ReceiveIDTypeChatID；receiveID 为对应类型的接收者 ID。
// cardJSON 为飞书 interactive 卡片的 schema JSON 串（可经 BuildActionCard 构造）。
//
// 成功返回飞书 message_id。发送经 larkhttp 自动退避 429 限流，失败时返回带
// req_id/code/msg 的 gerror，供调用方按评价者粒度计入未推送清单。
func (c *Client) SendInteractiveCard(ctx context.Context, receiveIDType, receiveID, cardJSON string) (string, error) {
	if receiveIDType == "" || receiveID == "" {
		return "", gerror.New("larkim: receiveIDType 与 receiveID 均不能为空")
	}
	req := larkimv1.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkimv1.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgTypeInteractive).
			Content(cardJSON).
			Build()).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 发送交互卡片请求失败")
	}
	if !resp.Success() {
		return "", gerror.Newf("larkim: 发送交互卡片失败 req_id=%s code=%d msg=%s%s",
			resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", gerror.New("larkim: 发送交互卡片未返回 message_id")
	}
	return larkcore.StringValue(resp.Data.MessageId), nil
}

// actionCardSchema 是飞书 interactive 卡片的最小可发送结构。
// 仅显式定义 larkim 需要拼装的字段；其余字段交飞书默认渲染。
type actionCardSchema struct {
	Schema       string           `json:"schema"` // 固定 "2.0"
	CardLink     *cardLink        `json:"card_link,omitempty"`
	Header       *cardHeader      `json:"header,omitempty"`
	Body         cardBody         `json:"body"`
	BodyElements []bodyElement    `json:"body_elements,omitempty"`
	Elements     []markdownColumn `json:"elements,omitempty"` // 兼容 1.0 卡片 column 元素
}

type cardLink struct {
	URL string `json:"url"`
}

type cardHeader struct {
	Title    cardText `json:"title"`
	Template string   `json:"template,omitempty"` // 颜色模板，如 "blue"
}

type cardBody struct {
	Elements []bodyElement `json:"elements"`
}

type bodyElement struct {
	Tag     string    `json:"tag"`
	Content string    `json:"content,omitempty"`
	Text    *cardText `json:"text,omitempty"`
	Actions []action  `json:"actions,omitempty"`
}

type cardText struct {
	Tag     string `json:"tag"`               // "plain_text" 或 "lark_md"
	Content string `json:"content,omitempty"` // plain_text 用
	Lines   []line `json:"lines,omitempty"`   // lark_md 多行
}

type line struct {
	Text string `json:"text"`
}

type action struct {
	Tag      string   `json:"tag"` // "button"
	Text     cardText `json:"text"`
	Type     string   `json:"type,omitempty"`
	URL      string   `json:"url,omitempty"`
	MultiURL multiURL `json:"multi_url,omitempty"`
}

type multiURL struct {
	URL string `json:"url"`
}

type markdownColumn struct {
	Tag      string   `json:"tag"`
	Text     cardText `json:"text,omitempty"`
	Elements []action `json:"elements,omitempty"`
}

// BuildActionCard 把「标题 + markdown 正文 + 按钮文案 + 按钮 URL」拼装为
// 飞书 interactive 卡片 JSON，供消费方无需了解卡片 JSON 细节即可发送
// "立即填写" 类卡片。markdown 为飞书 lark_md 串；btn 为空则不生成按钮。
func BuildActionCard(title, markdown, btnText, btnURL string) (string, error) {
	if title == "" && markdown == "" {
		return "", gerror.New("larkim: 标题与正文不能同时为空")
	}
	card := actionCardSchema{
		Schema: "2.0",
	}
	if title != "" {
		card.Header = &cardHeader{
			Title:    cardText{Tag: "plain_text", Content: title},
			Template: "blue",
		}
	}
	bodyElems := []bodyElement{}
	if markdown != "" {
		// lark_md 多行：按换行拆成 lines。
		lines := splitLines(markdown)
		mdLines := make([]line, 0, len(lines))
		for _, l := range lines {
			mdLines = append(mdLines, line{Text: l})
		}
		bodyElems = append(bodyElems, bodyElement{
			Tag:  "div",
			Text: &cardText{Tag: "lark_md", Lines: mdLines},
		})
	}
	if btnText != "" && btnURL != "" {
		bodyElems = append(bodyElems, bodyElement{
			Tag: "action",
			Actions: []action{{
				Tag:      "button",
				Text:     cardText{Tag: "plain_text", Content: btnText},
				Type:     "primary",
				MultiURL: multiURL{URL: btnURL},
			}},
		})
	}
	card.Body = cardBody{Elements: bodyElems}

	b, err := json.Marshal(card)
	if err != nil {
		return "", gerror.Wrapf(err, "larkim: 序列化卡片 JSON 失败")
	}
	return string(b), nil
}

// splitLines 按换行拆串，丢弃空行尾的空串，便于 lark_md lines 渲染。
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	// 去尾空串
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
