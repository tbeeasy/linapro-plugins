// relation_conflict.go 实现「同一被评估人下，同一评价者只能属于一个角色、且不得重复」的校验。
//
// 规则（FB-5）：以 (被评估人 EmpID, 评价者 EmpID) 为键——
//   - 同一键出现在不同角色 → 角色冲突（CodeRelationEvaluatorRoleConflict）；
//   - 同一键在同一角色下出现多次 → 重复（CodeRelationEvaluatorDuplicate）。
//
// 新增评价关系（含编辑弹窗）要把本批条目与库中该问卷已有关系合并判断；全量导入为覆盖语义，
// 只判断文件内部，冲突的每一行都判失败并写明涉及的行号。按员工 EmpID 判断，提示中显示姓名。
// 数据库唯一索引 (project_id, questionnaire_id, evaluatee_id, evaluator_emp_id) 仍是并发兜底。

package relation

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
)

// evaluatorPair 标识「被评估人 EmpID + 评价者 EmpID」一对。
type evaluatorPair struct {
	evaluateeEmpID int64
	evaluatorEmpID int64
}

// pairEntry 是一条待校验的评价关系（姓名、角色均已归一化）。
type pairEntry struct {
	// rowNum 为导入行号；新增入口不使用，保持 0。
	rowNum        int
	pair          evaluatorPair
	evaluateeName string
	evaluatorName string
	roleName      string
}

// checkAddConflicts 校验新增条目：先载入库中这些被评估人已有的关系角色，再按条目顺序逐条比对，
// 遇到第一处重复或角色冲突即返回对应业务错误；全部通过返回 nil。数据库查询次数固定（至多 3 次）。
func (s *serviceImpl) checkAddConflicts(ctx context.Context, questionnaireID int64, entries []pairEntry) error {
	eeEmpIDs := make(map[int64]struct{}, len(entries))
	for _, e := range entries {
		eeEmpIDs[e.pair.evaluateeEmpID] = struct{}{}
	}
	existing, err := s.loadExistingPairRoles(ctx, questionnaireID, eeEmpIDs)
	if err != nil {
		return err
	}
	// seen 记录每对已占用的角色名（库中已有在前，本批按出现顺序追加）。
	seen := make(map[evaluatorPair][]string, len(existing)+len(entries))
	for pair, role := range existing {
		seen[pair] = []string{role}
	}
	for _, e := range entries {
		roles := seen[e.pair]
		if len(roles) == 0 {
			seen[e.pair] = []string{e.roleName}
			continue
		}
		if slices.Contains(roles, e.roleName) {
			return bizerr.NewCode(CodeRelationEvaluatorDuplicate,
				bizerr.P("evaluatee", e.evaluateeName),
				bizerr.P("evaluator", e.evaluatorName),
				bizerr.P("role", e.roleName),
			)
		}
		return bizerr.NewCode(CodeRelationEvaluatorRoleConflict,
			bizerr.P("evaluatee", e.evaluateeName),
			bizerr.P("evaluator", e.evaluatorName),
			bizerr.P("roles", strings.Join(append(roles, e.roleName), "、")),
		)
	}
	return nil
}

