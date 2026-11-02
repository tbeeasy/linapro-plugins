// notify_impl.go 实现一键发送的飞书推送编排（tasks 8.4 / 8.5）。
//
// 推送单元 = 评价者（一人一卡）。寻址、推送应用凭据与跨企业扇出交给 linapro-lark-push
// （larkpushcap.SendCard）：本插件只给工号与卡片构造函数。卡片为飞书模板卡片：变量
// name=评价者姓名、dynamic_evaluation_url=带该企业身份应用 larkAppId 参数的「我的待办」
// H5 链接（员工端登录页据此选用身份应用换票），并经飞书 AppLink 包装为在飞书内置浏览器中
// 打开（系统浏览器无飞书登录态、无法免登）。任一企业投递成功即视为已触达。
//
// 发布按问卷判定（调用方只发布结果中未出现的问卷）：对全部待发布问卷的评价者并集经 lark-push
// 干跑一次（只寻址与构造卡片、不调飞书），有评价者不可达的问卷记为不可达失败；只向干跑通过
// 问卷的评价者并集正式投递，投递后仍有评价者未送达的问卷记为投递失败。
package notify

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/logger"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/notify/notifycode"
	"lina-plugin-linapro-eval-360/backend/internal/service/project"
	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"

	"linapro-lark-sdk/larkim"
)

// cardTemplate 是一次推送使用的卡片模板配置（读自宿主配置）。
// 卡片模板按推送应用分别配置：模板须授权给发送应用、跨应用不可复用，故只按推送应用取用、无兜底。
type cardTemplate struct {
	todoURL      string                       // 「我的待办」H5 完整 URL（必填，发送时按企业追加 larkAppId）
	appTemplates map[string]cardTemplateEntry // 推送应用 app_id → 该应用的卡片模板
}

// cardTemplateEntry 是某推送应用的卡片模板（模板 ID + 版本名）。
type cardTemplateEntry struct {
	templateID  string
	versionName string
}

// entryFor 返回指定推送应用应使用的卡片模板。该应用未配置模板时返回 ok=false
// （调用方应判该应用无法推送；Precheck 正常情况下已在发布前拦截）。
func (c cardTemplate) entryFor(appID string) (cardTemplateEntry, bool) {
	e, ok := c.appTemplates[appID]
	if !ok || e.templateID == "" {
		return cardTemplateEntry{}, false
	}
	return e, true
}

// Precheck 在发布前校验推送配置完整：todo_h5_url 须配置且 URL 可解析；linapro-lark-push
// 中本插件分组须至少有一个可用推送应用，且每个推送应用都须在 card_templates 中有卡片模板，否则该企业
// 的员工收不到卡片——须在发布前拦截（发布后再补配置不会重推）。
func (s *senderImpl) Precheck(ctx context.Context) error {
	card, err := s.loadCardTemplate(ctx)
	if err != nil {
		return err
	}
	apps, err := s.listPushApps(ctx)
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		return bizerr.NewCode(notifycode.CodeCardConfigMissing, bizerr.P("item", pushAppsConfigKey))
	}
	for _, app := range apps {
		if _, ok := card.entryFor(app.AppID); ok {
			continue
		}
		return bizerr.NewCode(notifycode.CodeCardConfigMissing,
			bizerr.P("item", cardTemplatesConfigKey+"[app_id="+app.AppID+"]"))
	}
	return nil
}

