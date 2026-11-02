// push_v1_send_text.go 实现 POST /open/v1/message/text：文本推送。

package push

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"

	v1 "lina-plugin-linapro-lark-push/backend/api/push/v1"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
)

// SendText 按签名凭据绑定的分组投递文本。
//
// 框架的参数绑定会静默忽略未知字段，因此这里按原始请求体严格解码：请求体带 group / chats 等
// 未知字段时以参数错误拒绝，持凭据者无法自选分组或群聊。请求体上限由 bodylimit 中间件把关。
func (c *ControllerV1) SendText(ctx context.Context, _ *v1.SendTextReq) (res *v1.SendTextRes, err error) {
	r := g.RequestFromCtx(ctx)
	if c.sender == nil {
		return nil, larkpushcode.WithHTTPStatus(r, bizerr.NewCode(larkpushcode.CodeSenderUnbound))
	}
	var body v1.SendTextReq
	dec := json.NewDecoder(bytes.NewReader(r.GetBody()))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&body); err != nil {
		return nil, invalidParam("请求体不是合法 JSON 或含未知字段: " + err.Error())
	}
	identity, _ := signature.FromContext(ctx)
	recipients := toRecipients(body.Recipients, identity)
	if len(recipients) == 0 {
		return nil, invalidParam(noRecipientReason)
	}
	// 分组与群聊只取自签名凭据，BizSource 由接口侧统一填入（外部调用）。
	result, sendErr := c.sender.SendText(ctx, larkpushcap.SendTextRequest{
		Group:      identity.Group,
		Recipients: recipients,
		Text:       body.Text,
		BizSource:  bizSourceExternal,
		BizRef:     body.BizRef,
	})
	out, err := finishSend(ctx, r, identity, body.BizRef, len(recipients), result, sendErr)
	if out == nil {
		return nil, err
	}
	return &v1.SendTextRes{SendResult: *out}, err
}
