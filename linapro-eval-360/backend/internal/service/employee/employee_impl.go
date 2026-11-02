// employee_impl.go 实现我的待办（按问卷聚合）、按问卷取批量题集（逐关系可见性）、批量提交与规范化存储。
// 数据库访问均为集合化查询，次数与被评估人数、题目数无关（提交时 submission 按被评估人逐条插入以取回 id）。

package employee

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// empSvc 返回 empcap 绑定的员工服务。
func (s *serviceImpl) empSvc() empcap.Service {
	return empcap.BoundEmployeeService()
}

// currentEmp 从 ctx 取 EmpContext，未注入返回 ErrUnauthorized。
func (s *serviceImpl) currentEmp(ctx context.Context) (empcap.EmpContext, error) {
	emp, ok := empcap.EmpFromContext(ctx)
	if !ok {
		return emp, bizerr.NewCode(empcap.CodeUnauthorized)
	}
	return emp, nil
}

// ListTodo 返回当前员工的待办：按问卷聚合，一个评价者 + 一份已发布问卷 = 一条，完成度 X/Y 人。
// 派生状态：名下全部已提交 = filled；否则项目已截止 = voided；否则 pending。
// 排序：待填写在前、已完成次之、已作废最后；同状态按问卷 id 倒序（新问卷在前）。
func (s *serviceImpl) ListTodo(ctx context.Context) ([]*TodoItem, error) {
	emp, err := s.currentEmp(ctx)
	if err != nil {
		return nil, err
	}
	var rels []*entitymodel.Relation
	if err = dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().EvaluatorEmpId, emp.EmpID).Scan(&rels); err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return []*TodoItem{}, nil
	}

	// 按问卷聚合关系数与已提交数，同时收集问卷/项目 id（批量查询，无 N+1）。
	type agg struct {
		projectID int64
		total     int
		completed int
	}
	byQ := make(map[int64]*agg)
	projectIDs := make(map[int64]struct{})
	for _, r := range rels {
		a := byQ[r.QuestionnaireId]
		if a == nil {
			a = &agg{projectID: r.ProjectId}
			byQ[r.QuestionnaireId] = a
		}
		a.total++
		if r.Completed == 1 {
			a.completed++
		}
		projectIDs[r.ProjectId] = struct{}{}
	}
	qIDs := make([]int64, 0, len(byQ))
	for id := range byQ {
		qIDs = append(qIDs, id)
	}

	// 只取已发布问卷（未发布对评价者不可见）。
	var qs []*entitymodel.Questionnaire
	if err = dao.Questionnaire.Ctx(ctx).
		WhereIn(dao.Questionnaire.Columns().Id, qIDs).
		Where(dao.Questionnaire.Columns().Published, questionnaire.PublishedYes).Scan(&qs); err != nil {
		return nil, err
	}
	var projects []*entitymodel.Project
	if err = dao.Project.Ctx(ctx).
		WhereIn(dao.Project.Columns().Id, keys(projectIDs)).Scan(&projects); err != nil {
		return nil, err
	}
	projByID := make(map[int64]*entitymodel.Project, len(projects))
	for _, p := range projects {
		projByID[p.Id] = p
	}

	// 名下被评估人明细只取已发布问卷的关系（批量取姓名/职务/部门/角色）。
	published := make(map[int64]struct{}, len(qs))
	for _, q := range qs {
		published[q.Id] = struct{}{}
	}
	pubRels := make([]*entitymodel.Relation, 0, len(rels))
	for _, r := range rels {
		if _, ok := published[r.QuestionnaireId]; ok {
			pubRels = append(pubRels, r)
		}
	}
	sort.SliceStable(pubRels, func(i, j int) bool { return pubRels[i].Id < pubRels[j].Id })
	labels, err := s.relationLabels(ctx, pubRels)
	if err != nil {
		return nil, err
	}
	eesByQ := make(map[int64][]*TodoEvaluatee, len(qs))
	for _, r := range pubRels {
		lb := labels[r.Id]
		eesByQ[r.QuestionnaireId] = append(eesByQ[r.QuestionnaireId], &TodoEvaluatee{
			RelationID:    r.Id,
			EvaluateeName: lb.evaluateeName,
			JobTitle:      lb.jobTitle,
			Department:    lb.department,
			RoleName:      lb.roleName,
			Submitted:     r.Completed == 1,
		})
	}

	now := time.Now()
	out := make([]*TodoItem, 0, len(qs))
	for _, q := range qs {
		a := byQ[q.Id]
		p := projByID[a.projectID]
		var startAt, endAt *time.Time
		if p != nil {
			startAt, endAt = p.StartAt, p.EndAt
		}
		out = append(out, &TodoItem{
			QuestionnaireID:    q.Id,
			ProjectID:          a.projectID,
			ProjectName:        projectName(p),
			QuestionnaireTitle: q.Title,
			StartAt:            startAt,
			EndAt:              endAt,
			Total:              a.total,
			CompletedCount:     a.completed,
			DerivedStatus:      deriveStatus(a.completed, a.total, isProjectClosed(p, now)),
			Evaluatees:         eesByQ[q.Id],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := statusRank(out[i].DerivedStatus), statusRank(out[j].DerivedStatus)
		if ri != rj {
			return ri < rj
		}
		return out[i].QuestionnaireID > out[j].QuestionnaireID
	})
	return out, nil
}

