// file.go 提供文件与图片的上传（im/v1/files、im/v1/images）以及文件 / 图片消息
// （msg_type=file / image）的发送。
//
// 飞书的 file_key / image_key 归属上传它的应用，换一个应用凭据就无法引用，因此调用方
// 必须用「即将发送消息的那个应用」上传，再用返回的 key 发送。上传与发送都复用 larkhttp
// 的 429 退避，失败统一返回 FeishuError 以便调用方记录 code / req_id。

package larkim

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"linapro-lark-sdk/utils"
)

// 上传上限常量，取自飞书开放平台文档。
const (
	// MaxFileBytes 是 im/v1/files 单个文件的大小上限（30 MB）。
	MaxFileBytes = 30 << 20
	// MaxImageBytes 是 im/v1/images 单张图片的大小上限（10 MB）。
	MaxImageBytes = 10 << 20
)

// 消息类型常量。
const (
	// msgTypeFile 表示文件消息类型。
	msgTypeFile = "file"
	// msgTypeImage 表示图片消息类型。
	msgTypeImage = "image"
	// imageTypeMessage 是上传图片时的用途：用于发送消息。
	imageTypeMessage = "message"
)

// 飞书 file_type 取值。未列出的扩展名统一按 FileTypeStream（普通二进制文件）上传。
const (
	FileTypeOpus   = "opus"
	FileTypeMP4    = "mp4"
	FileTypePDF    = "pdf"
	FileTypeDoc    = "doc"
	FileTypeXls    = "xls"
	FileTypePpt    = "ppt"
	FileTypeStream = "stream"
)

// fileTypeByExt 是扩展名到飞书 file_type 的映射（扩展名小写、含点）。
var fileTypeByExt = map[string]string{
	".opus": FileTypeOpus,
	".mp4":  FileTypeMP4,
	".pdf":  FileTypePDF,
	".doc":  FileTypeDoc,
	".docx": FileTypeDoc,
	".xls":  FileTypeXls,
	".xlsx": FileTypeXls,
	".ppt":  FileTypePpt,
	".pptx": FileTypePpt,
}

// uploadableImageMIME 是飞书图片上传支持、且能被 http.DetectContentType 识别的格式。
// TIFF / HEIC 无法按内容识别，按普通文件发送。
var uploadableImageMIME = map[string]struct{}{
	"image/jpeg":               {},
	"image/png":                {},
	"image/gif":                {},
	"image/webp":               {},
	"image/bmp":                {},
	"image/x-icon":             {},
	"image/vnd.microsoft.icon": {},
}

// fileCreator 是飞书「上传文件」接口的最小接缝：官方 SDK 的 Im.V1.File 结构化满足它。
type fileCreator interface {
	Create(ctx context.Context, req *larkimv1.CreateFileReq, options ...larkcore.RequestOptionFunc) (*larkimv1.CreateFileResp, error)
}

// imageCreator 是飞书「上传图片」接口的最小接缝：官方 SDK 的 Im.V1.Image 结构化满足它。
type imageCreator interface {
	Create(ctx context.Context, req *larkimv1.CreateImageReq, options ...larkcore.RequestOptionFunc) (*larkimv1.CreateImageResp, error)
}

// FileTypeOf 按文件名扩展名推导飞书 file_type；无法识别时返回 FileTypeStream。
func FileTypeOf(fileName string) string {
	if t, ok := fileTypeByExt[strings.ToLower(filepath.Ext(fileName))]; ok {
		return t
	}
	return FileTypeStream
}

// IsUploadableImage 按文件内容（而非扩展名）判断能否作为图片上传：格式须为飞书支持且可识别的
// 图片，大小不超过 MaxImageBytes。按内容判断是为了避免扩展名与实际格式不符时飞书拒收。
func IsUploadableImage(content []byte) bool {
	if len(content) == 0 || len(content) > MaxImageBytes {
		return false
	}
	_, ok := uploadableImageMIME[http.DetectContentType(content)]
	return ok
}

