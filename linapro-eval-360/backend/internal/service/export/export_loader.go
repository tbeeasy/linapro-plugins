// export_loader.go 实现导出数据装载：问卷 live 结构 + 已提交作答 + 关系/角色/员工姓名。
package export

import (
	"context"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
)

// exportBundle 是一次导出所需的全部数据（直读 live 结构，无快照）。
type exportBundle struct {
	dimensions     []*dimensionBundle
	submissions    []*entitymodel.Submission                 // 只含已提交
	relations      map[int64]*entitymodel.Relation           // relation_id → relation
	roleNames      map[int64]string                          // role_id → name
	evalInfos      map[int64]*evaluateeInfo                  // evaluatee_id → 被评估人姓名/职务/部门/项目
	evaluatorNames map[int64]string                          // evaluator_emp_id → 评价者姓名
	answers        map[int64]map[int64][]*entitymodel.Answer // submission_id → question_id → answers
}

// evaluateeInfo 是被评估人在导出前导列中的展示信息（姓名 + 问卷级档案字段）。
type evaluateeInfo struct {
	name        string
	jobTitle    string
	department  string
	workProject string
}

// dimensionBundle 是维度 + 其下题目。
type dimensionBundle struct {
	id        int64
	name      string
	sort      int
	questions []*questionBundle
}

// questionBundle 是题目 + 其从属结构（单选选项 / 矩阵列）。矩阵行即被评估人，不另存结构。
type questionBundle struct {
	Id            int64
	DimensionId   int64
	Type          string
	Title         string
	Required      int
	Sort          int
	options       []*entitymodel.QuestionOption
	matrixColumns []*entitymodel.MatrixColumn
}

// loadExportBundle 装载某问卷的导出数据（live 结构 + 已提交作答 + 关系/角色/员工）。
func (s *serviceImpl) loadExportBundle(ctx context.Context, questionnaireID int64) (*exportBundle, error) {
	// 问卷存在性校验（租户过滤）。
	var q *entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, questionnaireID).Scan(&q); err != nil {
		return nil, err
	}
	if q == nil {
		return nil, bizerr.NewCode(CodeQuestionnaireNotFound)
	}

	// 维度（按 sort、id 升序）。
	var dims []*entitymodel.Dimension
	if err := dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Dimension.Columns().Sort).OrderAsc(dao.Dimension.Columns().Id).
		Scan(&dims); err != nil {
		return nil, err
	}

	// 题目（按 sort、id 升序）。
	var questions []*entitymodel.Question
	if err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Question.Columns().Sort).OrderAsc(dao.Question.Columns().Id).
		Scan(&questions); err != nil {
		return nil, err
	}
	questionIDs := make([]int64, 0, len(questions))
	for _, qq := range questions {
		questionIDs = append(questionIDs, qq.Id)
	}

	// 从属结构。
	var (
		options []*entitymodel.QuestionOption
		mCols   []*entitymodel.MatrixColumn
	)
	if len(questionIDs) > 0 {
		if err := dao.QuestionOption.Ctx(ctx).
			WhereIn(dao.QuestionOption.Columns().QuestionId, questionIDs).
			OrderAsc(dao.QuestionOption.Columns().Sort).OrderAsc(dao.QuestionOption.Columns().Id).
			Scan(&options); err != nil {
			return nil, err
		}
		if err := dao.MatrixColumn.Ctx(ctx).
			WhereIn(dao.MatrixColumn.Columns().QuestionId, questionIDs).
			OrderAsc(dao.MatrixColumn.Columns().Sort).OrderAsc(dao.MatrixColumn.Columns().Id).
			Scan(&mCols); err != nil {
			return nil, err
		}
	}

	// 索引：question_id → 从属结构。
	optsByQ := make(map[int64][]*entitymodel.QuestionOption)
	for _, o := range options {
		optsByQ[o.QuestionId] = append(optsByQ[o.QuestionId], o)
	}
	colsByQ := make(map[int64][]*entitymodel.MatrixColumn)
	for _, c := range mCols {
		colsByQ[c.QuestionId] = append(colsByQ[c.QuestionId], c)
	}

	// 组装维度→题目树。
	questionsByDim := make(map[int64][]*questionBundle)
	for _, qq := range questions {
		qb := &questionBundle{
			Id: qq.Id, DimensionId: qq.DimensionId, Type: qq.Type, Title: qq.Title,
			Required: qq.Required, Sort: qq.Sort,
			options: optsByQ[qq.Id], matrixColumns: colsByQ[qq.Id],
		}
		questionsByDim[qq.DimensionId] = append(questionsByDim[qq.DimensionId], qb)
	}
	dimBundles := make([]*dimensionBundle, 0, len(dims))
	for _, d := range dims {
		qs := questionsByDim[d.Id]
		if qs == nil {
			qs = []*questionBundle{}
		}
		dimBundles = append(dimBundles, &dimensionBundle{id: d.Id, name: d.Name, sort: d.Sort, questions: qs})
	}

	// 已提交作答（submission）。
	subs, rels, err := s.loadSubmissionsAndRelations(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}

	// 答案（按 submission_id → question_id 分组）。
	answersBySub, err := s.loadAnswers(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}

	// 角色名映射。
	roleNames, err := s.loadRoleNames(ctx, questionnaireID, rels)
	if err != nil {
		return nil, err
	}

	// 被评估人信息（evaluatee.id → 姓名/职务/部门/项目）与评价人姓名映射。
	// 两者共用一次 empcap 批量查询，避免逐行取姓名。
	evalInfos, evaluatorNames, err := s.loadEvaluateeInfos(ctx, rels)
	if err != nil {
		return nil, err
	}

	return &exportBundle{
		dimensions:     dimBundles,
		submissions:    subs,
		relations:      rels,
		roleNames:      roleNames,
		evalInfos:      evalInfos,
		evaluatorNames: evaluatorNames,
		answers:        answersBySub,
	}, nil
}