// GetQuestionnaireSet 按问卷取批量题集。
//   - 问卷须已发布，且当前员工在该问卷下至少有一条关系；
//   - 每题逐关系判可见性：维1 角色挂在关系上（同一评价者对不同被评估人角色可不同），
//     维2 受限评价者名单按评价者本人全局生效（对其名下所有关系结论一致，被排除则整题隐藏）；
//     得到该题要渲染的被评估人行；对名下所有人都不可见的题、空维度直接不返回；
//   - 已提交关系的答案批量回读，供前端只读回显。
func (s *serviceImpl) GetQuestionnaireSet(ctx context.Context, questionnaireID int64) (*QuestionnaireSet, error) {
	emp, err := s.currentEmp(ctx)
	if err != nil {
		return nil, err
	}
	owned, err := s.loadOwned(ctx, emp.EmpID, questionnaireID)
	if err != nil {
		return nil, err
	}
	detail, err := s.questionnaire.GetDetail(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}
	subs, err := submissionsByRelation(ctx, relationIDs(owned.rels))
	if err != nil {
		return nil, err
	}
	labels, err := s.relationLabels(ctx, owned.rels)
	if err != nil {
		return nil, err
	}

	evaluatees := make([]*EvaluateeRow, 0, len(owned.rels))
	for _, r := range owned.rels {
		lb := labels[r.Id]
		evaluatees = append(evaluatees, &EvaluateeRow{
			RelationID:    r.Id,
			EvaluateeName: lb.evaluateeName,
			RoleName:      lb.roleName,
			Submitted:     subs[r.Id] != nil,
		})
	}

	dims := make([]*SetDimension, 0, len(detail.Dimensions))
	for _, d := range detail.Dimensions {
		qs := make([]*SetQuestion, 0, len(d.Questions))
		for _, qq := range d.Questions {
			rows := make([]int64, 0, len(owned.rels))
			for _, r := range owned.rels {
				if questionnaire.IsQuestionVisible(r.RoleId, r.EvaluatorEmpId, qq.VisibleRoleIds, qq.ExcludedEvaluatorEmpIds) {
					rows = append(rows, r.Id)
				}
			}
			if len(rows) == 0 {
				continue // 对名下所有人都不可见 → 整题隐藏。
			}
			qs = append(qs, &SetQuestion{Node: qq, RowRelationIDs: rows})
		}
		if len(qs) == 0 {
			continue
		}
		dims = append(dims, &SetDimension{Id: d.Id, Name: d.Name, Sort: d.Sort, Questions: qs})
	}

	answers, err := submittedAnswers(ctx, subs)
	if err != nil {
		return nil, err
	}
	return &QuestionnaireSet{
		QuestionnaireID:    questionnaireID,
		QuestionnaireTitle: owned.questionnaire.Title,
		Evaluatees:         evaluatees,
		Dimensions:         dims,
		SubmittedAnswers:   answers,
		Closed:             isProjectClosed(owned.project, time.Now()),
	}, nil
}

