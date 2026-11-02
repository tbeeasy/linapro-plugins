// push_file.go 定义对外文件推送接口的请求 / 响应结构与表单字段名。

package v1

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// 文件接口（multipart/form-data）的表单字段名。除这三个字段外出现任何字段都按参数错误拒绝，
// 与文本接口拒绝未知字段的口径一致。
const (
	// FileFormFile 是文件字段（必填，仅一个文件）。
	FileFormFile = "file"
	// FileFormRecipients 是个人收件人 JSON 数组字符串，元素同 RecipientReq。
	FileFormRecipients = "recipients"
	// FileFormBizRef 是可选业务单号。
	FileFormBizRef = "biz_ref"
)

// SendFileReq 是文件推送请求（multipart/form-data，一步式「上传即推送」）。字段在此声明以生成接口文档；
// 控制器另按原始表单严格校验（未知字段、重复字段、文件个数与大小）。
type SendFileReq struct {
	g.Meta     `path:"/message/file" method:"post" mime:"multipart/form-data" tags:"飞书推送开放接口" summary:"文件推送" dc:"上传一个文件并推送给员工：识别为图片且不超过 10 MB 时以图片消息发送，否则以文件消息发送（上限 30 MB）；收件人与群聊规则同文本推送。请求体上限 31 MB，超限返回 HTTP 413；文件只在内存中中转，不落盘"`
	File       *ghttp.UploadFile `json:"file" type:"file" dc:"要推送的文件，有且只有一个，最大 30 MB；接收方看到的文件名即上传文件名" eg:"2026年10月产线月报.pdf"`
	Recipients string            `json:"recipients" dc:"个人收件人，JSON 数组字符串，元素同文本推送的 recipients；签名凭据未配置群聊时不能为空" eg:"[{\"employee_no\":\"GZ001198\"}]"`
	BizRef     string            `json:"biz_ref" dc:"业务单号，仅用于日志对账；不传不记录" eg:"REPORT-202610"`
}

// SendFileRes 是文件推送结果，结构同文本推送。
type SendFileRes struct {
	SendResult
}
