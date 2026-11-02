// editor_impl.go 实现关系编辑器：明细列表、员工搜索、按姓名批量新增/改派/删除评价关系。
//
// 发布后的关系调整只能经此编辑器进行（不得再全量导入）。
// 删除关系在已有作答时拒绝，引导二次确认/清空作答（策略②）。
package relation

import (
	"context"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/textutil"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// ListRelations 返回问卷下「被评估人 × 角色 × 评价者（已评/未评）」明细。
// 已作废状态（未填且项目已截止）由 project 的 end_at/closed_at 读时派生（不落库、无 cron）。
func (s *serviceImpl) ListRelations(ctx context.Context, questionnaireID int64) (*RelationMatrix, error) {
	q, err := s.mustGetQuestionnaire(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}

	// 截止状态（读时派生）：项目 end_at/closed_at。
	projectClosed, err := s.isProjectClosed(ctx, q.ProjectId)
	if err != nil {
		return nil, err
	}

	// 被评估人列表。
	var evaluatees []*entitymodel.Evaluatee
	if err = dao.Evaluatee.Ctx(ctx).
		Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Evaluatee.Columns().Id).Scan(&evaluatees); err != nil {
		return nil, err
	}
	evalByID := make(map[int64]*entitymodel.Evaluatee, len(evaluatees))
	empIDSet := make(map[int64]struct{})
	for _, e := range evaluatees {
		evalByID[e.Id] = e
		empIDSet[e.EvaluateeEmpId] = struct{}{}
	}

	// 评价关系列表。
	var relations []*entitymodel.Relation
	if err = dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Relation.Columns().EvaluateeId).
		OrderAsc(dao.Relation.Columns().Id).Scan(&relations); err != nil {
		return nil, err
	}
	for _, r := range relations {
		empIDSet[r.EvaluatorEmpId] = struct{}{}
	}

	// 角色列表（按 id 升序，决定展示顺序）。
	var roles []*entitymodel.Role
	if err = dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Role.Columns().Id).Scan(&roles); err != nil {
		return nil, err
	}
	roleByID := make(map[int64]*entitymodel.Role, len(roles))
	roleItems := make([]*RoleItem, 0, len(roles))
	for _, r := range roles {
		roleByID[r.Id] = r
		roleItems = append(roleItems, &RoleItem{Id: r.Id, Name: r.Name, Weight: decimalString(r.Weight)})
	}

	// 经 empcap 批量取姓名/工号（未就绪时返回空 map，姓名/工号列留空，不阻断列表）。
	empMap := s.batchEmployees(ctx, empIDSet)

	// 组装：被评估人 → 角色 → 评价者。
	groupsByEval := make(map[int64]map[int64]*RoleGroup)
	for _, r := range relations {
		role := roleByID[r.RoleId]
		roleName := ""
		if role != nil {
			roleName = role.Name
		}
		if _, ok := evalByID[r.EvaluateeId]; !ok {
			continue
		}
		gm, ok := groupsByEval[r.EvaluateeId]
		if !ok {
			gm = make(map[int64]*RoleGroup)
			groupsByEval[r.EvaluateeId] = gm
		}
		g, ok := gm[r.RoleId]
		if !ok {
			g = &RoleGroup{RoleID: r.RoleId, RoleName: roleName, Items: []*RelationItem{}}
			gm[r.RoleId] = g
		}
		brief := empMap[r.EvaluatorEmpId]
		g.Items = append(g.Items, &RelationItem{
			ID:             r.Id,
			EvaluateeID:    r.EvaluateeId,
			EvaluatorEmpID: r.EvaluatorEmpId,
			EvaluatorNo:    brief.EmployeeNo,
			EvaluatorName:  brief.Name,
			RoleID:         r.RoleId,
			RoleName:       roleName,
			Source:         r.Source,
			Completed:      r.Completed,
			DerivedStatus:  deriveRelationStatus(r.Completed, projectClosed),
		})
	}

	// 组装被评估人行（角色分组按 roles 的 id 升序）。
	evalRows := make([]*EvaluateeRow, 0, len(evaluatees))
	for _, e := range evaluatees {
		gm := groupsByEval[e.Id]
		groups := make([]*RoleGroup, 0, len(gm))
		for _, r := range roles {
			if g, ok := gm[r.Id]; ok {
				groups = append(groups, g)
			}
		}
		brief := empMap[e.EvaluateeEmpId]
		evalRows = append(evalRows, &EvaluateeRow{
			EvaluateeID:         e.Id,
			EvaluateeEmpID:      e.EvaluateeEmpId,
			EvaluateeName:       brief.Name,
			EvaluateeNo:         brief.EmployeeNo,
			EvaluateeJobTitle:   e.JobTitle,
			EvaluateeDepartment: e.Department,
			Groups:              groups,
		})
	}

	return &RelationMatrix{
		QuestionnaireID: questionnaireID,
		Evaluatees:      evalRows,
		Roles:           roleItems,
	}, nil
}