// SubmitBatch 批量提交页面上已展示的未提交被评估人。
//  1. 已发布、时间窗校验一次；
//  2. 提交集合校验（checkSubmitSet）：全部已提交 = 幂等；部分已提交 = 拒绝；
//  3. 逐关系用其可见题集合校验答案与必填，任一失败整体拒绝（错误带被评估人姓名）；
//  4. 单事务逐关系插 submission（同一 duration_seconds、同一 submitted_at），答案（row_id=0）与关系完成态各批量写一次；
//  5. 唯一索引冲突（并发）→ 回滚后重查，按提交集合规则归类为幂等 / 部分已提交 / 原样报错。
func (s *serviceImpl) SubmitBatch(ctx context.Context, in SubmitBatchInput) (*SubmitBatchResult, error) {
	emp, err := s.currentEmp(ctx)
	if err != nil {
		return nil, err
	}
	owned, err := s.loadOwned(ctx, emp.EmpID, in.QuestionnaireID)
	if err != nil {
		return nil, err
	}
	if isProjectClosed(owned.project, time.Now()) {
		return nil, bizerr.NewCode(CodeProjectClosed)
	}

	requested := make([]int64, 0, len(in.Items))
	for _, it := range in.Items {
		requested = append(requested, it.RelationID)
	}
	subs, err := submissionsByRelation(ctx, relationIDs(owned.rels))
	if err != nil {
		return nil, err
	}
	outcome, err := checkSubmitSet(submittedState(owned.rels, subs), requested)
	if err != nil {
		return nil, err
	}
	if outcome == submitSetIdempotent {
		return idempotentResult(subs, requested), nil
	}

	// 逐关系按其可见题集合校验答案（直读 live 结构）。
	detail, err := s.questionnaire.GetDetail(ctx, in.QuestionnaireID)
	if err != nil {
		return nil, err
	}
	labels, err := s.relationLabels(ctx, owned.rels)
	if err != nil {
		return nil, err
	}
	relByID := make(map[int64]*entitymodel.Relation, len(owned.rels))
	for _, r := range owned.rels {
		relByID[r.Id] = r
	}
	for _, it := range in.Items {
		rel := relByID[it.RelationID]
		visible := make(map[int64]*questionnaire.QuestionNode)
		for _, d := range detail.Dimensions {
			for _, qq := range d.Questions {
				if questionnaire.IsQuestionVisible(rel.RoleId, rel.EvaluatorEmpId, qq.VisibleRoleIds, qq.ExcludedEvaluatorEmpIds) {
					visible[qq.Id] = qq
				}
			}
		}
		if err = validateAnswers(visible, it.Answers, evaluateeLabel(labels[rel.Id].evaluateeName, rel.Id)); err != nil {
			return nil, err
		}
	}

	// 单事务全成全败：逐关系插 submission（需取回 id）；答案与关系完成态各一次批量写入。
	now := time.Now()
	err = dao.Submission.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		answerRows := make([]do.Answer, 0)
		for _, it := range in.Items {
			rel := relByID[it.RelationID]
			submissionID, insErr := dao.Submission.Ctx(ctx).Data(do.Submission{
				RelationId:      rel.Id,
				QuestionnaireId: rel.QuestionnaireId,
				EvaluateeId:     rel.EvaluateeId,
				EvaluatorEmpId:  emp.EmpID,
				DurationSeconds: in.DurationSeconds,
				SubmittedAt:     &now,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			for _, a := range it.Answers {
				answerRows = append(answerRows, do.Answer{
					SubmissionId:    submissionID,
					QuestionnaireId: rel.QuestionnaireId,
					QuestionId:      a.QuestionID,
					RowId:           0, // 矩阵行由被评估人承载，row_id 恒 0。
					OptionId:        a.OptionID,
					TextValue:       a.TextValue,
				})
			}
		}
		if len(answerRows) > 0 {
			if _, aErr := dao.Answer.Ctx(ctx).Data(answerRows).Insert(); aErr != nil {
				return aErr
			}
		}
		if _, uErr := dao.Relation.Ctx(ctx).
			WhereIn(dao.Relation.Columns().Id, requested).
			Data(do.Relation{Completed: 1, CompletedAt: &now}).Update(); uErr != nil {
			return uErr
		}
		return nil
	})
	if err != nil {
		if !isUniqueViolation(err) {
			return nil, err
		}
		// 唯一索引冲突 = 并发提交：事务已回滚，重查提交状态后按提交集合规则归类。
		again, qErr := submissionsByRelation(ctx, relationIDs(owned.rels))
		if qErr != nil {
			return nil, err
		}
		againOutcome, cErr := checkSubmitSet(submittedState(owned.rels, again), requested)
		if cErr != nil {
			return nil, cErr
		}
		if againOutcome == submitSetIdempotent {
			return idempotentResult(again, requested), nil
		}
		return nil, err
	}
	return &SubmitBatchResult{SubmittedCount: len(in.Items), SubmittedAt: &now, Idempotent: false}, nil
}

