// Package notify 实现 360 测评「飞书推送编排」（eval-notify）。
//
// 它实现 project.Sender 接口，在写入发布状态前按评价者粒度推送飞书交互卡片：
//   - 一评价者一张卡：同一评价者名下 N 个待评对象只发一张卡。卡片版式由飞书
//     卡片模板维护（按推送应用分别配置于 card_templates），本插件只填
//     模板变量 name（评价者姓名）与 dynamic_evaluation_url（员工端「我的待办」H5，
//     不按关系深链）。
//   - 投递经 linapro-lark-push，并以本插件 ID 为推送分组（push_groups.linapro-eval-360）：
//     测评卡片由独立的测评机器人发出，不与其它插件共用。本插件只给工号与卡片构造函数，寻址（工号 → 企业级
//     user_id）、推送应用凭据与跨企业扇出都由 lark-push 负责；本插件不读取任何飞书
//     应用凭据，也不读 linapro-employee-core 的配置。
//   - 卡片模板按推送应用区分：模板须授权给实际发送的推送应用、跨企业不可复用，故每个
//     参与投递的推送应用都必须在 card_templates 中单独配置，无全局兜底。
//   - 链接按企业区分：dynamic_evaluation_url 在 todo_h5_url 上追加
//     larkAppId=<该推送应用的 identity_app_id>。员工端登录页按它选用对应企业的
//     **身份应用**换票（推送应用不参与登录），故不能填推送应用自身的 app_id。
//   - 双发可接受：同一评价者在两个企业都有身份时会各收到一张卡，本期不去重。
//   - 按问卷判定送达：先对全部待发布问卷的评价者并集经 lark-push 干跑一次（只寻址与构造卡片、
//     不调飞书），有评价者不可达的问卷判为不可达失败、其评价者不因它发卡；只向干跑通过问卷的
//     评价者并集正式投递（每人一张卡），单条失败不中断同批其余投递，任一企业投递成功即视为
//     已触达；投递后仍有评价者未送达的问卷判为投递失败。结果按问卷回报（评价者工号粒度），
//     调用方只发布通过的问卷。
//
// 依赖（见 openspec/changes/linapro-eval-360/design.md D4）：
//   - linapro-lark-push/backend/cap/larkpushcap：推送应用清单与卡片投递。
//   - linapro-lark-sdk/larkim：仅用于构造模板卡片 JSON（BuildTemplateCard），不发送。
//   - linapro-employee-core/backend/cap/empcap：EmpID → 工号 / 姓名的员工查询。
//   - 宿主 hostconfigcap：读取本插件自有的卡片模板与「我的待办」H5 URL。
package notify

import (
	"context"

	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-eval-360/backend/internal/service/project"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
)

// 本插件自有的推送配置键（宿主配置 plugin.linapro-eval-360.*）。飞书应用凭据不在
// 本插件配置里，由 linapro-lark-push 的 push_groups.linapro-eval-360 统一管理。
const (
	// todoH5URLConfigKey 是员工端「我的待办」H5 的完整 URL（hash 路由时含 #/ 段）。
	// 发送时按企业追加 larkAppId 参数后填入卡片模板变量 dynamic_evaluation_url。
	todoH5URLConfigKey = "plugin.linapro-eval-360.todo_h5_url"
	// cardTemplatesConfigKey 是按推送应用分别配置的卡片模板数组（唯一来源）。
	// 元素结构：{app_id, template_id, template_version}，app_id 为 linapro-lark-push
	// 本插件分组（push_groups.linapro-eval-360）中推送应用的 app_id；不做全局兜底。
	cardTemplatesConfigKey = "plugin.linapro-eval-360.card_templates"
	// pushAppsConfigKey 是 linapro-lark-push 中本插件推送分组的配置键，仅用于在「分组无推送
	// 应用」时提示管理员去哪里补配置；本插件不读取它。
	pushAppsConfigKey = "plugin.linapro-lark-push.push_groups." + pushGroup
)

// pushGroup 是本插件在 linapro-lark-push 中的推送分组（= 本插件 ID）：只用该分组的推送应用投递。
const pushGroup = "linapro-eval-360"

// bizSource 是投递日志中的调用方标识（只用于日志对账，不参与选路）。
const bizSource = "linapro-eval-360"

// 卡片模板变量名与链接参数名，须与飞书卡片模板中声明的变量一致。
const (
	// cardVarName 是模板变量：评价者姓名。
	cardVarName = "name"
	// cardVarEvaluationURL 是模板变量：「我的待办」H5 跳转链接。
	cardVarEvaluationURL = "dynamic_evaluation_url"
	// larkAppIDQueryKey 是员工端登录页识别飞书企业应用的 URL 参数名。
	larkAppIDQueryKey = "larkAppId"
)