// loadExistingPairRoles 批量读取问卷下给定被评估人（EmpID）已有评价关系的角色名，
// 返回 (被评估人 EmpID, 评价者 EmpID) → 角色名。唯一索引保证每对至多一条关系。
func (s *serviceImpl) loadExistingPairRoles(
	ctx context.Context, questionnaireID int64, eeEmpIDs map[int64]struct{},
) (map[evaluatorPair]string, error) {
	out := make(map[evaluatorPair]string)
	if len(eeEmpIDs) == 0 {
		return out, nil
	}
	var evaluatees []struct {
		Id             int64 `orm:"id"`
		EvaluateeEmpId int64 `orm:"evaluatee_emp_id"`
	}
	if err := dao.Evaluatee.Ctx(ctx).
		Fields(dao.Evaluatee.Columns().Id, dao.Evaluatee.Columns().EvaluateeEmpId).
		Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).
		WhereIn(dao.Evaluatee.Columns().EvaluateeEmpId, keysOfMap(eeEmpIDs)).
		Scan(&evaluatees); err != nil {
		return nil, err
	}
	if len(evaluatees) == 0 {
		return out, nil
	}
	empByEvaluatee := make(map[int64]int64, len(evaluatees))
	evaluateeIDs := make([]int64, 0, len(evaluatees))
	for _, e := range evaluatees {
		empByEvaluatee[e.Id] = e.EvaluateeEmpId
		evaluateeIDs = append(evaluateeIDs, e.Id)
	}

	var rels []struct {
		EvaluateeId    int64 `orm:"evaluatee_id"`
		EvaluatorEmpId int64 `orm:"evaluator_emp_id"`
		RoleId         int64 `orm:"role_id"`
	}
	if err := dao.Relation.Ctx(ctx).
		Fields(dao.Relation.Columns().EvaluateeId, dao.Relation.Columns().EvaluatorEmpId, dao.Relation.Columns().RoleId).
		Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).
		WhereIn(dao.Relation.Columns().EvaluateeId, evaluateeIDs).
		Scan(&rels); err != nil {
		return nil, err
	}
	if len(rels) == 0 {
		return out, nil
	}

	var roles []struct {
		Id   int64  `orm:"id"`
		Name string `orm:"name"`
	}
	if err := dao.Role.Ctx(ctx).
		Fields(dao.Role.Columns().Id, dao.Role.Columns().Name).
		Where(dao.Role.Columns().QuestionnaireId, questionnaireID).
		Scan(&roles); err != nil {
		return nil, err
	}
	roleNames := make(map[int64]string, len(roles))
	for _, r := range roles {
		roleNames[r.Id] = r.Name
	}
	for _, r := range rels {
		pair := evaluatorPair{evaluateeEmpID: empByEvaluatee[r.EvaluateeId], evaluatorEmpID: r.EvaluatorEmpId}
		out[pair] = roleNames[r.RoleId]
	}
	return out, nil
}

// importConflictReasons 判断导入文件内部的重复与角色冲突（全量导入为覆盖语义，不比对库中已有关系）。
// 同一对出现在多行时，涉及的每一行都判失败；返回 行号 → 失败原因，无冲突返回空 map。
func importConflictReasons(entries []pairEntry) map[int]string {
	groups := make(map[evaluatorPair][]pairEntry)
	order := make([]evaluatorPair, 0, len(entries))
	for _, e := range entries {
		if _, ok := groups[e.pair]; !ok {
			order = append(order, e.pair)
		}
		groups[e.pair] = append(groups[e.pair], e)
	}
	out := make(map[int]string)
	for _, pair := range order {
		group := groups[pair]
		if len(group) < 2 {
			continue
		}
		reason := importConflictReason(group)
		for _, e := range group {
			out[e.rowNum] = reason
		}
	}
	return out
}

// importConflictReason 为同一对的多行生成失败原因：角色全部相同报重复，否则报角色冲突并逐行列出角色。
func importConflictReason(group []pairEntry) string {
	first := group[0]
	sameRole := true
	rowNums := make([]string, 0, len(group))
	details := make([]string, 0, len(group))
	for _, e := range group {
		if e.roleName != first.roleName {
			sameRole = false
		}
		rowNums = append(rowNums, strconv.Itoa(e.rowNum))
		details = append(details, fmt.Sprintf("「%s」（第 %d 行）", e.roleName, e.rowNum))
	}
	if sameRole {
		return fmt.Sprintf("被评估人「%s」的角色「%s」下评价者「%s」重复（第 %s 行）",
			first.evaluateeName, first.roleName, first.evaluatorName, strings.Join(rowNums, "、"))
	}
	return fmt.Sprintf("被评估人「%s」的评价者「%s」同时出现在多个角色中：%s，同一评价者对同一被评估人只能属于一个角色",
		first.evaluateeName, first.evaluatorName, strings.Join(details, "、"))
}
