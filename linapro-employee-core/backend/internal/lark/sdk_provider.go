// sdk_provider.go 是 Provider 的生产飞书实现，由
// larksuite/oapi-sdk-go/v3 支撑：它通过 authen API 用登录 code 换身份，
// 并通过 contact API 解析部门。按调用构建每租户客户端，
// 使凭据修改无需重启即可生效。

package lark

import (
	"context"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	authen "github.com/larksuite/oapi-sdk-go/v3/service/authen/v1"
	contact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"

	"linapro-lark-sdk/larkhttp"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"
)

// userIDType 选择飞书通讯录查询所用的用户 ID 空间；它与
// authen access-token 换票返回的 user_id 一致。
const userIDType = "user_id"

// sdkProvider 是 Provider 的真实飞书实现，由
// larksuite/oapi-sdk-go/v3 支撑。每次调用都会加载每租户凭据，
// 使租户的 app_id/app_secret 变更无需进程重启即可生效。
type sdkProvider struct{}

// NewProvider 返回生产飞书 provider。
func NewProvider() Provider { return &sdkProvider{} }

// grantTypeAuthCode 是登录 code 换票的固定授权类型。
const grantTypeAuthCode = "authorization_code"

// ExchangeCode 使用 authen access-token API 实现 Provider，
// 身份来自 authen，部门来自 contact。
func (p *sdkProvider) ExchangeCode(ctx context.Context, cred Credential, code string) (LoginIdentity, error) {
	client := lark.NewClient(cred.AppID, cred.AppSecret, lark.WithHttpClient(larkhttp.NewClient()))

	identity, userAccessToken, err := p.exchangeIdentity(ctx, client, code)
	if err != nil {
		return LoginIdentity{}, err
	}

	// 部门需要用用户 access token 做一次独立的通讯录查询。
	// 缺少通讯录权限不应导致整个登录失败：软失败为空部门
	// （稍后由批量同步补上），但为运维记录失败原因。
	if identity.UserID != "" {
		depts, derr := p.resolveDepartments(ctx, client, identity.UserID, userAccessToken)
		if derr != nil {
			logger.Warningf(ctx, "linapro-employee-core 飞书部门查询软失败: %s", derr.Error())
		} else {
			identity.DepartmentIDs = depts
		}
	}
	return identity, nil
}

func (p *sdkProvider) exchangeIdentity(ctx context.Context, client *lark.Client, code string) (LoginIdentity, string, error) {
	req := authen.NewCreateAccessTokenReqBuilder().
		Body(authen.NewCreateAccessTokenReqBodyBuilder().
			GrantType(grantTypeAuthCode).
			Code(code).
			Build()).
		Build()

	resp, err := client.Authen.AccessToken.Create(ctx, req)
	if err != nil {
		return LoginIdentity{}, "", err
	}
	if !resp.Success() {
		return LoginIdentity{}, "", bizerr.NewCode(CodeAuthenFailed,
			bizerr.P("code", resp.Code), bizerr.P("msg", resp.Msg))
	}
	d := resp.Data
	return LoginIdentity{
		OpenID: larkcore.StringValue(d.OpenId),
		UserID: larkcore.StringValue(d.UserId),
		Name:   larkcore.StringValue(d.Name),
	}, larkcore.StringValue(d.AccessToken), nil
}

func (p *sdkProvider) resolveDepartments(ctx context.Context, client *lark.Client, userID, userAccessToken string) ([]string, error) {
	req := contact.NewGetUserReqBuilder().
		UserId(userID).
		UserIdType(userIDType).
		Build()

	resp, err := client.Contact.User.Get(ctx, req, larkcore.WithUserAccessToken(userAccessToken))
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, bizerr.NewCode(CodeContactFailed,
			bizerr.P("code", resp.Code), bizerr.P("msg", resp.Msg))
	}
	if resp.Data == nil || resp.Data.User == nil {
		return nil, nil
	}
	return resp.Data.User.DepartmentIds, nil
}
