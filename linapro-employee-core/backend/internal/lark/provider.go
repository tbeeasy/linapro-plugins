// Package lark 把所有飞书/Lark SDK 细节隔离在一个小的 provider 接口之后。
// 调用方（登录控制器）接收纯 Go 结构体，永远看不到 larksuite 类型，
// 因此未来替换身份提供方（如钉钉）只需为这个接口新增实现，
// 而无需改动波及业务代码。
package lark

import "context"

// LoginIdentity 是从飞书登录 code 解析出的原始身份。
// 部门来自第二次通讯录 API 查询（authen 响应不携带部门），
// 并要求飞书应用具备通讯录读取权限。
type LoginIdentity struct {
	OpenID        string
	UserID        string // 飞书 user_id，用于查询部门
	Name          string
	DepartmentIDs []string
}

// Provider 用登录 code 换取身份并解析部门。实现不得让 SDK 类型
// 越过此边界。凭据作为参数传入（由调用方加载一次），
// 使调用方也能拿到自动开通标志而无需二次读取。
type Provider interface {
	// ExchangeCode 使用给定租户的飞书应用凭据，把飞书登录预授权 code
	// 转换为身份。当应用具备所需通讯录权限时解析部门；权限失败时返回
	// DepartmentIDs 为空的身份，而不是让整个登录失败。
	ExchangeCode(ctx context.Context, cred Credential, code string) (LoginIdentity, error)
}
