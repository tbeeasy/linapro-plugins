// media.go 提供 Bitable 附件的上传：附件经飞书云空间（drive/v1/medias）上传后，
// 以返回的 file_token 写入记录的附件单元格。

package larkbitable

import (
	"context"
	"io"

	"github.com/gogf/gf/v2/errors/gerror"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"

	"linapro-lark-sdk/utils"
)

// UploadMedia 将文件字节作为 Bitable 附件上传到飞书云空间并返回 file_token。文件挂在目标
// Bitable 下（parent_type=bitable_file, parent_node=appToken），以便从记录附件单元格下载。
// size 必须是 r 的精确字节长度。
func (c *Client) UploadMedia(ctx context.Context, appToken, fileName string, size int, r io.Reader) (string, error) {
	req := larkdrive.NewUploadAllMediaReqBuilder().
		Body(larkdrive.NewUploadAllMediaReqBodyBuilder().
			FileName(fileName).
			ParentType(larkdrive.ParentTypeUploadAllMediaBitableFile).
			ParentNode(appToken).
			Size(size).
			File(r).
			Build()).
		Build()
	resp, err := c.client.Drive.V1.Media.UploadAll(ctx, req)
	if err != nil {
		return "", gerror.Wrap(err, "lark: 上传附件请求失败")
	}
	if !resp.Success() {
		return "", gerror.Newf("lark: 上传附件失败 req_id=%s code=%d msg=%s%s", resp.RequestId(), resp.Code, resp.Msg, utils.CodeErrorDetail(resp.CodeError))
	}
	if resp.Data == nil {
		return "", gerror.New("lark: 上传附件未返回数据")
	}
	return larkcore.StringValue(resp.Data.FileToken), nil
}
