// credential.go 解析飞书应用凭据，供登录换票与通讯录同步使用。
//
// 配置来源是本插件自己的私有配置键 plugin.linapro-employee-core.lark_apps，
// 平铺登记两类应用，以有无 identity_app_id 区分：
//   - 身份应用（无 identity_app_id）：参与通讯录同步、写 lark_identity，登录按 open_id 查员工；
//   - 免登应用（有 identity_app_id）：只用于员工端免登（如「360测评助手」网页应用），同步与
//     后台下拉都看不到它；登录用它自己的凭据换票取 user_id，再按其 identity_app_id 指向的
//     身份应用查 lark_identity。
//
// 密钥绝不存放在数据库中。当 app_id 未知或其密钥为空时解析按失败关闭处理，绝不回退到共享应用。
// LoadApps 只返回身份应用（同步、后台下拉、员工详情的口径不变）；登录经 NewCredentialLoader
// 能看到全部条目。两者都先校验整份清单，校验不过一律返回 bizerr，不带病空跑。
//
// 消息推送用的「推送应用」凭据仍属于 linapro-lark-push 自己的私有配置，本插件不读；
// 免登应用与推送应用可以是同一个飞书应用，但在两边各登记一次、各用各的。

package lark

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/plugin/capability/hostconfigcap"
)

// LarkAppsConfigKey 是本插件的飞书身份应用配置键。
const LarkAppsConfigKey = "plugin.linapro-employee-core.lark_apps"

// AppConfig 是单个飞书企业的应用配置，对应 lark_apps 数组的一个元素。
// 配置形如：
//
//	plugin:
//	  linapro-employee-core:
//	    lark_apps:
//	      - app_id: cli_xxx
//	        secret_key: "xxx"
//	        assoc: true   # 可选，默认 false=仅枚举 own
//	      - app_id: cli_yyy
//	        secret_key: "yyy"
//	        assoc: false
//	      - app_id: cli_zzz            # 免登应用：只用于员工端免登
//	        secret_key: "zzz"
//	        identity_app_id: cli_xxx   # 须指向本清单里的某个身份应用
type AppConfig struct {
	// AppID 是该飞书企业的 App ID（cli_xxx）。
	AppID string `json:"app_id"`
	// SecretKey 是该飞书应用的 App Secret。
	SecretKey string `json:"secret_key"`
	// Name 是可选的显示名（如「霍尔果斯飞书」），只用于后台展示与日志；
	// 不参与任何寻址逻辑，缺省为空。
	Name string `json:"name"`
	// Assoc 控制是否开启关联组织（assoc）枚举：默认 false 仅枚举 own；
	// 仅对需写多维表格跨租户人员字段的 app 置 true——置 true 的 app 同步时会额外
	// 调关联组织接口采集 assoc 身份行（id_source='assoc'，仅供写人员字段、
	// 不参与登录/考勤反查），assoc 枚举失败 soft-fail 不阻断 own。
	Assoc bool `json:"assoc"`
	// IdentityAppID 非空表示本条是免登应用：值为同企业身份应用的 app_id（须是本清单里
	// 未填 identity_app_id 的条目）。免登应用不参与同步，登录时按该身份应用查员工。
	// 空值表示本条就是身份应用。
	IdentityAppID string `json:"identity_app_id"`
}

// isLoginApp 报告本条是否为免登应用（只用于员工端免登，不参与同步）。
func (a AppConfig) isLoginApp() bool {
	return a.IdentityAppID != ""
}

// ErrCredentialMissing 在某个 lark_app_id 没有配置密钥时返回；
// 登录按失败关闭处理，而不是回退到共享应用。
var ErrCredentialMissing = bizerr.NewCode(CodeCredentialMissing)

// Credential 是一个飞书企业的 Lark 应用凭据。
type Credential struct {
	AppID     string
	AppSecret string
	// IdentityAppID 非空表示 AppID 是免登应用：登录换得的 user_id 须按这个身份应用
	// 查 lark_identity。空值表示 AppID 本身就是身份应用，登录按 open_id 查。
	IdentityAppID string
}

// CredentialLoader 按 lark_app_id 解析某个应用的凭据。它被注入登录服务，
// 使编排逻辑可以用 fake 进行单元测试。
type CredentialLoader func(ctx context.Context, larkAppID string) (Credential, error)