// SendAll 按评价者粒度推送飞书模板卡片，并按问卷回报推送结果。
//
// 流程（见 design.md D4 / D5）：
//  1. 一次查询收集各待发布问卷下「未填」评价关系的去重评价者 EmpID（一人一卡，指向待办 H5）。
//  2. 对全部问卷评价者的并集一次批量解析工号（寻址键）与姓名（模板变量 name）。
//  3. 经 lark-push 干跑 + 正式投递并逐份判定（见 pushEvaluators）。同一评价者在多个企业有
//     身份时各发一张（双发可接受、不去重）。
func (s *senderImpl) SendAll(ctx context.Context, projectID int64, questionnaireIDs []int64) (project.PushOutcome, error) {
	if len(questionnaireIDs) == 0 {
		// 无待发布问卷（已发布跳过、幂等）→ 无需推送。
		return project.PushOutcome{}, nil
	}
	if s.emp == nil {
		// empcap 未绑定 → 无法寻址，fail-closed：返回错误而非静默吞掉。
		return project.PushOutcome{}, bizerr.NewCode(notifycode.CodeEmpcapUnbound)
	}
	card, err := s.loadCardTemplate(ctx)
	if err != nil {
		return project.PushOutcome{}, err
	}

	// 1. 问卷 id → 去重评价者 EmpID（仅未填关系——「待评对象」语义）。
	empIDsByQ, err := s.collectPendingEvaluators(ctx, questionnaireIDs)
	if err != nil {
		return project.PushOutcome{}, err
	}

	// 2. 评价者并集一次解析为工号 + 姓名（寻址键、失败清单回报粒度、卡片称呼）。
	evaluatorByID, err := s.resolveEvaluators(ctx, unionEmpIDs(questionnaireIDs, empIDsByQ))
	if err != nil {
		return project.PushOutcome{}, err
	}
	groups := make([]questionnaireEvaluators, 0, len(questionnaireIDs))
	for _, qID := range questionnaireIDs {
		group := questionnaireEvaluators{questionnaireID: qID}
		for _, empID := range empIDsByQ[qID] {
			group.evaluators = append(group.evaluators, evaluatorByID[empID])
		}
		groups = append(groups, group)
	}

	// 3. 干跑 + 正式投递 + 逐份判定。
	return s.pushEvaluators(ctx, projectID, groups, card)
}

// unionEmpIDs 按问卷顺序、问卷内评价者顺序返回各问卷评价者 EmpID 的去重并集。
func unionEmpIDs(questionnaireIDs []int64, empIDsByQ map[int64][]int64) []int64 {
	seen := make(map[int64]struct{})
	out := make([]int64, 0)
	for _, qID := range questionnaireIDs {
		for _, empID := range empIDsByQ[qID] {
			if _, ok := seen[empID]; ok {
				continue
			}
			seen[empID] = struct{}{}
			out = append(out, empID)
		}
	}
	return out
}

// loadCardTemplate 读取待办链接与按推送应用配置的卡片模板。
// 宿主配置能力缺失返回 CodeHostConfigMissing；待办链接缺失或 URL 不可解析返回
// CodeCardConfigMissing。各应用的模板是否齐备由 Precheck 逐个校验，此处不拦截。
func (s *senderImpl) loadCardTemplate(ctx context.Context) (cardTemplate, error) {
	if s.hostConfig == nil {
		return cardTemplate{}, bizerr.NewCode(notifycode.CodeHostConfigMissing)
	}
	var (
		card cardTemplate
		err  error
	)
	if card.todoURL, err = s.hostConfig.String(ctx, todoH5URLConfigKey, ""); err != nil {
		return cardTemplate{}, gerror.Wrap(err, "eval-360 notify: read todo H5 URL config failed")
	}
	if card.appTemplates, err = s.loadAppCardTemplates(ctx); err != nil {
		return cardTemplate{}, err
	}
	card.todoURL = strings.TrimSpace(card.todoURL)
	if card.todoURL == "" {
		return cardTemplate{}, bizerr.NewCode(notifycode.CodeCardConfigMissing, bizerr.P("item", todoH5URLConfigKey))
	}
	if _, err = withLarkAppID(card.todoURL, ""); err != nil {
		return cardTemplate{}, bizerr.WrapCode(err, notifycode.CodeCardConfigMissing, bizerr.P("item", todoH5URLConfigKey))
	}
	return card, nil
}

// loadAppCardTemplates 读取按飞书应用配置的卡片模板数组（card_templates）。
// 解析失败返回错误；未配置返回空映射（此时 Precheck 会按应用报缺模板）。
// 同一 app_id 重复配置时以最后一条为准。
func (s *senderImpl) loadAppCardTemplates(ctx context.Context) (map[string]cardTemplateEntry, error) {
	v, err := s.hostConfig.Get(ctx, cardTemplatesConfigKey, nil)
	if err != nil {
		return nil, gerror.Wrap(err, "eval-360 notify: 读取卡片模板配置失败")
	}
	if v == nil || v.IsNil() {
		return nil, nil
	}
	var items []cardTemplatesCfg
	if err := v.Scan(&items); err != nil {
		return nil, gerror.Wrap(err, "eval-360 notify: 解析卡片模板配置失败")
	}
	out := make(map[string]cardTemplateEntry, len(items))
	for _, it := range items {
		appID := strings.TrimSpace(it.AppID)
		templateID := strings.TrimSpace(it.TemplateID)
		if appID == "" || templateID == "" {
			continue // 不完整的条目忽略，由 Precheck 按应用报缺。
		}
		out[appID] = cardTemplateEntry{
			templateID:  templateID,
			versionName: strings.TrimSpace(it.TemplateVersion),
		}
	}
	return out, nil
}