// loadSubmissionsAndRelations 装载已提交作答及其评价关系。
func (s *serviceImpl) loadSubmissionsAndRelations(ctx context.Context, questionnaireID int64) ([]*entitymodel.Submission, map[int64]*entitymodel.Relation, error) {
	var subs []*entitymodel.Submission
	if err := dao.Submission.Ctx(ctx).
		Where(dao.Submission.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Submission.Columns().SubmittedAt).OrderAsc(dao.Submission.Columns().Id).
		Scan(&subs); err != nil {
		return nil, nil, err
	}
	if len(subs) == 0 {
		return subs, map[int64]*entitymodel.Relation{}, nil
	}
	relIDs := make([]int64, 0, len(subs))
	for _, sub := range subs {
		relIDs = append(relIDs, sub.RelationId)
	}
	var rels []*entitymodel.Relation
	if err := dao.Relation.Ctx(ctx).
		WhereIn(dao.Relation.Columns().Id, relIDs).Scan(&rels); err != nil {
		return nil, nil, err
	}
	relMap := make(map[int64]*entitymodel.Relation, len(rels))
	for _, r := range rels {
		relMap[r.Id] = r
	}
	return subs, relMap, nil
}

// loadAnswers 装载问卷全部答案，按 submission_id → question_id 分组。
func (s *serviceImpl) loadAnswers(ctx context.Context, questionnaireID int64) (map[int64]map[int64][]*entitymodel.Answer, error) {
	var answers []*entitymodel.Answer
	if err := dao.Answer.Ctx(ctx).
		Where(dao.Answer.Columns().QuestionnaireId, questionnaireID).Scan(&answers); err != nil {
		return nil, err
	}
	out := make(map[int64]map[int64][]*entitymodel.Answer)
	for _, a := range answers {
		if a == nil {
			continue
		}
		byQ, ok := out[a.SubmissionId]
		if !ok {
			byQ = make(map[int64][]*entitymodel.Answer)
			out[a.SubmissionId] = byQ
		}
		byQ[a.QuestionId] = append(byQ[a.QuestionId], a)
	}
	return out, nil
}

// loadRoleNames 装载 role_id → name 映射（仅取本问卷用到的角色）。
func (s *serviceImpl) loadRoleNames(ctx context.Context, questionnaireID int64, rels map[int64]*entitymodel.Relation) (map[int64]string, error) {
	roleIDSet := make(map[int64]struct{})
	for _, r := range rels {
		roleIDSet[r.RoleId] = struct{}{}
	}
	if len(roleIDSet) == 0 {
		return map[int64]string{}, nil
	}
	roleIDs := make([]int64, 0, len(roleIDSet))
	for id := range roleIDSet {
		roleIDs = append(roleIDs, id)
	}
	var roles []*entitymodel.Role
	if err := dao.Role.Ctx(ctx).
		WhereIn(dao.Role.Columns().Id, roleIDs).Scan(&roles); err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(roles))
	for _, r := range roles {
		out[r.Id] = r.Name
	}
	return out, nil
}

// loadEvaluateeInfos 装载被评估人展示信息与评价人姓名。
// 返回 evaluatee.id → 被评估人信息、evaluator_emp_id → 评价者姓名；被评估人姓名与评价者姓名
// 走同一次 empcap 批量查询，评价行数增长不增加查询次数。
func (s *serviceImpl) loadEvaluateeInfos(ctx context.Context, rels map[int64]*entitymodel.Relation) (map[int64]*evaluateeInfo, map[int64]string, error) {
	evalIDSet := make(map[int64]struct{})
	evaluatorEmpIDs := make(map[int64]struct{})
	for _, r := range rels {
		evalIDSet[r.EvaluateeId] = struct{}{}
		evaluatorEmpIDs[r.EvaluatorEmpId] = struct{}{}
	}
	if len(evalIDSet) == 0 {
		return map[int64]*evaluateeInfo{}, map[int64]string{}, nil
	}
	evalIDs := make([]int64, 0, len(evalIDSet))
	for id := range evalIDSet {
		evalIDs = append(evalIDs, id)
	}
	var ees []*entitymodel.Evaluatee
	if err := dao.Evaluatee.Ctx(ctx).
		WhereIn(dao.Evaluatee.Columns().Id, evalIDs).Scan(&ees); err != nil {
		return nil, nil, err
	}
	// 被评估人与评价者的 EmpID 合并后一次批量取姓名。
	empIDSet := make(map[int64]struct{}, len(ees)+len(evaluatorEmpIDs))
	for id := range evaluatorEmpIDs {
		empIDSet[id] = struct{}{}
	}
	for _, e := range ees {
		empIDSet[e.EvaluateeEmpId] = struct{}{}
	}
	empNames := s.batchEmployeeNames(ctx, empIDSet)

	infos := make(map[int64]*evaluateeInfo, len(ees))
	for _, e := range ees {
		infos[e.Id] = &evaluateeInfo{
			name:        empNames[e.EvaluateeEmpId],
			jobTitle:    e.JobTitle,
			department:  e.Department,
			workProject: e.WorkProject,
		}
	}
	evaluatorNames := make(map[int64]string, len(evaluatorEmpIDs))
	for id := range evaluatorEmpIDs {
		evaluatorNames[id] = empNames[id]
	}
	return infos, evaluatorNames, nil
}

// batchEmployeeNames 经 empcap 批量取 EmpID → 姓名（一次查询，无 N+1）。
// empcap 未就绪时返回空 map（姓名列留空）。
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
