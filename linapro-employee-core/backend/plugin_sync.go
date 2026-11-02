// plugin_sync.go 提供生产用的 MokaFetcher 和按应用的 LarkAppFetcher 适配器，
// 把 sync 包的接口类型桥接到真实的外部客户端。把它们与 plugin.go 分离，
// 使主装配文件保持可读。
package backend

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
	"lina-plugin-linapro-employee-core/backend/internal/sync"
	"linapro-lark-sdk/larkcontact"
	"linapro-moka-hcm/hcm"
)

// 本插件的 Moka 凭据配置键（以插件 ID 作为命名空间）。
const (
	keyMokaAPIKey           = "plugin.linapro-employee-core.moka.apiKey"
	keyMokaEntCode          = "plugin.linapro-employee-core.moka.entCode"
	keyMokaPrivateKey       = "plugin.linapro-employee-core.moka.rsaPrivateKey"
	keyMokaBaseURL          = "plugin.linapro-employee-core.moka.baseURL"
	keyMokaAPICodeBatchData = "plugin.linapro-employee-core.moka.apiCodes.batchData"
	// operatorEmail 是 batch/data（员工列表）接口要求的操作人邮箱，作为签名查询参数 userName 传入。
	keyMokaOperatorEmail = "plugin.linapro-employee-core.moka.operatorEmail"
)

// -- MokaFetcher 适配器 -------------------------------------------------------

// mokaFetcherAdapter 把 *hcm.Client 适配为 sync.MokaFetcher，将 hcm.HCMEmployee
// 转换为 sync 包内的 MokaEmployee 类型。
type mokaFetcherAdapter struct {
	client *hcm.Client
}

func (a *mokaFetcherAdapter) ListAllEmployees(ctx context.Context) ([]sync.MokaEmployee, error) {
	hcmEmployees, err := a.client.ListAllEmployees(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sync.MokaEmployee, 0, len(hcmEmployees))
	for _, e := range hcmEmployees {
		out = append(out, sync.MokaEmployee{
			EmployeeNo:     e.EmployeeNo,
			Realname:       e.Realname,
			Department:     e.Department,
			OnBoardingDate: e.OnBoardingDate,
			Status:         e.Status,
		})
	}
	return out, nil
}

// newMokaFetcher 从宿主配置读取 Moka HCM 凭证并返回生产用 MokaFetcher 适配器。
// 必需凭证缺失时返回错误。
func newMokaFetcher(ctx context.Context, hc hostconfigcap.Service) (sync.MokaFetcher, error) {
	var (
		apiKey, entCode, pemKey         string
		baseURL, apiCode, operatorEmail string
	)
	// 逐键读取配置；任一键读取失败（配置后端异常）都直接返回，不当作「未配置」。
	reads := []struct {
		key string
		def string
		dst *string
	}{
		{keyMokaAPIKey, "", &apiKey},
		{keyMokaEntCode, "", &entCode},
		{keyMokaPrivateKey, "", &pemKey},
		{keyMokaBaseURL, hcm.DefaultBaseURL, &baseURL},
		{keyMokaAPICodeBatchData, "", &apiCode},
		{keyMokaOperatorEmail, "", &operatorEmail},
	}
	for _, r := range reads {
		v, err := hc.String(ctx, r.key, r.def)
		if err != nil {
			return nil, gerror.Wrapf(err, "employee directory sync: read config %s failed", r.key)
		}
		*r.dst = v
	}
	if apiKey == "" || entCode == "" || pemKey == "" {
		return nil, gerror.New("员工目录同步: Moka HCM 凭据不完整 (需要 apiKey/entCode/rsaPrivateKey)")
	}
	priv, err := hcm.ParsePrivateKey(pemKey)
	if err != nil {
		return nil, gerror.Wrap(err, "员工目录同步: 解析Moka RSA私钥失败")
	}
	cred := hcm.HCMCredential{
		APIKey:        apiKey,
		EntCode:       entCode,
		PrivateKey:    priv,
		OperatorEmail: operatorEmail,
		APICodes: map[string]string{
			hcm.APICodeKeyBatchData: apiCode,
		},
	}
	return &mokaFetcherAdapter{client: hcm.NewClient(baseURL, cred)}, nil
}

// -- LarkAppFetcher 适配器 ----------------------------------------------------

// larkAppFetcherAdapter 是共享库 larkcontact.Client 的薄壳，把中立的
// larkcontact.User 转换为 sync 包所需的 sync.LarkUser，实现 LarkAppFetcher。
// 通讯录遍历（部门递归、member_count 过滤、分页、按 open_id 去重）全部下沉
// 到共享库，本层只做类型映射。每个配置中的 lark_app_id 构造一个适配器。
type larkAppFetcherAdapter struct {
	client *larkcontact.Client
	// withAssoc 由 app 级开关决定：为 true 时追加关联组织（assoc）枚举，
	// 采集 id_source='assoc' 的共享成员（仅供写人员字段）；为 false 时仅枚举 own。
	withAssoc bool
}

// AppID 返回该适配器对应的飞书 App ID，用于写 lark_identity 时标识来源企业。
func (a *larkAppFetcherAdapter) AppID() string { return a.client.AppID() }

// ListUsers 枚举该飞书企业本租户（own）与（开关开启时）关联组织（assoc）的全部
// 通讯录用户（共享库负责遍历、assoc soft-fail 与按 open_id 去重），再转成 sync.LarkUser
// 并透传 Source（own/assoc）及 EmployeeNo（工号扩展字段）。own 通道遍历失败即返回 error，
// 由 syncer 记 error 并跳过该 app；assoc 通道失败在共享库内 soft-fail、不阻断 own。
func (a *larkAppFetcherAdapter) ListUsers(ctx context.Context) ([]sync.LarkUser, error) {
	contacts, err := a.client.ListAllUsers(ctx, a.withAssoc)
	if err != nil {
		return nil, err
	}
	users := make([]sync.LarkUser, 0, len(contacts))
	for _, c := range contacts {
		users = append(users, sync.LarkUser{
			Name:       c.Name,
			EmployeeNo: c.EmployeeNo,
			OpenID:     c.OpenID,
			UserID:     c.UserID,
			Source:     c.Source,
		})
	}
	return users, nil
}

// newLarkFetchers 从 lark_apps 配置为每个应用构造一个 LarkAppFetcher。
// 空数组时返回空切片（Moka-only 同步，不做飞书回填）。元素 assoc=true 的 app
// 额外开启关联组织枚举（assoc）。
func newLarkFetchers(ctx context.Context, hc hostconfigcap.Service) ([]sync.LarkAppFetcher, error) {
	apps, err := lark.LoadApps(ctx, hc)
	if err != nil {
		return nil, gerror.Wrap(err, "员工目录同步: 读取飞书应用配置失败")
	}
	fetchers := make([]sync.LarkAppFetcher, 0, len(apps))
	for _, app := range apps {
		if app.AppID == "" || app.SecretKey == "" {
			continue
		}
		fetchers = append(fetchers, &larkAppFetcherAdapter{
			client:    larkcontact.NewClient(app.AppID, app.SecretKey),
			withAssoc: app.Assoc,
		})
	}
	return fetchers, nil
}