// withLarkAppID 在待办 H5 链接上设置 larkAppId 参数。hash 路由链接（含 #/...）的
// 路由参数位于 fragment 内，参数须写进 fragment 的 query 才能被前端路由读到；
// 无 fragment 时写入普通 query。appID 为空时只做可解析校验、原样返回。
func withLarkAppID(rawURL, appID string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", gerror.Wrapf(err, "eval-360 notify: todo H5 URL is not parseable: %s", rawURL)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", gerror.Newf("eval-360 notify: todo H5 URL must be absolute with scheme and host: %s", rawURL)
	}
	if appID == "" {
		return rawURL, nil
	}
	if u.Fragment != "" {
		routePath, routeQuery, _ := strings.Cut(u.Fragment, "?")
		query, qErr := url.ParseQuery(routeQuery)
		if qErr != nil {
			return "", gerror.Wrapf(qErr, "eval-360 notify: todo H5 URL route query is not parseable: %s", rawURL)
		}
		query.Set(larkAppIDQueryKey, appID)
		u.Fragment = routePath + "?" + query.Encode()
		u.RawFragment = ""
		return u.String(), nil
	}
	query := u.Query()
	query.Set(larkAppIDQueryKey, appID)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// wrapLarkAppLink 把目标链接包装为飞书 AppLink「在飞书中打开网页」地址（独立窗口模式），
// 使卡片按钮在飞书内置浏览器中打开。目标链接整体作为 url 参数编码，其中的 #、&、? 不会
// 被误当作 AppLink 自身的片段或参数分隔符。
func wrapLarkAppLink(targetURL string) string {
	query := url.Values{}
	query.Set("mode", appLinkModeWindow)
	query.Set("url", targetURL)
	return larkWebURLAppLink + "?" + query.Encode()
}

// questionnaireEvaluators 是一份待发布问卷及其待评评价者（问卷内已去重）。
type questionnaireEvaluators struct {
	questionnaireID int64
	evaluators      []evaluator
}

// pushEvaluators 经 lark-push 投递模板卡片并按问卷判定结果：
//  1. 干跑：对全部问卷评价者并集只干跑一次（只寻址与构造卡片、不调飞书）。某问卷有评价者无工号
//     或在任何企业都不可投递 → 该问卷记入 Unreachable，避免发出卡片后才发现该问卷有人收不到。
//  2. 正式投递：只发给干跑通过问卷的评价者并集（每人一张卡；只出现在不可达问卷中的评价者不发卡），
//     单条失败不中断；某问卷有评价者在所有企业都未投递成功 → 该问卷记入 Failed（已送达的卡片
//     无法撤回）。没有评价者的问卷直接通过。
//
// lark-push 无法完成本次请求（未就绪、配置非法）时返回其错误，调用方不发布任何问卷。抽出此方法
// 使推送编排可在不触库的前提下单测：调用方直接传入合成的问卷评价者与 card，配合 fake Pusher
// 覆盖全部分支。
func (s *senderImpl) pushEvaluators(
	ctx context.Context,
	projectID int64,
	groups []questionnaireEvaluators,
	card cardTemplate,
) (project.PushOutcome, error) {
	outcome := project.PushOutcome{
		Unreachable: make(map[int64][]string),
		Failed:      make(map[int64][]string),
	}
	addressable, err := s.deliverToGroups(ctx, projectID, groups, card, true)
	if err != nil {
		return project.PushOutcome{}, err
	}
	passed := make([]questionnaireEvaluators, 0, len(groups))
	for _, g := range groups {
		if unreachable := labelsMissing(g.evaluators, addressable); len(unreachable) > 0 {
			outcome.Unreachable[g.questionnaireID] = unreachable
			continue
		}
		passed = append(passed, g)
	}

	reached, err := s.deliverToGroups(ctx, projectID, passed, card, false)
	if err != nil {
		return project.PushOutcome{}, err
	}
	for _, g := range passed {
		if failed := labelsMissing(g.evaluators, reached); len(failed) > 0 {
			outcome.Failed[g.questionnaireID] = failed
		}
	}
	if len(outcome.Unreachable) > 0 || len(outcome.Failed) > 0 {
		logger.Warningf(ctx, "eval-360 notify: questionnaires not published due to delivery project=%d unreachable=%v failed=%v",
			projectID, outcome.Unreachable, outcome.Failed)
	}
	return outcome, nil
}

