// credential.go 解析飞书应用凭据，供登录换票与通讯录同步使用。
//
// 配置来源是本插件自己的私有配置键 plugin.linapro-employee-core.lark_apps：
// 这里放的是**登录 / 通讯录同步应用**，密钥绝不存放在数据库中。当 app_id 未知或
// 其密钥为空时解析按失败关闭处理，绝不回退到共享应用。
//
// 消息推送用的「推送应用」凭据不在这里，属于 linapro-lark-push 自己的私有配置：
// 推送只需用工号经 lark_identity 查 user_id，查表不需要身份应用的密钥，因此
// 本插件没有任何理由去读推送配置；反过来，推送插件也不需要读这里的密钥。

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
}

// ErrCredentialMissing 在某个 lark_app_id 没有配置密钥时返回；
// 登录按失败关闭处理，而不是回退到共享应用。
var ErrCredentialMissing = bizerr.NewCode(CodeCredentialMissing)

// Credential 是一个飞书企业的 Lark 应用凭据。
type Credential struct {
	AppID     string
	AppSecret string
}

// CredentialLoader 按 lark_app_id 解析某个应用的凭据。它被注入登录服务，
// 使编排逻辑可以用 fake 进行单元测试。
type CredentialLoader func(ctx context.Context, larkAppID string) (Credential, error)

// LoadApps 读取本插件 lark_apps 里的身份应用清单（每个元素含 app_id、secret_key
// 与 assoc 开关）。
//
// 配置缺失时返回空切片，以便调用方统一处理「无应用」；配置存在但结构非法时返回
// 错误（不返回部分结果，避免半份配置掩盖问题）。错误会经登录接口返回调用方，
// 因此统一为 bizerr：读取失败为 CodeLarkAppsConfigReadFailed，结构非法为
// CodeLarkAppsConfigInvalid；具体原因作为 cause 保留，只进日志不进响应文案。
func LoadApps(ctx context.Context, hc hostconfigcap.Service) ([]AppConfig, error) {
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
			gerror.Newf("%s must be an object array, got %T", LarkAppsConfigKey, raw),
			CodeLarkAppsConfigInvalid,
		)
	}
	var apps []AppConfig
	if err := v.Scan(&apps); err != nil {
		return nil, bizerr.WrapCode(err, CodeLarkAppsConfigInvalid)
	}
	return apps, nil
}

// NewCredentialLoader 返回一个由宿主机配置支撑的 CredentialLoader。
// 它每次调用都重新读取 lark_apps 配置，因此凭据修改无需进程重启即可生效。
func NewCredentialLoader(hc hostconfigcap.Service) CredentialLoader {
	return func(ctx context.Context, larkAppID string) (Credential, error) {
		apps, err := LoadApps(ctx, hc)
		if err != nil {
			return Credential{}, err
		}
		if larkAppID == "" {
			return Credential{}, ErrCredentialMissing
		}
		for _, app := range apps {
			if app.AppID == larkAppID && app.SecretKey != "" {
				return Credential{AppID: larkAppID, AppSecret: app.SecretKey}, nil
			}
		}
		return Credential{}, ErrCredentialMissing
	}
}