// 飞书 AppLink「在飞书中打开网页」协议：卡片链接经它包装后由飞书客户端内置浏览器
// 打开（而非系统浏览器），员工端才能复用飞书登录态完成免登。需飞书 7.5 及以上版本。
const (
	// larkWebURLAppLink 是 AppLink 打开网页的协议地址。
	larkWebURLAppLink = "https://applink.feishu.cn/client/web_url/open"
	// appLinkModeWindow 表示在飞书独立窗口中打开（问卷内容多，侧边栏过窄）。
	appLinkModeWindow = "window"
)

// Sender 是一键发送的推送编排实现（实现 project.Sender）。
type Sender interface {
	// Precheck 在发布问卷前校验推送配置完整：todo_h5_url 合法、linapro-lark-push 至少有
	// 一个可用推送应用（本插件分组内），且每个推送应用都配置了卡片模板。已发布问卷会被一键发送跳过、
	// 不再重推，因此配置缺失必须在发布前拦截；缺失时返回 notifycode.CodeCardConfigMissing
	// （参数 item 为缺失的配置键，分组无推送应用时为 lark-push 的 push_groups.linapro-eval-360 键），lark-push
	// 未就绪返回 notifycode.CodeLarkPushUnbound，lark-push 配置非法时透传其业务错误。
	Precheck(ctx context.Context) error
	// SendAll 按评价者粒度推送飞书模板卡片，并按问卷回报结果（实现 project.Sender）。
	// questionnaireIDs 为本次待发布（尚未写发布状态）的问卷；为空时返回零值结果。
	//   - 干跑：对全部问卷评价者并集只干跑一次；某问卷有评价者在任何企业都不可投递（无工号、
	//     无身份、user_id 为空、碰撞、卡片构造失败）即记入 PushOutcome.Unreachable（值为该问卷
	//     不可达者工号，无工号回退 EmpID）。
	//   - 正式投递：只发给干跑通过问卷的评价者并集，只出现在不可达问卷中的评价者不发卡；投递后
	//     仍有评价者未送达的问卷记入 PushOutcome.Failed（已送达的卡片不撤回）。一名评价者未送达
	//     会使其所在的全部问卷失败。
	//   - 未出现在结果中的问卷即通过（含没有未填评价关系的问卷）。
	// 请求级错误返回 error，调用方不得发布任何问卷：empcap 未绑定返回 CodeEmpcapUnbound，
	// lark-push 未就绪返回 CodeLarkPushUnbound，卡片配置不可读或 lark-push 配置非法时透传对应错误。
	SendAll(ctx context.Context, projectID int64, questionnaireIDs []int64) (project.PushOutcome, error)
}

// 编译期断言 senderImpl 实现 Sender 与 project.Sender。
var (
	_ Sender         = (*senderImpl)(nil)
	_ project.Sender = (*senderImpl)(nil)
)

// senderImpl 是 Sender 的默认实现。
type senderImpl struct {
	emp        EmpLookup             // 员工查询（empcap）：EmpID → 工号 / 姓名
	hostConfig hostconfigcap.Service // 读取卡片模板与 todo H5 URL
	pusher     Pusher                // 飞书投递（larkpushcap）
}

// EmpLookup 收窄 empcap.Service 到 notify 实际消费的只读方法，便于单测注入 fake。
type EmpLookup interface {
	// MapByEmpIDs 按 EmpID 集合批量返回员工（取工号用于寻址、姓名用于卡片称呼，无 N+1）。
	MapByEmpIDs(ctx context.Context, empIDs []int64) (map[int64]*empcap.Employee, error)
}

// Pusher 收窄 larkpushcap 门面到 notify 实际消费的方法，便于单测注入 fake。
type Pusher interface {
	// ListPushApps 返回 group 分组内参与投递的推送应用（不含密钥），供发布前按应用预检卡片模板。
	ListPushApps(ctx context.Context, group string) ([]larkpushcap.PushApp, error)
	// SendCard 按工号扇出投递交互卡片，返回逐「收件人 × 推送应用」明细（req.DryRun 时只干跑）。
	SendCard(ctx context.Context, req larkpushcap.SendCardRequest) (larkpushcap.SendResult, error)
}

// New 构造推送编排 Sender。
//   - emp：员工查询（生产传 empcap.BoundEmployeeService()）。
//   - hostConfig：宿主配置（读卡片模板与 todo H5 URL）。
//   - pusher：飞书投递（生产传懒取 larkpushcap 绑定的实现，测试可注入 fake）。
func New(emp EmpLookup, hostConfig hostconfigcap.Service, pusher Pusher) Sender {
	return &senderImpl{emp: emp, hostConfig: hostConfig, pusher: pusher}
}

// cardTemplatesCfg 是 card_templates 数组元素的本地最小投影：某推送应用使用的卡片模板。
// app_id 为推送应用 App ID；template_version 留空表示使用模板当前发布版本。
type cardTemplatesCfg struct {
	AppID           string `json:"app_id"`
	TemplateID      string `json:"template_id"`
	TemplateVersion string `json:"template_version"`
}
