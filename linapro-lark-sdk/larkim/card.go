// card.go 提供交互卡片消息（msg_type=interactive）的构造与发送。
//
// 卡片版式由飞书卡片搭建工具中的模板维护，发送方经 BuildTemplateCard 只提交
// 模板 ID、版本与模板变量；SendInteractiveCard 对 content 原样下发，不做二次校验。

package larkim

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

// msgTypeInteractive 表示交互卡片消息类型。
const msgTypeInteractive = "interactive"

// SendInteractiveCard 向指定接收者发送一张交互卡片。
//
// receiveIDType 取 ReceiveIDTypeOpenID / ReceiveIDTypeUserID /
// ReceiveIDTypeEmail / ReceiveIDTypeChatID；receiveID 为对应类型的接收者 ID。
// cardJSON 为 interactive 消息的 content JSON 串（引用卡片模板时经 BuildTemplateCard 构造）。
//
// 成功返回飞书 message_id。发送经 larkhttp 自动退避 429 限流，失败时返回带
// req_id/code/msg 的 gerror，供调用方按评价者粒度计入未推送清单。
func (c *Client) SendInteractiveCard(ctx context.Context, receiveIDType, receiveID, cardJSON string) (string, error) {
	return c.createMessage(ctx, receiveIDType, receiveID, msgTypeInteractive, cardJSON, msgKindInteractive)
}

// cardTypeTemplate 是模板卡片的 content.type 固定值：卡片结构由飞书卡片搭建工具中的
// 模板承载，发送方只提交模板 ID、版本与模板变量。
const cardTypeTemplate = "template"

// templateCardContent 是 msg_type=interactive 时引用卡片模板的 content 结构：
// {"type":"template","data":{"template_id":...,"template_version_name":...,"template_variable":{...}}}。
type templateCardContent struct {
	Type string           `json:"type"`
	Data templateCardData `json:"data"`
}

// templateCardData 是模板卡片的引用数据。template_version_name 为空时省略，
// 由飞书使用该模板当前已发布的版本。
type templateCardData struct {
	TemplateID          string         `json:"template_id"`
	TemplateVersionName string         `json:"template_version_name,omitempty"`
	TemplateVariable    map[string]any `json:"template_variable,omitempty"`
}

// BuildTemplateCard 把「卡片模板 ID + 版本名 + 模板变量」拼装为 interactive 消息的
// content JSON 串，可直接传给 SendInteractiveCard。卡片版式由飞书卡片模板维护，
// 消费方只需按模板声明的变量名填值（如按钮跳转 URL、称呼）。
//
// templateID 必填；versionName 为空表示使用模板当前发布版本；variables 为空则不下发变量。
func BuildTemplateCard(templateID, versionName string, variables map[string]any) (string, error) {
	if templateID == "" {
		return "", gerror.New("larkim: card template id is required")
	}
	content := templateCardContent{
		Type: cardTypeTemplate,
		Data: templateCardData{
			TemplateID:          templateID,
			TemplateVersionName: versionName,
			TemplateVariable:    variables,
		},
	}
	b, err := json.Marshal(content)
	if err != nil {
		return "", gerror.Wrap(err, "larkim: marshal template card content failed")
	}
	return string(b), nil
}