// deriveRelationStatus 派生评价关系的展示状态：pending/filled/voided。
// 未填且项目已截止 → voided；已填 → filled；其余 → pending。
func deriveRelationStatus(completed int, projectClosed bool) string {
	if completed == 1 {
		return "filled"
	}
	if projectClosed {
		return "voided"
	}
	return "pending"
}

// SearchEmployees 按工号精确查找在职员工，供关系编辑器选择评价者。
// 本插件为平台级、问卷对所有租户员工通用，故按工号跨租户解析（工号全公司唯一）；
// empcap 无跨租户在职列表接口，且前端新增弹窗已改为纯工号录入，故仅保留工号精确匹配。
func (s *serviceImpl) SearchEmployees(ctx context.Context, keyword string) ([]*EmployeeOption, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return []*EmployeeOption{}, nil
	}
	empSvc := s.empSvc()
	if empSvc == nil {
		return nil, bizerr.NewCode(CodeEmpcapUnbound)
	}
	// 按工号跨租户精确匹配（命中且在职返回单项，否则空）。
	if emp, err := empSvc.GetByEmployeeNoAnyTenant(ctx, NormalizeEmployeeNo(kw)); err == nil && emp != nil && emp.Active() {
		return []*EmployeeOption{toEmployeeOption(emp)}, nil
	}
	return []*EmployeeOption{}, nil
}

// toEmployeeOption 把 empcap.Employee 转为选项 DTO。
func toEmployeeOption(emp *empcap.Employee) *EmployeeOption {
	if emp == nil {
		return nil
	}
	return &EmployeeOption{
		EmpID:      emp.EmpID,
		EmployeeNo: emp.EmployeeNo,
		Name:       emp.Name,
	}
}