// UploadFile 以当前应用凭据上传一个文件，返回 file_key（仅本应用可用）。
//
// fileType 取 FileType* 常量（可用 FileTypeOf 推导）；fileName 为接收方看到的文件名。
// 内容为空或超过 MaxFileBytes 时直接返回参数错误、不发起请求。
func (c *Client) UploadFile(ctx context.Context, fileType, fileName string, content []byte) (string, error) {
	if strings.TrimSpace(fileName) == "" {
		return "", gerror.New("larkim: 文件名不能为空")
	}
	if len(content) == 0 || len(content) > MaxFileBytes {
		return "", gerror.Newf("larkim: 文件大小须在 1 字节到 %d 字节之间，实际 %d 字节", MaxFileBytes, len(content))
	}
	req := larkimv1.NewCreateFileReqBuilder().
		Body(larkimv1.NewCreateFileReqBodyBuilder().
			FileType(fileType).
			FileName(fileName).
			File(bytes.NewReader(content)).
			Build()).
		Build()
	resp, err := c.filesOrDefault().Create(ctx, req)
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 上传文件请求失败")
	}
	if !resp.Success() {
		return "", newUploadError(msgKindFile, resp.Code, resp.Msg, resp.RequestId(), utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil || larkcore.StringValue(resp.Data.FileKey) == "" {
		return "", gerror.New("larkim: 上传文件未返回 file_key")
	}
	return larkcore.StringValue(resp.Data.FileKey), nil
}

// UploadImage 以当前应用凭据上传一张用于发送消息的图片，返回 image_key（仅本应用可用）。
//
// 内容为空或超过 MaxImageBytes 时直接返回参数错误、不发起请求。
func (c *Client) UploadImage(ctx context.Context, content []byte) (string, error) {
	if len(content) == 0 || len(content) > MaxImageBytes {
		return "", gerror.Newf("larkim: 图片大小须在 1 字节到 %d 字节之间，实际 %d 字节", MaxImageBytes, len(content))
	}
	req := larkimv1.NewCreateImageReqBuilder().
		Body(larkimv1.NewCreateImageReqBodyBuilder().
			ImageType(imageTypeMessage).
			Image(bytes.NewReader(content)).
			Build()).
		Build()
	resp, err := c.imagesOrDefault().Create(ctx, req)
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 上传图片请求失败")
	}
	if !resp.Success() {
		return "", newUploadError(msgKindImage, resp.Code, resp.Msg, resp.RequestId(), utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil || larkcore.StringValue(resp.Data.ImageKey) == "" {
		return "", gerror.New("larkim: 上传图片未返回 image_key")
	}
	return larkcore.StringValue(resp.Data.ImageKey), nil
}

// SendFile 用 file_key 向指定接收者发送一条文件消息，成功返回 message_id。
// file_key 必须由同一应用的 UploadFile 产生。
func (c *Client) SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) (string, error) {
	if strings.TrimSpace(fileKey) == "" {
		return "", gerror.New("larkim: file_key 不能为空")
	}
	content, err := json.Marshal(map[string]string{"file_key": fileKey})
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 编码文件消息内容失败")
	}
	return c.createMessage(ctx, receiveIDType, receiveID, msgTypeFile, string(content), msgKindFile)
}

// SendImage 用 image_key 向指定接收者发送一条图片消息，成功返回 message_id。
// image_key 必须由同一应用的 UploadImage 产生。
func (c *Client) SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) (string, error) {
	if strings.TrimSpace(imageKey) == "" {
		return "", gerror.New("larkim: image_key 不能为空")
	}
	content, err := json.Marshal(map[string]string{"image_key": imageKey})
	if err != nil {
		return "", gerror.Wrap(err, "larkim: 编码图片消息内容失败")
	}
	return c.createMessage(ctx, receiveIDType, receiveID, msgTypeImage, string(content), msgKindImage)
}

// filesOrDefault 返回上传文件接缝；未显式注入时回退到官方 SDK 实现。
func (c *Client) filesOrDefault() fileCreator {
	if c.files != nil {
		return c.files
	}
	return c.client.Im.V1.File
}

// imagesOrDefault 返回上传图片接缝；未显式注入时回退到官方 SDK 实现。
func (c *Client) imagesOrDefault() imageCreator {
	if c.images != nil {
		return c.images
	}
	return c.client.Im.V1.Image
}

// newUploadError 构造上传失败的结构化错误；种类后缀「（上传）」便于与发送失败区分，
// 错误文本形如「larkim: 发送文件（上传）失败 req_id=... code=...」。
func newUploadError(kind string, code int, msg, reqID, detail string) *FeishuError {
	return newFeishuError(kind+"（上传）", code, msg, reqID, detail)
}
