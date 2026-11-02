// apikey_list.go 定义签名凭据分页列表与推送分组下拉接口的请求 / 响应结构。

package v1

import "github.com/gogf/gf/v2/frame/g"

// ListReq 是签名凭据分页列表请求。
type ListReq struct {
	g.Meta   `path:"/api-keys" method:"get" tags:"飞书推送凭据" summary:"签名凭据列表" dc:"分页查询对外推送接口的 HMAC 签名凭据，按更新时间倒序；响应不含 secret，查看 secret 需调用独立接口" permission:"linapro-lark-push:api-key:list"`
	PageNum  int    `json:"pageNum" dc:"页码，不传默认第 1 页" eg:"1"`
	PageSize int    `json:"pageSize" dc:"每页条数，不传默认 10，最大 100" eg:"10"`
	Keyword  string `json:"keyword" dc:"按名称或 app_key 模糊匹配，不传不过滤" eg:"ops"`
	Group    string `json:"group" dc:"按绑定的推送分组精确过滤，不传不过滤" eg:"ops-alert"`
	Status   *int   `json:"status" dc:"按状态过滤（字典 sys_normal_disable）：1=正常 0=停用；不传不过滤" eg:"1"`
}

// ListRes 是签名凭据分页列表响应。
type ListRes struct {
	List  []*Item `json:"list" dc:"当前页凭据" eg:"[]"`
	Total int     `json:"total" dc:"符合条件的总条数" eg:"1"`
}

// Item 是一条签名凭据（不含 secret）。
type Item struct {
	Id            int64  `json:"id" dc:"凭据 ID" eg:"1"`
	Name          string `json:"name" dc:"调用方名称" eg:"运维告警平台"`
	AppKey        string `json:"appKey" dc:"公开的应用标识，调用方放在 X-App-Key 请求头" eg:"ops-alert-prod"`
	Group         string `json:"group" dc:"绑定的推送分组（push_groups 中的分组名），投递只用该分组的推送应用" eg:"ops-alert"`
	ChatId        string `json:"chatId" dc:"配置的群聊 chat_id；为空表示不推群聊" eg:"oc_5ad11d72b830411d72b836c20"`
	ChatPushAppId string `json:"chatPushAppId" dc:"发送群聊所用的推送应用 app_id；为空表示用分组唯一的推送应用" eg:"cli_a1b2c3d4e5f6"`
	Status        int    `json:"status" dc:"状态（字典 sys_normal_disable）：1=正常 0=停用" eg:"1"`
	Remark        string `json:"remark" dc:"备注" eg:"生产环境告警"`
	CreatedAt     *int64 `json:"createdAt" dc:"创建时间，Unix timestamp in milliseconds" eg:"1777334400000"`
	UpdatedAt     *int64 `json:"updatedAt" dc:"更新时间，Unix timestamp in milliseconds" eg:"1777334400000"`
}

// ListGroupsReq 是推送分组下拉请求。
type ListGroupsReq struct {
	g.Meta `path:"/push-groups" method:"get" tags:"飞书推送凭据" summary:"推送分组列表" dc:"返回配置 plugin.linapro-lark-push.push_groups 中已配置的分组（按名升序）及各分组的推送应用（不含密钥），供新增 / 编辑凭据时选择绑定分组与群聊推送应用" permission:"linapro-lark-push:api-key:list"`
}

// ListGroupsRes 是推送分组下拉响应。
type ListGroupsRes struct {
	List []*GroupItem `json:"list" dc:"推送分组列表，按名升序" eg:"[]"`
}

// GroupItem 是一个推送分组。
type GroupItem struct {
	Name     string         `json:"name" dc:"分组名" eg:"ops-alert"`
	PushApps []*PushAppItem `json:"pushApps" dc:"分组内的推送应用（按配置顺序）" eg:"[]"`
}

// PushAppItem 是一个推送应用（不含密钥）。
type PushAppItem struct {
	AppId string `json:"appId" dc:"飞书应用 App ID" eg:"cli_a1b2c3d4e5f6"`
	Name  string `json:"name" dc:"推送应用显示名" eg:"系统通知(霍尔果斯)"`
}
