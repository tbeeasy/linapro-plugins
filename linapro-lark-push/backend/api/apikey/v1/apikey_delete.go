// apikey_delete.go 定义删除签名凭据接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// DeleteReq 是删除签名凭据请求。
type DeleteReq struct {
	g.Meta `path:"/api-keys/{id}" method:"delete" tags:"飞书推送凭据" summary:"删除签名凭据" dc:"软删除签名凭据：删除后以该 app_key 签名的请求被拒绝（HTTP 200，errorCode 为 LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY），同名 app_key 可以重新创建" permission:"linapro-lark-push:api-key:remove"`
	Id     int64 `json:"id" v:"required|min:1" dc:"凭据 ID" eg:"1"`
}

// DeleteRes 是删除签名凭据响应。
type DeleteRes struct{}