// deliverToGroups 向各问卷评价者的并集（按工号去重，无工号者跳过）投递一次卡片，返回投递
// 通过的工号集合：dryRun 时为可投递（addressable）的工号，否则为任一企业投递成功的工号。
// 没有可投递收件人时不调用 lark-push。
func (s *senderImpl) deliverToGroups(
	ctx context.Context,
	projectID int64,
	groups []questionnaireEvaluators,
	card cardTemplate,
	dryRun bool,
) (map[string]bool, error) {
	recipients := make([]larkpushcap.Recipient, 0)
	nameByNo := make(map[string]string)
	for _, g := range groups {
		for _, ev := range g.evaluators {
			if ev.employeeNo == "" {
				continue // 无工号无法寻址，判定时计入不可达。
			}
			if _, ok := nameByNo[ev.employeeNo]; ok {
				continue // 多份问卷共享的评价者只发一张卡。
			}
			nameByNo[ev.employeeNo] = ev.name
			recipients = append(recipients, larkpushcap.Recipient{EmployeeNo: ev.employeeNo})
		}
	}
	passed := make(map[string]bool, len(recipients))
	if len(recipients) == 0 {
		return passed, nil
	}
	res, err := s.sendCards(ctx, projectID, recipients, card, nameByNo, dryRun)
	if err != nil {
		return nil, err
	}
	for _, d := range res.Deliveries {
		if dryRun && d.Status == larkpushcap.DeliveryStatusAddressable || !dryRun && d.Succeeded() {
			passed[d.EmployeeNo] = true
		}
	}
	return passed, nil
}

// labelsMissing 返回不在 ok 集合中的评价者标签（无工号者一律计入），保持入参顺序。
func labelsMissing(evaluators []evaluator, ok map[string]bool) []string {
	out := make([]string, 0)
	for _, ev := range evaluators {
		if ev.employeeNo != "" && ok[ev.employeeNo] {
			continue
		}
		out = append(out, ev.label())
	}
	return out
}

// sendCards 经 lark-push 投递一批卡片：每条「推送应用 × 评价者」由 buildCard 构造；dryRun 为
// true 时只寻址与构造、不调飞书。lark-push 未注入或未就绪时返回 CodeLarkPushUnbound，其余
// 请求级错误（如配置非法）原样透传。逐条明细中的失败由调用方按成功与否汇总。
func (s *senderImpl) sendCards(
	ctx context.Context,
	projectID int64,
	recipients []larkpushcap.Recipient,
	card cardTemplate,
	nameByNo map[string]string,
	dryRun bool,
) (larkpushcap.SendResult, error) {
	if s.pusher == nil {
		return larkpushcap.SendResult{}, bizerr.NewCode(notifycode.CodeLarkPushUnbound)
	}
	res, err := s.pusher.SendCard(ctx, larkpushcap.SendCardRequest{
		Group:      pushGroup,
		Recipients: recipients,
		Build: func(app larkpushcap.PushApp, r larkpushcap.Recipient) (string, error) {
			return buildCard(card, app, nameByNo[r.EmployeeNo])
		},
		BizSource: bizSource,
		BizRef:    strconv.FormatInt(projectID, 10),
		DryRun:    dryRun,
	})
	if errors.Is(err, larkpushcap.ErrSenderUnbound) {
		return larkpushcap.SendResult{}, bizerr.WrapCode(err, notifycode.CodeLarkPushUnbound)
	}
	return res, err
}

