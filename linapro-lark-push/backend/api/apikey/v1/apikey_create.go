// apikey_create.go 定义新增签名凭据接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// CreateReq 是新增签名凭据请求。
type CreateReq struct {
	g.Meta        `path:"/api-keys" method:"post" tags:"飞书推送凭据" summary:"新增签名凭据" dc:"新增一条对外推送接口的签名凭据并绑定推送分组，可选配置一个群聊；secret 必填（管理页打开新增表单时已在浏览器预填随机值），服务端不代为生成。app_key 在未删除凭据中唯一，分组必须已在 push_groups 中配置" permission:"linapro-lark-push:api-key:add"`
	Name          string `json:"name" v:"required|max-length:64" dc:"调用方名称，1~64 个字符" eg:"运维告警平台"`
	AppKey        string `json:"appKey" v:"required|max-length:64" dc:"公开的应用标识，1~64 个字母、数字、_、-、. 字符，在未删除凭据中唯一" eg:"ops-alert-prod"`
	Secret        string `json:"secret" v:"required|length:16,128" dc:"签名 secret，必填，16~128 个可见 ASCII 字符" eg:"9f2c4e6a8b0d1f3e5a7c9e1b3d5f7a9c"`
	Group         string `json:"group" v:"required" dc:"绑定的推送分组，必须是 push_groups 中已配置的分组名" eg:"ops-alert"`
	ChatId        string `json:"chatId" v:"max-length:128" dc:"群聊 chat_id（oc_ 开头），可选；配置后该凭据的每次推送都会同时发到这个群，调用方不能自行指定群聊" eg:"oc_5ad11d72b830411d72b836c20"`
	ChatPushAppId string `json:"chatPushAppId" v:"max-length:64" dc:"发送群聊所用的推送应用 app_id，可选：仅配置了 chatId 时有效；分组只有一个推送应用时可不填，有多个时必填且须属于绑定分组" eg:"cli_a1b2c3d4e5f6"`
	Status        int    `json:"status" v:"in:0,1" dc:"状态（字典 sys_normal_disable）：1=正常 0=停用；不传按 0 处理，管理页默认提交 1" eg:"1"`
	Remark        string `json:"remark" v:"max-length:500" dc:"备注，最多 500 个字符" eg:"生产环境告警"`
}

// CreateRes 是新增签名凭据响应。
type CreateRes struct {
	Id     int64  `json:"id" dc:"新凭据 ID" eg:"1"`
	Secret string `json:"secret" dc:"保存的 secret，供管理页当场展示" eg:"9f2c4e6a8b0d1f3e5a7c9e1b3d5f7a9c"`
}
