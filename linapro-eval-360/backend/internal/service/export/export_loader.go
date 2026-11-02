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
	dimensions  []*dimensionBundle
	submissions []*entitymodel.Submission                 // 只含已提交
	relations   map[int64]*entitymodel.Relation           // relation_id → relation
	roleNames   map[int64]string                          // role_id → name
	evalNames   map[int64]string                          // evaluatee_id → 员工姓名
	answers     map[int64]map[int64][]*entitymodel.Answer // submission_id → question_id → answers
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

	// 被评估人姓名映射（evaluatee.id → 员工姓名）。
	evalNames, err := s.loadEvaluateeNames(ctx, questionnaireID, rels)
	if err != nil {
		return nil, err
	}

	return &exportBundle{
		dimensions:  dimBundles,
		submissions: subs,
		relations:   rels,
		roleNames:   roleNames,
		evalNames:   evalNames,
		answers:     answersBySub,
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

// loadEvaluateeNames 装载 evaluatee.id → 员工姓名 映射。
func (s *serviceImpl) loadEvaluateeNames(ctx context.Context, questionnaireID int64, rels map[int64]*entitymodel.Relation) (map[int64]string, error) {
	evalIDSet := make(map[int64]struct{})
	for _, r := range rels {
		evalIDSet[r.EvaluateeId] = struct{}{}
	}
	if len(evalIDSet) == 0 {
		return map[int64]string{}, nil
	}
	evalIDs := make([]int64, 0, len(evalIDSet))
	for id := range evalIDSet {
		evalIDs = append(evalIDs, id)
	}
	var ees []*entitymodel.Evaluatee
	if err := dao.Evaluatee.Ctx(ctx).
		WhereIn(dao.Evaluatee.Columns().Id, evalIDs).Scan(&ees); err != nil {
		return nil, err
	}
	// evaluatee.id → emp_id，再批量取姓名（一次查询，无 N+1）。
	empIDSet := make(map[int64]struct{}, len(ees))
	for _, e := range ees {
		empIDSet[e.EvaluateeEmpId] = struct{}{}
	}
	empNames := s.batchEmployeeNames(ctx, empIDSet)
	out := make(map[int64]string, len(ees))
	for _, e := range ees {
		out[e.Id] = empNames[e.EvaluateeEmpId]
	}
	return out, nil
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
