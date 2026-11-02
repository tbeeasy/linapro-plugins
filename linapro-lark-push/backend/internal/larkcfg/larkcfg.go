// Package larkcfg 读取并校验 linapro-lark-push 自己的推送应用配置。
//
// 配置键是插件私有键 plugin.linapro-lark-push.push_groups，值是「分组名 → 推送应用清单」
// 的映射。分组名取调用方插件 ID（对外 HTTP 调用方的分组由签名凭据绑定、名字自定），
// 不同调用方用不同推送应用，接收方看到的机器人名也就不同：
//
//	plugin:
//	  linapro-lark-push:
//	    push_groups:
//	      linapro-eval-360:
//	        - app_id: cli_yyy
//	          secret_key: "yyy"
//	          name: 360测评助手(霍尔果斯)
//	          identity_app_id: cli_xxx   # 投递时取哪个企业的 user_id
//
// 本包只负责推送应用这一件事。登录 / 通讯录同步应用的凭据属于
// linapro-employee-core 自己的配置（plugin.linapro-employee-core.lark_apps），
// 本插件不需要、也不读取它们——推送只用工号经 lark_identity 查 user_id，
// 查表不需要任何身份应用的密钥。
//
// 本包只做「结构体 + Load + Validate」，不含任何旧键（push_apps）兼容与迁移兜底：
// 兜底是迁移期策略，放进长期代码等于让它永久存活。
package larkcfg

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"
)

// ConfigKey 是本插件的推送分组配置键（插件私有，单消费方）。
const ConfigKey = "plugin.linapro-lark-push.push_groups"

// Getter 是本包需要的最小宿主配置读取接口。
//
// 宿主 hostconfigcap.Service 的方法集包含同一签名的方法，因此可直接传入，无需适配器。
type Getter interface {
	// Get 返回配置键对应的值；键不存在时返回 defaultValue 包装的值。
	Get(ctx context.Context, key string, defaultValue any) (*gvar.Var, error)
}

// PushApp 是一个消息推送应用的配置。它与登录/同步应用是**不同的飞书应用**
// （机器人名不同、责任边界不同），投递凭据只可能取自这里。
type PushApp struct {
	// AppID 是飞书应用 App ID（cli_xxx）。
	AppID string `json:"app_id"`
	// SecretKey 是飞书应用 App Secret。
	SecretKey string `json:"secret_key"`
	// Name 仅用于日志与结果标识（如「系统通知(霍尔果斯)」），不驱动飞书侧机器人名。
	Name string `json:"name"`
	// IdentityAppID 声明本推送应用投递时使用的 user_id 取自哪个企业的身份应用，
	// 即按哪个 lark_app_id 查「工号 → user_id」映射。它指向 linapro-employee-core
	// 配置里的一个身份应用，本包无法校验其存在性，由 Status 的可寻址人数暴露。
	IdentityAppID string `json:"identity_app_id"`
}

// Load 从宿主配置读取全部推送分组（分组名 → 推送应用清单）。
//
// 配置缺失时返回空映射且不报错（由调用方决定「分组无推送应用」是否构成错误）；配置存在但
// 结构无法解析时返回错误，**不返回部分解析结果**——结构坏了就无法可靠划分分组，
// 半份配置比没有配置更难排查。
func Load(ctx context.Context, getter Getter) (map[string][]PushApp, error) {
	if getter == nil {
		return nil, gerror.New("larkcfg: 配置读取器为空")
	}
	v, err := getter.Get(ctx, ConfigKey, nil)
	if err != nil {
		return nil, gerror.Wrapf(err, "larkcfg: 读取 %s 配置失败", ConfigKey)
	}
	if v == nil || v.IsNil() {
		return map[string][]PushApp{}, nil
	}
	return decodeGroups(v.Val())
}

// decodeGroups 把配置值解析为「分组名 → 推送应用清单」。分组名去首尾空白后不得为空。
func decodeGroups(root any) (map[string][]PushApp, error) {
	entries, ok := mapEntries(root)
	if !ok {
		return nil, gerror.Newf("larkcfg: %s 格式非法（应为「分组名 → 推送应用数组」的映射，实际为 %T）", ConfigKey, root)
	}
	out := make(map[string][]PushApp, len(entries))
	for name, value := range entries {
		group := strings.TrimSpace(name)
		if group == "" {
			return nil, gerror.Newf("larkcfg: %s 含空分组名", ConfigKey)
		}
		if _, dup := out[group]; dup {
			return nil, gerror.Newf("larkcfg: %s 分组名 %q 重复（去首尾空白后相同）", ConfigKey, group)
		}
		apps, err := decodeList(group, value)
		if err != nil {
			return nil, err
		}
		out[group] = apps
	}
	return out, nil
}

// mapEntries 把宿主流式返回的几种映射形态统一为 map[string]any。
func mapEntries(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[gconv.String(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

// decodeList 把某个分组的配置值解析为 []PushApp。空值视为空清单。
//
// 这里刻意不直接 Scan：gconv 对「本该是清单却给了字符串」这类格式错误会静默产出空
// 清单（例如 "oops" → []），从而把配置错误伪装成「没配」。故先做粗粒度类型判定，
// 再逐项解析。
func decodeList(group string, root any) ([]PushApp, error) {
	field := ConfigKey + "." + group
	switch items := root.(type) {
	case nil:
		return nil, nil
	case []any:
		for i, item := range items {
			if !isMap(item) {
				return nil, gerror.Newf("larkcfg: %s[%d] 格式非法（应为对象，实际为 %T）", field, i, item)
			}
		}
	case []map[string]any:
		// 已是结构化清单，交给下面的 gconv 解析。
	default:
		return nil, gerror.Newf("larkcfg: %s 格式非法（应为对象数组，实际为 %T）", field, root)
	}

	var out []PushApp
	if err := gconv.Structs(root, &out); err != nil {
		return nil, gerror.Wrapf(err, "larkcfg: 解析 %s 失败", field)
	}
	return out, nil
}

// isMap 判定配置值是否为对象（支持宿主流式返回的几种映射形态）。
func isMap(v any) bool {
	switch v.(type) {
	case map[string]any, map[any]any, map[string]string:
		return true
	default:
		return false
	}
}