// ownedSet 是当前评价者在某问卷下的作答上下文。
type ownedSet struct {
	questionnaire *entitymodel.Questionnaire
	project       *entitymodel.Project
	rels          []*entitymodel.Relation // 按关系 id 升序（被评估人行顺序）
}

// loadOwned 读取问卷（须已发布）、所属项目与当前评价者在该问卷下的全部关系（至少一条）。
func (s *serviceImpl) loadOwned(ctx context.Context, empID, questionnaireID int64) (*ownedSet, error) {
	var q *entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, questionnaireID).Scan(&q); err != nil {
		return nil, err
	}
	if q == nil || q.Published != questionnaire.PublishedYes {
		return nil, bizerr.NewCode(CodeQuestionnaireNotFound)
	}
	var rels []*entitymodel.Relation
	if err := dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).
		Where(dao.Relation.Columns().EvaluatorEmpId, empID).
		OrderAsc(dao.Relation.Columns().Id).Scan(&rels); err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return nil, bizerr.NewCode(CodeRelationNotFound)
	}
	var p *entitymodel.Project
	if err := dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, q.ProjectId).Scan(&p); err != nil {
		return nil, err
	}
	return &ownedSet{questionnaire: q, project: p, rels: rels}, nil
}

// relationLabel 是关系的展示信息。
type relationLabel struct {
	evaluateeName string
	jobTitle      string
	department    string
	roleName      string
}

// relationLabels 批量取关系 → 被评估人姓名 / 职务 / 部门 / 角色名（各一次查询，无 N+1）。
func (s *serviceImpl) relationLabels(ctx context.Context, rels []*entitymodel.Relation) (map[int64]relationLabel, error) {
	evalIDs := make(map[int64]struct{}, len(rels))
	roleIDs := make(map[int64]struct{}, len(rels))
	for _, r := range rels {
		evalIDs[r.EvaluateeId] = struct{}{}
		roleIDs[r.RoleId] = struct{}{}
	}
	var ees []*entitymodel.Evaluatee
	if err := dao.Evaluatee.Ctx(ctx).
		WhereIn(dao.Evaluatee.Columns().Id, keys(evalIDs)).Scan(&ees); err != nil {
		return nil, err
	}
	eeByID := make(map[int64]*entitymodel.Evaluatee, len(ees))
	empIDSet := make(map[int64]struct{}, len(ees))
	for _, e := range ees {
		eeByID[e.Id] = e
		empIDSet[e.EvaluateeEmpId] = struct{}{}
	}
	empNames := s.batchEmployeeNames(ctx, empIDSet)
	var roles []*entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		WhereIn(dao.Role.Columns().Id, keys(roleIDs)).Scan(&roles); err != nil {
		return nil, err
	}
	roleByID := make(map[int64]*entitymodel.Role, len(roles))
	for _, r := range roles {
		roleByID[r.Id] = r
	}
	out := make(map[int64]relationLabel, len(rels))
	for _, r := range rels {
		lb := relationLabel{roleName: roleName(roleByID[r.RoleId])}
		if e := eeByID[r.EvaluateeId]; e != nil {
			lb.evaluateeName = empNames[e.EvaluateeEmpId]
			lb.jobTitle = e.JobTitle
			lb.department = e.Department
		}
		out[r.Id] = lb
	}
	return out, nil
}

// submissionsByRelation 批量取关系 → 提交记录（relation_id 唯一，一关系至多一条）。
func submissionsByRelation(ctx context.Context, relIDs []int64) (map[int64]*entitymodel.Submission, error) {
	out := make(map[int64]*entitymodel.Submission, len(relIDs))
	if len(relIDs) == 0 {
		return out, nil
	}
	var subs []*entitymodel.Submission
	if err := dao.Submission.Ctx(ctx).
		WhereIn(dao.Submission.Columns().RelationId, relIDs).Scan(&subs); err != nil {
		return nil, err
	}
	for _, sub := range subs {
		out[sub.RelationId] = sub
	}
	return out, nil
}

