// send_file.go 实现文件 / 图片投递。
//
// 与文本、卡片共用同一条寻址与扇出编排（dispatch / deliverOne），区别在最后一步：飞书
// file_key / image_key 归属上传它的应用，所以每个推送应用要用自己的凭据上传一次再发送。
// 上传是懒执行的——某推送应用出现第一条可投递明细时才上传，结果（key 或错误）在本次请求内
// 按应用缓存，同应用后续明细直接复用；上传失败只把该应用下的明细记为 failed。

package send

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/logger"

	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
)

// filePayload 是一次文件投递的不可变内容与发送形态。
type filePayload struct {
	// name 是接收方看到的文件名（已去掉路径部分）。
	name string
	// fileType 是飞书 file_type，仅按文件发送时使用。
	fileType string
	// content 是文件内容。
	content []byte
	// asImage 为 true 时上传为图片并发送 image 消息，否则上传为文件并发送 file 消息。
	asImage bool
}

// uploadResult 是某推送应用的一次上传结果。
type uploadResult struct {
	// key 是 file_key 或 image_key。
	key string
	// err 是上传失败原因；非 nil 时 key 无意义。
	err error
}

// SendFile 执行一次文件投递，错误语义同 SendText：返回 error 仅表示「无法完成本次请求」，
// 收件人级别的失败（含某推送应用上传失败）体现在 Deliveries 里。
func (s *serviceImpl) SendFile(ctx context.Context, req larkpushcap.SendFileRequest) (larkpushcap.SendResult, error) {
	group, err := normalizeGroup(req.Group)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	payload, err := newFilePayload(req.FileName, req.Content)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	recipients, err := normalizeRecipients(req.Recipients)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	pushApps, err := s.requirePushApps(ctx, group)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	if recipients, err = bindChatPushApps(recipients, pushApps); err != nil {
		return larkpushcap.SendResult{}, err
	}

	// dispatch 串行遍历推送应用，本 map 只在本次请求内使用，无需加锁。
	uploads := make(map[string]uploadResult, len(pushApps))
	deliver := func(ctx context.Context, sender MessageSender, app larkcfg.PushApp, _ larkpushcap.Recipient, receiveIDType, receiveID string) (string, error) {
		up, done := uploads[app.AppID]
		if !done {
			up = payload.upload(ctx, sender)
			uploads[app.AppID] = up
		}
		if up.err != nil {
			return "", up.err
		}
		if payload.asImage {
			return sender.SendImage(ctx, receiveIDType, receiveID, up.key)
		}
		return sender.SendFile(ctx, receiveIDType, receiveID, up.key)
	}
	result, err := s.dispatch(ctx, group, pushApps, recipients, deliver)
	if err != nil {
		return larkpushcap.SendResult{}, err
	}
	// 文件名可能含业务敏感信息，日志只记形态与大小。
	logger.Infof(ctx, "lark-push: 文件投递 group=%s biz_ref=%s 形态=%s 大小=%d 字节 上传应用数=%d",
		group, req.BizRef, payload.kind(), len(payload.content), len(uploads))
	s.logRequest(ctx, group, req.BizSource, req.BizRef, len(req.Recipients), pushApps, result)
	return result, nil
}

// newFilePayload 校验文件名与内容，并按内容决定以图片还是文件发送。
//
// 文件名只保留最后一段（防止调用方把本地路径带进消息）；内容为空或超过飞书文件上限时
// 返回参数错误。图片判定按内容而非扩展名，且超过图片上限的图片退化为按文件发送。
func newFilePayload(fileName string, content []byte) (filePayload, error) {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return filePayload{}, invalidParam("文件名不能为空")
	}
	if len(content) == 0 {
		return filePayload{}, invalidParam("文件内容不能为空")
	}
	if len(content) > larkim.MaxFileBytes {
		return filePayload{}, invalidParam("文件大小超过上限 " + strconv.Itoa(larkim.MaxFileBytes>>20) + " MB")
	}
	return filePayload{
		name:     name,
		fileType: larkim.FileTypeOf(name),
		content:  content,
		asImage:  larkim.IsUploadableImage(content),
	}, nil
}

// upload 以某推送应用的发送器上传本文件，返回该应用可用的 key 或错误。
func (p filePayload) upload(ctx context.Context, sender MessageSender) uploadResult {
	var (
		key string
		err error
	)
	if p.asImage {
		key, err = sender.UploadImage(ctx, p.content)
	} else {
		key, err = sender.UploadFile(ctx, p.fileType, p.name, p.content)
	}
	if err != nil {
		return uploadResult{err: gerror.Wrap(err, "lark-push: 上传失败")}
	}
	return uploadResult{key: key}
}

// kind 返回日志用的发送形态描述。
func (p filePayload) kind() string {
	if p.asImage {
		return "image"
	}
	return "file:" + p.fileType
}
