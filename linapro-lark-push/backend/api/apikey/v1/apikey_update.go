// apikey_update.go 定义编辑签名凭据与启停接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// UpdateReq 是编辑签名凭据请求。
type UpdateReq struct {
	g.Meta        `path:"/api-keys/{id}" method:"put" tags:"飞书推送凭据" summary:"编辑签名凭据" dc:"编辑签名凭据的名称、app_key、绑定分组、群聊、状态与备注；secret 不填表示保持原值，填写则替换（调用方须同步更换）。修改在下一次签名请求即生效" permission:"linapro-lark-push:api-key:edit"`
	Id            int64  `json:"id" v:"required|min:1" dc:"凭据 ID" eg:"1"`
	Name          string `json:"name" v:"required|max-length:64" dc:"调用方名称，1~64 个字符" eg:"运维告警平台"`
	AppKey        string `json:"appKey" v:"required|max-length:64" dc:"公开的应用标识，1~64 个字母、数字、_、-、. 字符，在未删除凭据中唯一" eg:"ops-alert-prod"`
	Secret        string `json:"secret" dc:"新的签名 secret，16~128 个可见 ASCII 字符；不填表示保持原值" eg:""`
	Group         string `json:"group" v:"required" dc:"绑定的推送分组，必须是 push_groups 中已配置的分组名" eg:"ops-alert"`
	ChatId        string `json:"chatId" v:"max-length:128" dc:"群聊 chat_id（oc_ 开头），可选；配置后该凭据的每次推送都会同时发到这个群，调用方不能自行指定群聊" eg:"oc_5ad11d72b830411d72b836c20"`
	ChatPushAppId string `json:"chatPushAppId" v:"max-length:64" dc:"发送群聊所用的推送应用 app_id，可选：仅配置了 chatId 时有效；分组只有一个推送应用时可不填，有多个时必填且须属于绑定分组" eg:"cli_a1b2c3d4e5f6"`
	Status        int    `json:"status" v:"in:0,1" dc:"状态（字典 sys_normal_disable）：1=正常 0=停用" eg:"1"`
	Remark        string `json:"remark" v:"max-length:500" dc:"备注，最多 500 个字符" eg:"生产环境告警"`
}

// UpdateRes 是编辑签名凭据响应。
type UpdateRes struct{}

// UpdateStatusReq 是启停签名凭据请求。
type UpdateStatusReq struct {
	g.Meta `path:"/api-keys/{id}/status" method:"put" tags:"飞书推送凭据" summary:"启停签名凭据" dc:"启用或停用签名凭据；停用后以该 app_key 签名的请求从下一次起即被拒绝（HTTP 200，errorCode 为 LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY）" permission:"linapro-lark-push:api-key:edit"`
	Id     int64 `json:"id" v:"required|min:1" dc:"凭据 ID" eg:"1"`
	Status int   `json:"status" v:"in:0,1" dc:"目标状态（字典 sys_normal_disable）：1=正常 0=停用" eg:"0"`
}

// UpdateStatusRes 是启停签名凭据响应。
type UpdateStatusRes struct{}