// submittedAnswers 批量回读已提交关系的答案（一次查询），供只读回显。
func submittedAnswers(ctx context.Context, subs map[int64]*entitymodel.Submission) ([]*SubmittedAnswer, error) {
	out := make([]*SubmittedAnswer, 0)
	if len(subs) == 0 {
		return out, nil
	}
	relBySub := make(map[int64]int64, len(subs))
	subIDs := make([]int64, 0, len(subs))
	for relID, sub := range subs {
		relBySub[sub.Id] = relID
		subIDs = append(subIDs, sub.Id)
	}
	var rows []*entitymodel.Answer
	if err := dao.Answer.Ctx(ctx).
		WhereIn(dao.Answer.Columns().SubmissionId, subIDs).
		OrderAsc(dao.Answer.Columns().Id).Scan(&rows); err != nil {
		return nil, err
	}
	for _, a := range rows {
		out = append(out, &SubmittedAnswer{
			RelationID: relBySub[a.SubmissionId],
			QuestionID: a.QuestionId,
			OptionID:   a.OptionId,
			TextValue:  a.TextValue,
		})
	}
	return out, nil
}

// submittedState 把关系列表 + 提交记录投影为「关系 id → 是否已提交」，供 checkSubmitSet 使用。
func submittedState(rels []*entitymodel.Relation, subs map[int64]*entitymodel.Submission) map[int64]bool {
	out := make(map[int64]bool, len(rels))
	for _, r := range rels {
		out[r.Id] = subs[r.Id] != nil
	}
	return out
}

// idempotentResult 构造幂等返回：提交时间取请求内被评估人中最早的一次提交。
func idempotentResult(subs map[int64]*entitymodel.Submission, requested []int64) *SubmitBatchResult {
	var first *time.Time
	for _, id := range requested {
		sub := subs[id]
		if sub == nil || sub.SubmittedAt == nil {
			continue
		}
		if first == nil || sub.SubmittedAt.Before(*first) {
			first = sub.SubmittedAt
		}
	}
	return &SubmitBatchResult{SubmittedCount: len(requested), SubmittedAt: first, Idempotent: true}
}

// relationIDs 提取关系 id 切片。
func relationIDs(rels []*entitymodel.Relation) []int64 {
	out := make([]int64, 0, len(rels))
	for _, r := range rels {
		out = append(out, r.Id)
	}
	return out
}

// batchEmployeeNames 经 empcap 批量取 EmpID → 姓名（一次查询，无 N+1）。
// empcap 未就绪时返回空 map（姓名列留空，不阻断列表）。
func (s *serviceImpl) batchEmployeeNames(ctx context.Context, empIDs map[int64]struct{}) map[int64]string {
	out := make(map[int64]string, len(empIDs))
	empSvc := s.empSvc()
	if empSvc == nil {
		return out
	}
	if len(empIDs) == 0 {
		return out
	}
	ids := make([]int64, 0, len(empIDs))
	for id := range empIDs {
		ids = append(ids, id)
	}
	empMap, err := empSvc.MapByEmpIDs(ctx, ids)
	if err != nil {
		return out
	}
	for id, emp := range empMap {
		if emp != nil {
			out[id] = emp.Name
		}
	}
	return out
}

// isProjectClosed 判定项目是否已截止（读时派生）。
func isProjectClosed(p *entitymodel.Project, now time.Time) bool {
	if p == nil {
		return false
	}
	if p.ClosedAt != nil {
		return true
	}
	if p.EndAt == nil {
		return false
	}
	return p.EndAt.Before(now)
}

// deriveStatus 派生问卷级待办状态：全部已提交 = filled；否则已截止 = voided；否则 pending。
func deriveStatus(completed, total int, projectClosed bool) DerivedStatus {
	if total > 0 && completed >= total {
		return DerivedStatusFilled
	}
	if projectClosed {
		return DerivedStatusVoided
	}
	return DerivedStatusPending
}

// statusRank 是待办排序权重：待填写 < 已完成 < 已作废。
func statusRank(status DerivedStatus) int {
	switch status {
	case DerivedStatusPending:
		return 0
	case DerivedStatusFilled:
		return 1
	default:
		return 2
	}
}

// projectName 安全取项目名称。
func projectName(p *entitymodel.Project) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// roleName 安全取角色名。
func roleName(r *entitymodel.Role) string {
	if r == nil {
		return ""
	}
	return r.Name
}

// keys 提取 map 的键切片。
func keys(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// isUniqueViolation 判定错误是否为唯一索引冲突（幂等兜底）。
// GoFrame 把唯一冲突归为 CodeDbOperationError，错误信息含 "duplicate key" / "unique constraint"。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
