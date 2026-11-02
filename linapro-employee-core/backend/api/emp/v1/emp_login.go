// emp_login.go 定义 linapro-employee-core 的员工登录 DTO。Login
// 是公开的（飞书 code 换票）；受保护的身份 DTO 位于 emp_me.go。
package v1

import "github.com/gogf/gf/v2/frame/g"

// LoginReq 用飞书登录预授权 code 换取员工 JWT。公开接口：无需会话。
// LarkAppId 选择飞书企业应用凭据，并与解析出的 open_id 一起
// 定位员工（进而定位租户）。
type LoginReq struct {
	g.Meta    `path:"/emp/login" method:"post" tags:"Attendance / Employee" summary:"Employee Lark login" dc:"用飞书登录码换取员工 JWT。公开接口（无需会话）。通过 (larkAppId, open_id) → emp_id 映射解析员工；未登记的身份以 401 拒绝（不自动开通）。租户由解析出的员工推导。"`
	LarkAppId string `json:"larkAppId" v:"required|length:1,128" dc:"飞书企业应用 id（cli_xxx），用于标识飞书企业；选择应用密钥并限定 open_id 查询范围" eg:"cli_a1b2c3d4e5"`
	Code      string `json:"code" v:"required|length:1,256" dc:"来自 tt.login / 网页授权的飞书登录预授权码" eg:"2g1..."`
}

// LoginRes 返回员工 JWT 和最小身份快照。
type LoginRes struct {
	Token          string `json:"token" dc:"员工 JWT（HS256，2 小时，无刷新）；过期后重新登录" eg:"eyJhbGciOiJIUzI1NiI..."`
	EmpId          int64  `json:"empId" dc:"内部员工 id（与身份提供方无关）" eg:"1001"`
	TenantId       int    `json:"tenantId" dc:"租户 id（由解析出的员工推导，与 token 绑定）" eg:"1"`
	OpenId         string `json:"openId" dc:"本次登录换票解析出的飞书 open_id（随所选飞书企业应用而定）" eg:"ou_a1b2c3d4"`
	Name           string `json:"name" dc:"员工显示名称" eg:"Jane Doe"`
	MokaEmployeeNo string `json:"mokaEmployeeNo" dc:"Moka HR 系统工号，未设置时为空" eg:"GZ001198"`
}