// AddRelations 经「新增评价关系」/「编辑」弹窗一次批量新增评价关系（只录入姓名，不录入工号）。
//
// 先把全部条目的被评估人、评价者姓名一次解析（规则见 relation_resolve.go），收集**全部**在职重名
// 与找不到的姓名：任一非空即整体拒绝、不写入任何数据，错误参数 names 列出这些姓名（优先报重名）。
// 全部解析通过后在单事务内写入：被评估人查已有、没有才建（职务/部门按条目顺序合并，后出现的非空值
// 覆盖前值，再以「非空覆盖、空保留」写入既有被评估人）；角色查已有、没有才建；评价关系为纯 INSERT。
func (s *serviceImpl) AddRelations(ctx context.Context, in AddRelationsInput) ([]int64, error) {
	if len(in.Items) == 0 {
		return nil, bizerr.NewCode(CodeRelationItemsEmpty)
	}
	if len(in.Items) > maxAddRelationItems {
		return nil, bizerr.NewCode(CodeRelationItemsTooMany, bizerr.P("max", maxAddRelationItems))
	}
	q, err := s.mustGetQuestionnaire(ctx, in.QuestionnaireID)
	if err != nil {
		return nil, err
	}
	empSvc := s.empSvc()
	if empSvc == nil {
		return nil, bizerr.NewCode(CodeEmpcapUnbound)
	}

	// 归一化并做与数据库无关的校验（姓名/角色非空）。
	type normalizedItem struct {
		evaluateeName string
		evaluatorName string
		roleName      string
		profile       evaluateeProfile
	}
	items := make([]normalizedItem, 0, len(in.Items))
	names := make([]string, 0, len(in.Items)*2)
	for _, it := range in.Items {
		n := normalizedItem{
			evaluateeName: normalizePersonName(it.EvaluateeName),
			evaluatorName: normalizePersonName(it.EvaluatorName),
			roleName:      NormalizeRoleName(it.RoleName),
			// 职务/部门与导入同一归一化口径；长度上限由 API 层 max-length 校验兜住。
			profile: evaluateeProfile{
				JobTitle:   textutil.NormalizeName(it.EvaluateeJobTitle),
				Department: textutil.NormalizeName(it.EvaluateeDepartment),
			},
		}
		if n.evaluateeName == "" || n.evaluatorName == "" {
			return nil, bizerr.NewCode(CodeEmployeeNameEmpty)
		}
		if n.roleName == "" {
			return nil, bizerr.NewCode(CodeRoleNameEmpty)
		}
		items = append(items, n)
		names = append(names, n.evaluateeName, n.evaluatorName)
	}

	// 一次批量解析全部姓名（无 N+1），收集全部重名与找不到的姓名后整体判定。
	byName, err := resolveNames(ctx, empSvc, names)
	if err != nil {
		return nil, err
	}
	var (
		duplicates []string
		missing    []string
		reported   = make(map[string]struct{}, len(byName))
	)
	for _, n := range names {
		if _, done := reported[n]; done {
			continue
		}
		reported[n] = struct{}{}
		switch byName[n].outcome {
		case nameDuplicate:
			duplicates = append(duplicates, n)
		case nameNotFound:
			missing = append(missing, n)
		}
	}
	if len(duplicates) > 0 {
		return nil, bizerr.NewCode(CodeEmployeeNameDuplicate, bizerr.P("names", strings.Join(duplicates, "、")))
	}
	if len(missing) > 0 {
		return nil, bizerr.NewCode(CodeEmployeeNameNotFound, bizerr.P("names", strings.Join(missing, "、")))
	}

	// 自评拦截，并按被评估人合并职务/部门（后出现的非空值覆盖前值）。
	profiles := make(map[int64]evaluateeProfile)
	for _, it := range items {
		eeID, erID := byName[it.evaluateeName].emp.EmpID, byName[it.evaluatorName].emp.EmpID
		if eeID == erID {
			return nil, bizerr.NewCode(CodeImportSelfEvaluate)
		}
		merged := profiles[eeID]
		if it.profile.JobTitle != "" {
			merged.JobTitle = it.profile.JobTitle
		}
		if it.profile.Department != "" {
			merged.Department = it.profile.Department
		}
		profiles[eeID] = merged
	}

	ids := make([]int64, 0, len(items))
	err = dao.Relation.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 同一批内被评估人、角色各只 upsert 一次（批量上限由 API 层限定）。
		evaluateeIDs := make(map[int64]int64, len(profiles))
		roleIDs := make(map[string]int64)
		for _, it := range items {
			eeEmpID, erEmpID := byName[it.evaluateeName].emp.EmpID, byName[it.evaluatorName].emp.EmpID
			evaluateeID, ok := evaluateeIDs[eeEmpID]
			if !ok {
				id, eErr := s.upsertEvaluatee(ctx, q.ProjectId, in.QuestionnaireID, eeEmpID, profiles[eeEmpID])
				if eErr != nil {
					return eErr
				}
				evaluateeIDs[eeEmpID] = id
				evaluateeID = id
			}
			roleID, ok := roleIDs[it.roleName]
			if !ok {
				id, rErr := s.upsertRole(ctx, in.QuestionnaireID, it.roleName)
				if rErr != nil {
					return rErr
				}
				roleIDs[it.roleName] = id
				roleID = id
			}
			// 评价关系：纯新增（source=manual，completed=0）。
			id, insErr := dao.Relation.Ctx(ctx).Data(do.Relation{
				ProjectId:       q.ProjectId,
				QuestionnaireId: in.QuestionnaireID,
				EvaluateeId:     evaluateeID,
				EvaluatorEmpId:  erEmpID,
				RoleId:          roleID,
				Source:          SourceManual,
				Completed:       0,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// UpdateRelationRole 修改一条评价关系的角色（改派）。
func (s *serviceImpl) UpdateRelationRole(ctx context.Context, relationID, roleID int64) error {
	rel, err := s.mustGetRelation(ctx, relationID)
	if err != nil {
		return err
	}
	role, err := s.mustGetRole(ctx, roleID)
	if err != nil {
		return err
	}
	if role.QuestionnaireId != rel.QuestionnaireId {
		return bizerr.NewCode(CodeRoleNotFound)
	}
	_, err = dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().Id, relationID).
		Data(map[string]any{dao.Relation.Columns().RoleId: roleID}).Update()
	return err
}

// DeleteRelation 删除一条评价关系。存在作答时拒绝，引导先清空作答/二次确认。
func (s *serviceImpl) DeleteRelation(ctx context.Context, relationID int64) error {
	if _, err := s.mustGetRelation(ctx, relationID); err != nil {
		return err
	}
	cnt, err := dao.Submission.Ctx(ctx).
		Where(dao.Submission.Columns().RelationId, relationID).Count()
	if err != nil {
		return err
	}
	if cnt > 0 {
		return bizerr.NewCode(CodeRelationHasSubmission)
	}
	_, err = dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().Id, relationID).Delete()
	return err
}

// DeleteEvaluatee 删除一个被评估人及其全部评价关系。其下任一评价关系已有作答时拒绝，
// 引导先清空作答。删除在单事务内完成：先删评价关系，再删被评估人本身（两张表均为硬删除，
// 无 deleted_at）。题目受限评价者名单按评价者本人维护、与被评估人无关，不在此清理。
func (s *serviceImpl) DeleteEvaluatee(ctx context.Context, evaluateeID int64) error {
	evaluatee, err := s.mustGetEvaluatee(ctx, evaluateeID)
	if err != nil {
		return err
	}
	// 作答拦截：按 evaluatee_id 直接统计，避免逐条评价关系查询（无 N+1）。
	cnt, err := dao.Submission.Ctx(ctx).
		Where(dao.Submission.Columns().EvaluateeId, evaluateeID).Count()
	if err != nil {
		return err
	}
	if cnt > 0 {
		return bizerr.NewCode(CodeEvaluateeHasSubmission)
	}
	return dao.Evaluatee.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 评价关系删除同时带上问卷维度，命中 (questionnaire_id, evaluatee_id) 组合索引。
		if _, dErr := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, evaluatee.QuestionnaireId).
			Where(dao.Relation.Columns().EvaluateeId, evaluateeID).Delete(); dErr != nil {
			return dErr
		}
		_, dErr := dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().Id, evaluateeID).Delete()
		return dErr
	})
}

// isProjectClosed 读时派生项目截止状态：now > end_at 或 closed_at != nil。
// 本插件无 cron，截止判定在读路径即时计算。
func (s *serviceImpl) isProjectClosed(ctx context.Context, projectID int64) (bool, error) {
	var p *entitymodel.Project
	if err := dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, projectID).Scan(&p); err != nil {
		return false, err
	}
	if p == nil {
		return false, nil
	}
	if p.ClosedAt != nil {
		return true, nil
	}
	if p.EndAt == nil {
		return false, nil
	}
	return p.EndAt.Before(time.Now()), nil
}

// batchEmployees 经 empcap 批量取 EmpID → (Name, EmployeeNo)（一次查询，无 N+1）。
// empcap 未就绪时返回空 map（姓名/工号列留空，不阻断列表）。
func (s *serviceImpl) batchEmployees(ctx context.Context, empIDs map[int64]struct{}) map[int64]empBrief {
	out := make(map[int64]empBrief, len(empIDs))
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
			out[id] = empBrief{Name: emp.Name, EmployeeNo: emp.EmployeeNo}
		}
	}
	return out
}

// empBrief 是员工摘要（姓名+工号），供列表展示。
type empBrief struct {
	Name       string
	EmployeeNo string
}
