// text.go 提供纯文本消息（msg_type=text）的构造与发送。
//
// 飞书对 text 消息的 content 结构是 {"text":"..."}，文本里的换行、引号等都必须由
// 调用方编码成合法 JSON——手工拼串极易在引号/反斜杠/换行上生成非法 JSON，故统一经
// BuildTextContent 编码，发送侧只接受已编码的 content。

package larkim

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"
)

// msgTypeText 表示纯文本消息类型。
const msgTypeText = "text"

// MaxTextRunes 是单条文本消息的长度上限（按字符计，非字节）。
//
// 飞书 text 消息单条上限约 30KB，本库取更保守的 2000 字符：告警、通知类消息远短于此，
// 而超限由调用方自行拆条比库内静默截断更安全（截断会丢关键信息且调用方无从察觉）。
const MaxTextRunes = 2000

// textContent 是 msg_type=text 的 content 结构。
type textContent struct {
	Text string `json:"text"`
}

// BuildTextContent 把纯文本编码为 text 消息的 content JSON 串，可直接传给 SendText。
//
// 编码走 encoding/json，因此引号、反斜杠、换行、制表符与控制字符都会被正确转义，
// 非 BMP 字符（emoji、生僻字）按 UTF-8 原样输出。空文本与超长文本返回参数错误，
// 不做截断。
func BuildTextContent(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", gerror.New("larkim: 文本消息内容不能为空")
	}
	if !utf8.ValidString(text) {
		return "", gerror.New("larkim: 文本消息内容不是合法的 UTF-8")
	}
	if n := utf8.RuneCountInString(text); n > MaxTextRunes {
		return "", gerror.Newf("larkim: 文本消息超长（%d 字符，上限 %d）", n, MaxTextRunes)
	}
	b, err := json.Marshal(textContent{Text: text})
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 编码文本消息内容失败")
	}
	return string(b), nil
}

// SendText 向指定接收者发送一条纯文本消息。
//
// receiveIDType 取 ReceiveIDTypeOpenID / ReceiveIDTypeUserID /
// ReceiveIDTypeEmail / ReceiveIDTypeChatID；receiveID 为对应类型的接收者 ID。
// 文本经 BuildTextContent 校验与编码，为空或超长时直接返回参数错误、不发起飞书请求。
//
// 成功返回飞书 message_id。发送经 larkhttp 自动退避 429 限流，失败时返回带
// req_id/code/msg 的 gerror。
func (c *Client) SendText(ctx context.Context, receiveIDType, receiveID, text string) (string, error) {
	content, err := BuildTextContent(text)
	if err != nil {
		return "", err
	}
	return c.createMessage(ctx, receiveIDType, receiveID, msgTypeText, content, msgKindText)
}
