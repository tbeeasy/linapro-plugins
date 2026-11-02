// status.go 定义推送能力自检报告（Sender.Status 的返回值）的公开类型。

package larkpushcap

// StatusReport 是推送能力的自检报告，供部署验收与告警使用。
//
// 设计意图：飞书侧最常见的两类静默故障是「推送应用凭据缺失」与「身份应用缺少
// 获取用户 user_id 的权限」。前者让投递根本发不出去，后者让 lark_user_id 全表为空、
// 表现为「所有人都没有身份」——两者都不会让进程报错，只能在部署时主动查。
type StatusReport struct {
	// PluginReady 报告拥有方是否已绑定实现。
	PluginReady bool
	// ConfigValid 报告 push_groups 配置是否全部通过校验（任一分组有问题即为 false；
	// 但单个分组非法只影响该分组的投递）。
	ConfigValid bool
	// ConfigIssues 是配置问题清单（Field + Message 的人类可读串，Field 带分组路径）。
	ConfigIssues []string
	// PushApps 是逐「分组 × 推送应用」的自检明细，按分组名、App ID 排序。
	PushApps []PushAppStatus
}

// PushAppStatus 是某个分组内单个推送应用的自检明细。同一 App ID 出现在多个分组时
// 每个分组各一条。
type PushAppStatus struct {
	// Group 是该推送应用所属的分组名。
	Group string
	// AppID 是推送应用 App ID。
	AppID string
	// Name 是配置里的展示名。
	Name string
	// IdentityAppID 是该推送应用声明的身份应用。
	IdentityAppID string
	// CredentialReady 报告 app_id 与 secret_key 是否齐备。
	CredentialReady bool
	// AddressableEmployees 是该企业作用域下可投递（user_id 非空且无碰撞）的员工数。
	//
	// identity_app_id 指向的是 linapro-employee-core 配置里的身份应用，本插件读不到
	// 那份配置、无法在配置期校验其存在性——填错时的唯一表现就是这里为 0
	// （且 BlankUserIDRows 通常也为 0）。部署验收应把它当作该应用是否可用的判据。
	AddressableEmployees int
	// BlankUserIDEmployees 是该企业作用域下有身份但 lark_user_id 为空的员工数。
	// AddressableEmployees=0 且 BlankUserIDEmployees>0 即身份应用缺权限。
	BlankUserIDEmployees int
	// Collisions 是该企业作用域下「同一工号对应多个飞书账号」的员工数。
	Collisions int
	// ProbeError 是取该企业寻址索引时的错误（如员工能力未就绪）。
	ProbeError string
}
