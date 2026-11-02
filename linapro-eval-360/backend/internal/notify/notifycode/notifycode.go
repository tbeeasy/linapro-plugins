// 本文件定义 linapro-eval-360 飞书推送编排模块（eval-notify）的业务错误码及失败清单参数构造。
package notifycode

import (
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"

	"lina-core/pkg/bizerr"
)

var (
	// CodeEmpcapUnbound empcap 员工查询服务未绑定（employee-core 未启用）。
	CodeEmpcapUnbound = bizerr.MustDefine(
		"EVAL360_NOTIFY_EMPCAP_UNBOUND", "员工能力服务未就绪，无法推送", gcode.CodeNotImplemented,
	)
	// CodeLarkPushUnbound 飞书推送能力（linapro-lark-push 的 larkpushcap）未就绪。
	CodeLarkPushUnbound = bizerr.MustDefine(
		"EVAL360_NOTIFY_LARK_PUSH_UNBOUND", "飞书推送能力未就绪，请确认已启用 linapro-lark-push", gcode.CodeNotImplemented,
	)
	// CodeHostConfigMissing 宿主配置能力不可用（读不到卡片推送配置）。
	CodeHostConfigMissing = bizerr.MustDefine(
		"EVAL360_NOTIFY_HOST_CONFIG_MISSING", "宿主配置能力不可用", gcode.CodeNotImplemented,
	)
	// CodeCardConfigMissing 飞书卡片推送配置缺失或非法（待办 H5 链接 / 卡片模板 / 推送应用），
	// 一键发送在发布前据此拦截。参数 item 为缺失或非法的配置键。
	CodeCardConfigMissing = bizerr.MustDefine(
		"EVAL360_NOTIFY_CARD_CONFIG_MISSING", "Lark card push configuration is missing or invalid: {item}", gcode.CodeInvalidConfiguration,
	)
	// CodeEvaluatorsUnreachable 发布前干跑发现有评价者在任何企业都无法投递（无飞书身份、
	// user_id 为空、工号碰撞或卡片构造失败），整批拒绝发布、未发出任何卡片。
	// 参数 count 为不可达人数，list 为其工号（超长截断）。
	CodeEvaluatorsUnreachable = bizerr.MustDefine(
		"EVAL360_NOTIFY_EVALUATORS_UNREACHABLE",
		"{count} evaluator(s) cannot receive the Lark card; nothing was published: {list}",
		gcode.CodeInvalidOperation,
	)
	// CodePushFailed 正式投递后仍有评价者未送达（飞书拒绝、限流、网络失败），问卷不发布。
	// 已送达者的卡片无法撤回，重新发布时会再收到一张。参数 count 为未送达人数，list 为其工号（超长截断）。
	CodePushFailed = bizerr.MustDefine(
		"EVAL360_NOTIFY_PUSH_FAILED",
		"Lark card delivery failed for {count} evaluator(s); nothing was published (evaluators who already received a card will get another one when you publish again): {list}",
		gcode.CodeOperationFailed,
	)
)

// maxFailedListed 是失败清单在错误文案中最多列出的人数，超出部分以省略号表示（count 仍为总数）。
const maxFailedListed = 20

// FailedParams 构造失败类错误码（CodeEvaluatorsUnreachable / CodePushFailed）的参数：
// count 为失败人数，list 为以逗号分隔的评价者标签（工号或 EmpID），超过 20 人时只列前 20 人并加省略号。
func FailedParams(labels []string) []bizerr.Param {
	listed := labels
	suffix := ""
	if len(listed) > maxFailedListed {
		listed, suffix = listed[:maxFailedListed], " ..."
	}
	return []bizerr.Param{
		bizerr.P("count", len(labels)),
		bizerr.P("list", strings.Join(listed, ", ")+suffix),
	}
}
