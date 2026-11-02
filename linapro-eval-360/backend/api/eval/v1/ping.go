// Package v1 定义 360 测评管理端 API 的请求/响应 DTO。
package v1

import "github.com/gogf/gf/v2/frame/g"

// PingReq 是 GET /api/v1/eval/admin/ping 的请求。
// 携带 permission 标签，验证宿主 Permission() 中间件已挂载并生效。
type PingReq struct {
	g.Meta `path:"/ping" method:"get" tags:"Eval360 Admin" summary:"管理端健康探针" dc:"验证管理端路由组已注册且 Auth/Permission 链就绪。" permission:"eval:360:list"`
}

// PingRes 回显插件标识与就绪状态。
type PingRes struct {
	Plugin string `json:"plugin" dc:"插件 ID" eg:"linapro-eval-360"`
	Ready  bool   `json:"ready"  dc:"骨架是否就绪" eg:"true"`
}
