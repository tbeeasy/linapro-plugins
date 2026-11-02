// push_status.go 定义对外自检接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// StatusReq 是自检请求，无参数。
type StatusReq struct {
	g.Meta `path:"/status" method:"get" tags:"飞书推送开放接口" summary:"推送能力自检" dc:"部署验收与监控用，不发送任何消息：返回推送能力是否就绪、推送配置是否合法，以及所有分组内各推送应用的凭据齐备性与可寻址员工数。推送能力未就绪时返回 HTTP 503"`
}

// StatusRes 是自检结果。
type StatusRes struct {
	PluginReady  bool                `json:"plugin_ready" dc:"推送能力是否就绪" eg:"true"`
	ConfigValid  bool                `json:"config_valid" dc:"服务端推送配置是否全部合法；任一分组有问题即为 false，但只影响该分组" eg:"true"`
	ConfigIssues []string            `json:"config_issues,omitempty" dc:"配置问题清单，仅有问题时出现" eg:"[]"`
	PushApps     []*PushAppStatusRes `json:"push_apps" dc:"逐「分组 × 推送应用」的自检明细，按分组名、app_id 排序" eg:"[]"`
}

// PushAppStatusRes 是单个推送应用的自检明细。
type PushAppStatusRes struct {
	Group                string `json:"group" dc:"推送分组名" eg:"outer_notify"`
	AppID                string `json:"app_id" dc:"推送应用 App ID（实际发消息的飞书应用）" eg:"cli_a1b2c3d4e5f6"`
	Name                 string `json:"name" dc:"推送应用的配置名" eg:"外网消息通知"`
	IdentityAppID        string `json:"identity_app_id" dc:"投递时取 user_id 所用的身份应用 App ID" eg:"cli_f6e5d4c3b2a1"`
	CredentialReady      bool   `json:"credential_ready" dc:"app_id 与 secret_key 是否齐备" eg:"true"`
	AddressableEmployees int    `json:"addressable_employees" dc:"该企业中可以收到消息的员工数；为 0 说明该企业整体发不出去" eg:"1532"`
	BlankUserIDRows      int    `json:"blank_user_id_rows" dc:"有身份但缺少 user_id 的员工数，通常是身份应用缺少获取 user_id 的权限" eg:"0"`
	Collisions           int    `json:"collisions" dc:"工号对应多个账号、被拒绝投递的员工数" eg:"0"`
	ProbeError           string `json:"probe_error,omitempty" dc:"自检出错原因，仅出错时出现" eg:"员工能力服务未就绪"`
}