// LoadApps 读取本插件 lark_apps 里的**身份应用**清单（每个元素含 app_id、secret_key
// 与 assoc 开关），免登应用（填了 identity_app_id 的条目）不在结果中——同步、后台
// 「绑定飞书身份」下拉与员工详情只认身份应用，配置免登应用不改变它们拿到的清单。
//
// 配置缺失时返回空切片，以便调用方统一处理「无应用」；配置存在但结构非法或未通过
// 校验时返回错误（不返回部分结果，避免半份配置掩盖问题）。错误会经登录接口返回调用方，
// 因此统一为 bizerr：读取失败为 CodeLarkAppsConfigReadFailed，结构非法或校验不过为
// CodeLarkAppsConfigInvalid；具体原因作为 cause 保留，只进日志不进响应文案。
func LoadApps(ctx context.Context, hc hostconfigcap.Service) ([]AppConfig, error) {
	all, err := loadAllApps(ctx, hc)
	if err != nil {
		return nil, err
	}
	apps := make([]AppConfig, 0, len(all))
	for _, app := range all {
		if !app.isLoginApp() {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

// loadAllApps 读取并校验 lark_apps 的全部条目（身份应用与免登应用），顺序保持配置原样。
// 错误语义同 LoadApps。
func loadAllApps(ctx context.Context, hc hostconfigcap.Service) ([]AppConfig, error) {
	v, err := hc.Get(ctx, LarkAppsConfigKey, nil)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeLarkAppsConfigReadFailed)
	}
	if v == nil || v.IsNil() {
		return nil, nil
	}
	raw := v.Val()
	if raw == nil {
		return nil, nil
	}
	// gconv 对「本该是清单却给了字符串」这类格式错误会静默产出空清单，
	// 把配置错误伪装成「没配」，故先做粗粒度类型判定。
	switch raw.(type) {
	case []any, []map[string]any:
	default:
		return nil, bizerr.WrapCode(
			gerror.Newf("%s 必须是对象数组，实际类型为 %T", LarkAppsConfigKey, raw),
			CodeLarkAppsConfigInvalid,
		)
	}
	var apps []AppConfig
	if err := v.Scan(&apps); err != nil {
		return nil, bizerr.WrapCode(err, CodeLarkAppsConfigInvalid)
	}
	if err := validateApps(apps); err != nil {
		return nil, bizerr.WrapCode(err, CodeLarkAppsConfigInvalid)
	}
	return apps, nil
}

// validateApps 校验清单内部约束：
//   - app_id 不重复（空 app_id 不参与判重，同步与登录都会跳过它）；
//   - 免登应用的 identity_app_id 必须指向本清单里的身份应用，不能指向自身、另一个免登应用
//     或清单外的 app_id——否则登录会按一个不写 lark_identity 的应用查员工，永远查不到。
//
// 返回裸 gerror，由调用方包装为 CodeLarkAppsConfigInvalid。
func validateApps(apps []AppConfig) error {
	isIdentity := make(map[string]bool, len(apps)) // app_id → 是否身份应用
	for i, app := range apps {
		if app.AppID == "" {
			continue
		}
		if _, dup := isIdentity[app.AppID]; dup {
			return gerror.Newf("%s[%d].app_id 重复：%s", LarkAppsConfigKey, i, app.AppID)
		}
		isIdentity[app.AppID] = !app.isLoginApp()
	}
	for i, app := range apps {
		if !app.isLoginApp() {
			continue
		}
		if !isIdentity[app.IdentityAppID] {
			return gerror.Newf("%s[%d].identity_app_id=%s 必须指向同一清单中的身份应用（即不含 identity_app_id 的条目）",
				LarkAppsConfigKey, i, app.IdentityAppID)
		}
	}
	return nil
}

// NewCredentialLoader 返回一个由宿主机配置支撑的 CredentialLoader。
// 它能看到 lark_apps 的全部条目（身份应用与免登应用），免登应用的凭据带上
// IdentityAppID 供登录按 user_id 查员工。每次调用都重新读取配置，因此凭据修改
// 无需进程重启即可生效。
func NewCredentialLoader(hc hostconfigcap.Service) CredentialLoader {
	return func(ctx context.Context, larkAppID string) (Credential, error) {
		apps, err := loadAllApps(ctx, hc)
		if err != nil {
			return Credential{}, err
		}
		if larkAppID == "" {
			return Credential{}, ErrCredentialMissing
		}
		for _, app := range apps {
			if app.AppID == larkAppID && app.SecretKey != "" {
				return Credential{
					AppID:         larkAppID,
					AppSecret:     app.SecretKey,
					IdentityAppID: app.IdentityAppID,
				}, nil
			}
		}
		return Credential{}, ErrCredentialMissing
	}
}
