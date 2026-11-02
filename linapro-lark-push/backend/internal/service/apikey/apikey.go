// Package apikey 管理对外推送接口的 HMAC 签名凭据（FB-5）。
//
// 凭据存放在本插件数据表 plugin_linapro_lark_push_api_key，由后台管理页维护：每条凭据把一个
// 公开的 app_key、一个签名 secret 绑定到 push_groups 中的一个推送分组，并可配置一个群聊
// chat_id（FB-10）：该凭据的每次推送都会发到这个群，调用方不能自行传 chat_id。签名中间件每次请求
// 经 FindEnabledByAppKey 直接查库（不缓存），因此停用、删除与修改在下一次请求即生效。
//
// 边界：
//   - secret 必须以明文存储（HMAC 验签需要原值）。列表与详情接口不返回 secret，查看走
//     GetSecret，由独立权限点保护；
//   - 凭据属于平台级配置（插件 platform_only），不归属组织或个人，不接入组织数据权限过滤，
//     访问边界由菜单 / 按钮权限点决定；
//   - 删除为软删除，app_key 唯一约束只覆盖未删除行，删除后同名 app_key 可以重建。
package apikey

import (
	"context"

	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// Status 是凭据状态，取值与宿主字典 sys_normal_disable 一致。
type Status int

// 凭据状态常量。
const (
	// StatusDisabled 表示停用：签名请求按「app_key 未知或未启用」拒绝。
	StatusDisabled Status = 0
	// StatusNormal 表示正常：可用于签名鉴权。
	StatusNormal Status = 1
)

// ListInput 是凭据分页列表的查询条件。
type ListInput struct {
	// Keyword 按名称或 app_key 模糊匹配；为空不过滤。
	Keyword string
	// Group 按绑定分组精确过滤；为空不过滤。
	Group string
	// Status 按状态过滤；nil 不过滤。
	Status *Status
	// PageNum 是页码，小于 1 时按 1 处理。
	PageNum int
	// PageSize 是每页条数，小于 1 时按 10 处理，最大 100。
	PageSize int
}

// Item 是凭据的公开投影，不含 secret。
type Item struct {
	// Id 是凭据 ID。
	Id int64
	// Name 是调用方名称。
	Name string
	// AppKey 是公开的应用标识。
	AppKey string
	// Group 是绑定的推送分组。
	Group string
	// ChatID 是配置的群聊 chat_id；为空表示不推群聊。
	ChatID string
	// ChatPushAppID 是发送群聊所用的推送应用；为空表示用分组唯一的推送应用。
	ChatPushAppID string
	// Status 是凭据状态。
	Status Status
	// Remark 是备注。
	Remark string
	// CreatedAt 是创建时间（Unix 毫秒）。
	CreatedAt *int64
	// UpdatedAt 是更新时间（Unix 毫秒）。
	UpdatedAt *int64
}

// ListOutput 是凭据分页列表结果。
type ListOutput struct {
	// List 是当前页凭据。
	List []*Item
	// Total 是符合条件的总条数。
	Total int
}

// SaveInput 是新增 / 编辑凭据的输入。
type SaveInput struct {
	// Name 是调用方名称，必填，1~64 个字符。
	Name string
	// AppKey 是公开的应用标识，必填，1~64 个字符，仅允许字母、数字、_、-、.。
	AppKey string
	// Secret 是签名密钥，16~128 个可见 ASCII 字符。新增时必填；编辑时为空表示保持原值。
	Secret string
	// Group 是绑定的推送分组，必须是 push_groups 中已配置的分组名。
	Group string
	// ChatID 是群聊 chat_id（oc_ 开头），可选；配置后该凭据的每次推送都会发到这个群。
	ChatID string
	// ChatPushAppID 是发送群聊所用的推送应用 app_id，可选：仅在配置了 ChatID 时有意义；分组只有一个
	// 推送应用时可留空，有多个时必填，且必须属于 Group。
	ChatPushAppID string
	// Status 是凭据状态，取 StatusNormal 或 StatusDisabled。
	Status Status
	// Remark 是备注，最多 500 个字符。
	Remark string
}

// CreateOutput 是新增凭据的结果。
type CreateOutput struct {
	// Id 是新凭据 ID。
	Id int64
	// Secret 是保存的签名密钥，供管理页当场展示。
	Secret string
}

// Credential 是签名中间件所需的凭据视图（含 secret，仅供鉴权路径使用）。
type Credential struct {
	// Id 是凭据 ID，用于日志中的 key_id（不是密钥内容）。
	Id int64
	// AppKey 是公开的应用标识。
	AppKey string
	// Secret 是签名密钥，只参与 HMAC 计算，绝不进日志。
	Secret string
	// Group 是绑定的推送分组。
	Group string
	// ChatID 是配置的群聊 chat_id；为空表示不推群聊。
	ChatID string
	// ChatPushAppID 是发送群聊所用的推送应用；为空表示用分组唯一的推送应用。
	ChatPushAppID string
}

// GroupOption 是推送分组下拉项，带分组内的推送应用，供管理页选择群聊发送应用。
type GroupOption struct {
	// Name 是分组名。
	Name string
	// PushApps 是分组内的推送应用（按配置顺序）。
	PushApps []PushAppOption
}

// PushAppOption 是推送应用的公开投影（不含密钥）。
type PushAppOption struct {
	// AppID 是飞书应用 App ID。
	AppID string
	// Name 是推送应用显示名。
	Name string
}

// Service 是签名凭据管理与查询服务。
type Service interface {
	// List 分页查询凭据（不含 secret），按更新时间倒序。数据库失败返回 CodeApiKeyQueryFailed。
	List(ctx context.Context, in ListInput) (*ListOutput, error)
	// Get 返回单条凭据（不含 secret）；不存在或已删除返回 CodeApiKeyNotFound。
	Get(ctx context.Context, id int64) (*Item, error)
	// Create 新增凭据并返回保存的 secret。参数非法（含 secret 为空、chat_id 格式、群聊推送应用不属于分组或
	// 分组有多个推送应用却未指定）返回 CodeApiKeyInvalidParam，app_key 与未删除凭据重复返回
	// CodeApiKeyAppKeyExists，分组不在 push_groups 中返回 CodeApiKeyGroupNotFound。
	Create(ctx context.Context, in SaveInput) (*CreateOutput, error)
	// Update 编辑凭据；in.Secret 为空时保持原 secret（不报错）。其余错误语义同 Create，另有 CodeApiKeyNotFound。
	Update(ctx context.Context, id int64, in SaveInput) error
	// SetStatus 启用或停用凭据，下一次签名请求即生效。不存在返回 CodeApiKeyNotFound。
	SetStatus(ctx context.Context, id int64, status Status) error
	// Delete 软删除凭据，删除后同名 app_key 可以重建。不存在返回 CodeApiKeyNotFound。
	Delete(ctx context.Context, id int64) error
	// GetSecret 返回凭据的 secret 明文，供有「查看密钥」权限的管理员反复查看。
	// 不存在返回 CodeApiKeyNotFound。
	GetSecret(ctx context.Context, id int64) (string, error)
	// ListGroups 返回 push_groups 中已配置的分组（按名升序）及各自的推送应用，供管理页下拉选择。
	// 宿主配置能力缺失或配置结构非法时返回错误。
	ListGroups(ctx context.Context) ([]GroupOption, error)
	// FindEnabledByAppKey 按 app_key 查询启用且未删除的凭据，供签名中间件每次请求调用。
	// 未命中（不存在、已停用或已删除）返回 (nil, nil)；数据库失败返回错误，由调用方失败关闭。
	FindEnabledByAppKey(ctx context.Context, appKey string) (*Credential, error)
}

// 编译期断言：serviceImpl 满足 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现。
type serviceImpl struct {
	// hostConfig 用于读取 push_groups 分组名（校验与下拉）；为 nil 时分组相关操作返回错误。
	hostConfig hostconfigcap.Service
}

// New 构造签名凭据服务。hostConfig 由插件路由注册回调从宿主服务目录取得并传入。
func New(hostConfig hostconfigcap.Service) Service {
	return &serviceImpl{hostConfig: hostConfig}
}
