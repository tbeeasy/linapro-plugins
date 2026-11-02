// validate.go 承担推送配置的合法性校验。
//
// 校验目标是让「配置错误」在发送前就暴露，而不是等到某条告警发不出去时才发现。
// 本文件只产出问题清单，不决定「谁能启动」——推送插件缺推送应用只是该能力不可用。
//
// 校验按分组独立进行：一个分组非法只让该分组不可用，不拖垮其它调用方的推送。
// 同一 app_id 允许出现在多个分组（是否共用机器人由运维决定），故不做跨组校验。
//
// 能校验的只有单清单内部的约束。identity_app_id 指向的是 linapro-employee-core
// 配置里的身份应用，本插件看不到那份配置，故无法校验其存在性——填错的表现是
// Status 里该应用的可寻址人数为 0，属于运行期可观测，不是配置期可拦截。

package larkcfg

import (
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// Issue 是一条配置问题。Field 用点分路径定位到具体配置项，便于运维直接照着改。
type Issue struct {
	// Field 是出问题的配置路径，如 push_groups.linapro-eval-360[0].identity_app_id。
	Field string
	// Message 是中文问题描述。
	Message string
}

// String 便于在日志里直接打印一条问题。
func (i Issue) String() string {
	return i.Field + ": " + i.Message
}

// Validate 校验某个分组内推送应用清单的不变式，返回全部问题（空切片表示合法）。
// group 只用于拼问题项的字段路径；未配置的分组（apps 为空）没有问题项。
//
// 校验项（均在组内）：
//   - app_id / secret_key / identity_app_id 非空；
//   - app_id 唯一；
//   - 同一个 identity_app_id 不被两个推送应用引用（否则同一个人在同一企业会收到两条）。
func Validate(group string, apps []PushApp) []Issue {
	var issues []Issue

	ids := make(map[string]int, len(apps))         // push app_id → 组内下标
	identityUse := make(map[string]int, len(apps)) // identity_app_id → 首次引用它的组内下标
	for i, app := range apps {
		field := groupField(group) + "[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(app.AppID) == "" {
			issues = append(issues, Issue{Field: field + ".app_id", Message: "app_id 不能为空"})
		} else if prev, dup := ids[app.AppID]; dup {
			issues = append(issues, Issue{Field: field + ".app_id",
				Message: "app_id 重复（与 " + groupField(group) + "[" + strconv.Itoa(prev) + "] 相同）"})
		} else {
			ids[app.AppID] = i
		}
		if strings.TrimSpace(app.SecretKey) == "" {
			issues = append(issues, Issue{Field: field + ".secret_key", Message: "secret_key 不能为空"})
		}

		id := strings.TrimSpace(app.IdentityAppID)
		if id == "" {
			issues = append(issues, Issue{Field: field + ".identity_app_id",
				Message: "identity_app_id 不能为空（须填该企业登录/同步应用在 employee-core 配置中的 app_id）"})
			continue
		}
		if prev, dup := identityUse[id]; dup {
			issues = append(issues, Issue{Field: field + ".identity_app_id",
				Message: "该身份应用已被 " + groupField(group) + "[" + strconv.Itoa(prev) + "] 引用：同一人会在同一企业收到两条消息"})
			continue
		}
		identityUse[id] = i
	}

	return issues
}

// groupField 返回某分组在配置中的路径前缀，如 push_groups.linapro-eval-360。
func groupField(group string) string {
	return "push_groups." + group
}

// ErrValidate 把问题清单折叠成一个便于返回的错误；无问题时返回 nil。
func ErrValidate(issues []Issue) error {
	if len(issues) == 0 {
		return nil
	}
	parts := make([]string, 0, len(issues))
	for _, it := range issues {
		parts = append(parts, it.String())
	}
	return gerror.New("larkcfg: 飞书推送配置非法——" + strings.Join(parts, "；"))
}

// Pushable 返回参与投递的推送应用清单：剔除凭据不全的条目。
//
// 凭据不全的条目「只是不可用」，不应让整个推送能力报错（其余企业照常可发），
// 故这里静默剔除；这类条目的存在会由 Validate 报告。调用方据此判断
// 「有无可用推送应用」——空清单即无法投递。
func Pushable(apps []PushApp) []PushApp {
	out := make([]PushApp, 0, len(apps))
	for _, app := range apps {
		if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.SecretKey) == "" {
			continue
		}
		out = append(out, app)
	}
	return out
}