// buildCard 为某推送应用构造评价者的模板卡片：取该推送应用的卡片模板，按其所属企业的
// 身份应用生成带 larkAppId 的待办链接并包装为飞书 AppLink。员工端换票用的是身份应用，
// 故 larkAppId 取 IdentityAppID 而不是推送应用自身的 AppID。该推送应用未配模板时返回
// 错误（lark-push 记该条为失败；Precheck 正常情况下已在发布前拦截）。
func buildCard(card cardTemplate, app larkpushcap.PushApp, name string) (string, error) {
	tpl, ok := card.entryFor(app.AppID)
	if !ok {
		return "", gerror.Newf("eval-360 notify: 推送应用 %s 未配置卡片模板", app.AppID)
	}
	todoURL, err := withLarkAppID(card.todoURL, app.IdentityAppID)
	if err != nil {
		return "", err
	}
	return larkim.BuildTemplateCard(tpl.templateID, tpl.versionName, map[string]any{
		cardVarName:          name,
		cardVarEvaluationURL: wrapLarkAppLink(todoURL),
	})
}

// listPushApps 经 lark-push 取本插件分组的推送应用清单；lark-push 未注入或未就绪映射为
// CodeLarkPushUnbound，其余错误（如 lark-push 配置非法的业务错误）原样透传。
func (s *senderImpl) listPushApps(ctx context.Context) ([]larkpushcap.PushApp, error) {
	if s.pusher == nil {
		return nil, bizerr.NewCode(notifycode.CodeLarkPushUnbound)
	}
	apps, err := s.pusher.ListPushApps(ctx, pushGroup)
	if errors.Is(err, larkpushcap.ErrSenderUnbound) {
		return nil, bizerr.WrapCode(err, notifycode.CodeLarkPushUnbound)
	}
	return apps, err
}

// collectPendingEvaluators 一次查询返回各待发布问卷下未填评价关系的去重评价者 EmpID
// （问卷 id → EmpID，问卷内按查询顺序去重；无未填关系的问卷不出现在结果中）。
// 已填关系不推（评价者已无待办）。
func (s *senderImpl) collectPendingEvaluators(ctx context.Context, questionnaireIDs []int64) (map[int64][]int64, error) {
	cols := dao.Relation.Columns()
	var rows []struct {
		QuestionnaireId int64 `orm:"questionnaire_id"`
		EvaluatorEmpId  int64 `orm:"evaluator_emp_id"`
	}
	if err := dao.Relation.Ctx(ctx).
		WhereIn(cols.QuestionnaireId, questionnaireIDs).
		Where(cols.Completed, 0).
		Fields(cols.QuestionnaireId, cols.EvaluatorEmpId).
		Scan(&rows); err != nil {
		return nil, err
	}
	seen := make(map[[2]int64]struct{}, len(rows))
	out := make(map[int64][]int64)
	for _, r := range rows {
		key := [2]int64{r.QuestionnaireId, r.EvaluatorEmpId}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out[r.QuestionnaireId] = append(out[r.QuestionnaireId], r.EvaluatorEmpId)
	}
	return out, nil
}

// evaluator 是一个待推送评价者：EmpID + 工号（寻址键与回报标签）+ 姓名（卡片称呼）。
type evaluator struct {
	empID      int64
	employeeNo string
	name       string
}

// label 返回失败清单的回报标签：优先工号，工号为空时回退 EmpID 字符串（仍可定位人）。
func (e evaluator) label() string {
	if e.employeeNo != "" {
		return e.employeeNo
	}
	return strconv.FormatInt(e.empID, 10)
}

// resolveEvaluators 经 empcap 批量把 EmpID 解析为工号与姓名（一次查询，无 N+1），
// 返回 EmpID → 评价者。查不到的员工工号留空（label 回退 EmpID，干跑时计入不可达）。
func (s *senderImpl) resolveEvaluators(ctx context.Context, empIDs []int64) (map[int64]evaluator, error) {
	out := make(map[int64]evaluator, len(empIDs))
	if len(empIDs) == 0 {
		return out, nil
	}
	empMap, err := s.emp.MapByEmpIDs(ctx, empIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range empIDs {
		ev := evaluator{empID: id}
		if emp := empMap[id]; emp != nil {
			ev.employeeNo = emp.EmployeeNo
			ev.name = emp.Name
		}
		out[id] = ev
	}
	return out, nil
}
