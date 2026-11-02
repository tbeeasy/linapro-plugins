// apikey_get.go 定义签名凭据详情与查看 secret 接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// GetReq 是签名凭据详情请求。
type GetReq struct {
	g.Meta `path:"/api-keys/{id}" method:"get" tags:"飞书推送凭据" summary:"签名凭据详情" dc:"查询单条签名凭据用于编辑表单回填，响应不含 secret；凭据不存在或已删除时返回错误" permission:"linapro-lark-push:api-key:list"`
	Id     int64 `json:"id" v:"required|min:1" dc:"凭据 ID" eg:"1"`
}

// GetRes 是签名凭据详情响应。
type GetRes struct {
	*Item
}

// GetSecretReq 是查看 secret 请求。
type GetSecretReq struct {
	g.Meta `path:"/api-keys/{id}/secret" method:"get" tags:"飞书推送凭据" summary:"查看签名 secret" dc:"返回签名凭据的 secret 明文，供管理员核对或转交调用方；需要独立的「查看密钥」权限，可反复查看" permission:"linapro-lark-push:api-key:secret"`
	Id     int64 `json:"id" v:"required|min:1" dc:"凭据 ID" eg:"1"`
}

// GetSecretRes 是查看 secret 响应。
type GetSecretRes struct {
	Secret string `json:"secret" dc:"签名 secret 明文，调用方只用于本地计算 HMAC-SHA256 签名，不得在网络上传输" eg:"9f2c4e6a8b0d1f3e5a7c9e1b3d5f7a9c"`
}
