// push_text.go 定义对外文本推送接口的请求 / 响应结构，以及文本、文件接口共用的收件人与投递结果结构。
//
// 对外接口面向外部系统（非宿主用户会话），由 HMAC 签名鉴权；响应体由宿主统一响应中间件包装为
// code / message / data / errorCode，下面的 *Res 结构即 data 部分（FB-12）。

package v1

import "github.com/gogf/gf/v2/frame/g"

// SendTextReq 是文本推送请求。请求体只允许下列字段，出现未知字段（如 group、chats）按参数错误拒绝；
// 推送分组与群聊由签名凭据的后台配置决定，调用方不能指定。
type SendTextReq struct {
	g.Meta     `path:"/message/text" method:"post" tags:"飞书推送开放接口" summary:"文本推送" dc:"按工号向员工推送飞书纯文本消息：员工在几家飞书企业有身份就各发一条；签名凭据配置了群聊时同时发到该群。请求体上限 256 KB，超限返回 HTTP 413；部分收件人失败时 code 非 0、data.failed 列出失败项"`
	Recipients []RecipientReq `json:"recipients" dc:"个人收件人列表；签名凭据未配置群聊时不能为空，配置了群聊时可为空（只发群）。同一收件人只发一次" eg:"[{\"employee_no\":\"GZ001198\"}]"`
	Text       string         `json:"text" dc:"消息正文，纯文本，支持换行；不能为空，最长 2000 字符，超长直接报参数错误、不截断" eg:"【预警】产线 3 号机组温度超过阈值"`
	BizRef     string         `json:"biz_ref" dc:"业务单号，仅用于双方日志对账，不具备幂等或去重作用；不传不记录" eg:"ALERT-20261028-0012"`
}

// SendTextRes 是文本推送结果。
type SendTextRes struct {
	SendResult
}

// RecipientReq 是一个个人收件人。employee_no 与 user_id 二选一；同时给出时以工号为准。
type RecipientReq struct {
	EmployeeNo string `json:"employee_no" dc:"员工工号，系统据此自动扇出到该员工有身份的各个飞书企业；与 user_id 二选一" eg:"GZ001198"`
	UserID     string `json:"user_id" dc:"飞书企业级 user_id，调用方已持有该标识时使用；与 employee_no 二选一" eg:"5d9bdxxx"`
}

// SendResult 是文本 / 文件推送共用的投递结果。全部成功时 failed 省略；
// 存在失败时响应 code 非 0（errorCode 为 LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED），failed 列出失败项。
type SendResult struct {
	AllSucceeded bool         `json:"all_succeeded" dc:"是否全部投递成功；只在部分企业没有身份不算失败" eg:"true"`
	Failed       []*FailedRes `json:"failed,omitempty" dc:"失败项，全部成功时省略；调用方据此只重试失败的收件人" eg:"[]"`
}

// FailedRes 是一条投递失败，只带调用方定位收件人与向飞书查询所需的信息（FB-11）；
// 推送应用、message_id、错误详情等只进服务端日志。
type FailedRes struct {
	EmployeeNo string `json:"employee_no,omitempty" dc:"失败收件人的工号（按工号寻址时给出）" eg:"GZ001200"`
	UserID     string `json:"user_id,omitempty" dc:"失败收件人的 user_id（按 user_id 寻址时给出）" eg:"5d9bdxxx"`
	ChatID     string `json:"chat_id,omitempty" dc:"失败的群聊 chat_id（签名凭据配置的群聊投递失败时给出）" eg:"oc_5ad11d72b830411d72b836c20"`
	LarkCode   int    `json:"lark_code" dc:"飞书错误码；非飞书侧原因（如工号无身份）为 0" eg:"230013"`
	LarkMsg    string `json:"lark_msg" dc:"飞书错误信息；非飞书侧原因时为失败状态：no_identity=所有企业都没有身份（工号写错或不存在也是此状态），user_id_missing=身份缺少 user_id，collision=工号对应多个账号，failed=网络等本地原因" eg:"Bot has NO availability to this user."`
	LarkReqID  string `json:"lark_req_id" dc:"飞书请求 ID，向飞书反馈问题时提供；非飞书侧原因时为空" eg:"20261028101530C8A3E0F1D2B7A9C4E5F6"`
}
