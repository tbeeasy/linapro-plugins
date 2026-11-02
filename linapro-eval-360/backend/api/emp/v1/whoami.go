// Package v1 定义 360 测评员工端 API 的请求/响应 DTO。
// 这些路由由 empcap.EmpAuth 保护，不走宿主 Auth()/Permission() 链，
// 身份仅来自 EmpAuth 解析的员工 JWT。
package v1

import "github.com/gogf/gf/v2/frame/g"

// WhoamiReq 是 GET /api/v1/eval/emp/whoami 的请求。
// 无 permission 标签、无宿主 Auth：身份完全来自 EmpAuth 注入的 EmpContext。
type WhoamiReq struct {
	g.Meta `path:"/whoami" method:"get" tags:"Eval360 Emp" summary:"回显员工身份" dc:"验证 empcap.EmpAuth 已注入员工上下文且无需宿主 Auth。"`
}

// WhoamiRes 回显 EmpAuth 注入 bizctx 的员工身份。
type WhoamiRes struct {
	TenantID int   `json:"tenantId" dc:"来自员工 JWT 的租户 ID" eg:"1"`
	EmpID    int64 `json:"empId"    dc:"内部员工 ID" eg:"1001"`
}
