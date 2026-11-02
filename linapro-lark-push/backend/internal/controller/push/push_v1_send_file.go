// push_v1_send_file.go 实现 POST /open/v1/message/file：一步式「上传即推送」。
//
// 请求为 multipart/form-data，只接受 file / recipients / biz_ref 三个字段（其余按参数错误拒绝）；
// 群聊取自签名凭据配置，调用方不能指定。表单已由 bodylimit 中间件在 31 MB 上限内解析到内存，
// 文件内容只在内存中流转，处理完即丢弃，不落盘、不持久化。

package push

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"strings"

	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"

	"linapro-lark-sdk/larkim"

	v1 "lina-plugin-linapro-lark-push/backend/api/push/v1"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/middleware/signature"
)

// allowedFileFormValues 是文件接口允许的普通表单字段。
var allowedFileFormValues = map[string]struct{}{
	v1.FileFormRecipients: {},
	v1.FileFormBizRef:     {},
}

// SendFile 按签名凭据绑定的分组投递文件或图片。框架参数绑定不校验未知字段与重复字段，
// 因此这里按已解析的原始表单严格校验。
func (c *ControllerV1) SendFile(ctx context.Context, _ *v1.SendFileReq) (res *v1.SendFileRes, err error) {
	r := g.RequestFromCtx(ctx)
	if c.sender == nil {
		return nil, larkpushcode.WithHTTPStatus(r, bizerr.NewCode(larkpushcode.CodeSenderUnbound))
	}
	if r.Request.MultipartForm == nil {
		return nil, invalidParam("请求体不是合法的 multipart/form-data")
	}
	form, reason := readFileForm(ctx, r.Request.MultipartForm)
	if reason != "" {
		return nil, invalidParam(reason)
	}
	identity, _ := signature.FromContext(ctx)
	recipients := toRecipients(form.recipients, identity)
	if len(recipients) == 0 {
		return nil, invalidParam(noRecipientReason)
	}
	result, sendErr := c.sender.SendFile(ctx, larkpushcap.SendFileRequest{
		Group:      identity.Group,
		Recipients: recipients,
		FileName:   form.fileName,
		Content:    form.content,
		BizSource:  bizSourceExternal,
		BizRef:     form.bizRef,
	})
	out, err := finishSend(ctx, r, identity, form.bizRef, len(recipients), result, sendErr)
	if out == nil {
		return nil, err
	}
	return &v1.SendFileRes{SendResult: *out}, err
}

// fileForm 是解析后的文件接口表单。
type fileForm struct {
	// fileName 是上传文件的原始文件名。
	fileName string
	// content 是文件内容。
	content []byte
	// recipients 是个人收件人。
	recipients []v1.RecipientReq
	// bizRef 是可选业务单号。
	bizRef string
}

// readFileForm 校验已解析的 multipart 表单并取出各字段；失败时返回面向调用方的错误原因。
func readFileForm(ctx context.Context, mf *multipart.Form) (fileForm, string) {
	var form fileForm
	defer func() {
		// 解析可能产生临时文件（超出内存上限时），无论成败都要清理。
		if rmErr := mf.RemoveAll(); rmErr != nil {
			logger.Warningf(ctx, "lark-push http: 清理 multipart 临时文件失败: %v", rmErr)
		}
	}()
	for name, values := range mf.Value {
		if _, ok := allowedFileFormValues[name]; !ok {
			return form, "未知表单字段: " + name
		}
		if len(values) != 1 {
			return form, "表单字段 " + name + " 只能出现一次"
		}
	}
	for name := range mf.File {
		if name != v1.FileFormFile {
			return form, "未知文件字段: " + name
		}
	}
	files := mf.File[v1.FileFormFile]
	if len(files) != 1 {
		return form, "必须且只能上传一个 file 字段"
	}
	if raw := firstValue(mf.Value, v1.FileFormRecipients); raw != "" {
		if err := decodeStrict(raw, &form.recipients); err != nil {
			return form, "recipients 不是合法的 JSON 数组或含未知字段: " + err.Error()
		}
	}
	form.bizRef = firstValue(mf.Value, v1.FileFormBizRef)
	fh := files[0]
	if fh.Size > larkim.MaxFileBytes {
		return form, "文件超过 30 MB 上限"
	}
	f, err := fh.Open()
	if err != nil {
		return form, "读取上传文件失败: " + err.Error()
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			logger.Warningf(ctx, "lark-push http: 关闭上传文件失败: %v", closeErr)
		}
	}()
	content, err := io.ReadAll(io.LimitReader(f, larkim.MaxFileBytes+1))
	if err != nil {
		return form, "读取上传文件失败: " + err.Error()
	}
	if len(content) > larkim.MaxFileBytes {
		return form, "文件超过 30 MB 上限"
	}
	form.fileName = fh.Filename
	form.content = content
	return form, ""
}

// firstValue 返回表单字段的唯一取值（调用前已校验最多一个）。
func firstValue(values map[string][]string, name string) string {
	if vs := values[name]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// decodeStrict 按 JSON 解码并拒绝未知字段，与文本接口的请求体口径一致。
func decodeStrict(raw string, out any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}
