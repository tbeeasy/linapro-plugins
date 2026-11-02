// import_impl.go 实现评价关系 Excel 全量导入与导出。
//
// 导入语义（见 design.md / spec）：
//   - 仅全量模式、仅发布前允许；发布后拒绝，追加走关系编辑器弹窗。
//   - 模板列：被评估人姓名 / 被评估人职务 / 被评估人部门 / 评价者姓名 / 角色（不录入工号）。
//   - 按表头识别列：被评估人姓名、评价者姓名、角色为必需列；职务/部门可选；其余列（如导出文件中的
//     两个工号列）忽略，故导出的 Excel 修改后可直接再导入。
//   - 行级校验：姓名按 relation_resolve.go 规则解析为唯一在职员工（找不到或在职重名判行失败并点名）、
//     角色名非空、被评估人≠评价者；职务/部门可选，超长或同一被评估人多行取值冲突判行失败。
//   - 任一行校验失败 → 中止整批、回报行号与原因、不写入任何数据。
//   - 全部通过后单事务 upsert 角色（归一化名称，默认权重 0）→ 被评估人 → 评价关系。
package relation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/xuri/excelize/v2"

	"lina-core/pkg/bizerr"
	"lina-core/pkg/excelutil"
	"lina-core/pkg/logger"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	"lina-plugin-linapro-eval-360/backend/internal/service/textutil"
)

// 导入/导出列的表头文案；导入按表头文案识别列，不依赖列序。
const (
	headerEvaluateeName       = "被评估人姓名"
	headerEvaluateeNo         = "被评估人工号" // 仅导出
	headerEvaluateeJobTitle   = "被评估人职务" // 可选
	headerEvaluateeDepartment = "被评估人部门" // 可选
	headerEvaluatorName       = "评价者姓名"
	headerEvaluatorNo         = "评价者工号" // 仅导出
	headerRole                = "角色"
	importHeaderRow           = 1
)

// 被评估人职务/部门的长度上限（字符数），与 evaluatee 表 VARCHAR 宽度一致。
const (
	maxJobTitleLen   = 100
	maxDepartmentLen = 200
)

// importHeaders 返回导入模板的表头（不含工号）。
func importHeaders() []string {
	return []string{headerEvaluateeName, headerEvaluateeJobTitle, headerEvaluateeDepartment, headerEvaluatorName, headerRole}
}

// exportHeaders 返回导出表头：在导入模板基础上保留两个工号列便于核对；再导入时工号列被忽略。
func exportHeaders() []string {
	return []string{
		headerEvaluateeName, headerEvaluateeNo, headerEvaluateeJobTitle, headerEvaluateeDepartment,
		headerEvaluatorName, headerEvaluatorNo, headerRole,
	}
}

// requiredImportHeaders 是导入文件必须包含的列（职务/部门可选）。
func requiredImportHeaders() []string {
	return []string{headerEvaluateeName, headerEvaluatorName, headerRole}
}

// importRow 是解析后的单行导入数据。
type importRow struct {
	rowNum        int
	evaluateeName string
	jobTitle      string
	department    string
	evaluatorName string
	roleName      string
}

// evaluateeProfile 是被评估人的问卷级录入属性（职务/部门），空串表示未填写。
type evaluateeProfile struct {
	JobTitle   string
	Department string
}

// mergeEvaluateeProfile 把同一被评估人新一行的职务/部门并入已收集的属性：
// 空值不覆盖已有值；已有非空值与新非空值不同视为冲突，返回失败原因（此时 merged 原样返回 prev）。
// 入参 jobTitle/department 须已归一化。
func mergeEvaluateeProfile(prev evaluateeProfile, jobTitle, department string) (merged evaluateeProfile, reason string) {
	if jobTitle != "" && prev.JobTitle != "" && jobTitle != prev.JobTitle {
		return prev, fmt.Sprintf("被评估人职务「%s」与该被评估人此前填写的「%s」不一致", jobTitle, prev.JobTitle)
	}
	if department != "" && prev.Department != "" && department != prev.Department {
		return prev, fmt.Sprintf("被评估人部门「%s」与该被评估人此前填写的「%s」不一致", department, prev.Department)
	}
	merged = prev
	if merged.JobTitle == "" {
		merged.JobTitle = jobTitle
	}
	if merged.Department == "" {
		merged.Department = department
	}
	return merged, ""
}

// evaluateeProfileLengthReason 校验职务/部门长度，超出表字段宽度时返回失败原因，合法返回空串。
func evaluateeProfileLengthReason(jobTitle, department string) string {
	if utf8.RuneCountInString(jobTitle) > maxJobTitleLen {
		return fmt.Sprintf("被评估人职务不能超过 %d 个字", maxJobTitleLen)
	}
	if utf8.RuneCountInString(department) > maxDepartmentLen {
		return fmt.Sprintf("被评估人部门不能超过 %d 个字", maxDepartmentLen)
	}
	return ""
}

// resolveRowProfile 归一化一行导入的职务/部门，校验长度后并入该被评估人已收集的属性；
// 失败时返回原因（此时 merged 为 prev，调用方应判该行失败）。
func resolveRowProfile(prev evaluateeProfile, rawJobTitle, rawDepartment string) (merged evaluateeProfile, reason string) {
	jobTitle := textutil.NormalizeName(rawJobTitle)
	department := textutil.NormalizeName(rawDepartment)
	if reason = evaluateeProfileLengthReason(jobTitle, department); reason != "" {
		return prev, reason
	}
	return mergeEvaluateeProfile(prev, jobTitle, department)
}

// GenerateImportTemplate 生成评价关系 Excel 全量导入模板。
func (s *serviceImpl) GenerateImportTemplate(ctx context.Context) (data []byte, err error) {
	f := excelize.NewFile()
	defer excelutil.CloseFile(ctx, f, &err)
	sheet := "Sheet1"

	for i, h := range importHeaders() {
		if err = excelutil.SetCellValue(f, sheet, i+1, importHeaderRow, h); err != nil {
			return nil, err
		}
	}
	// 示例行（顺序与 importHeaders 一致）。
	sample := []string{"张三", "销售总监", "华东销售部", "李四", "上级"}
	for i, v := range sample {
		if err = excelutil.SetCellValue(f, sheet, i+1, importHeaderRow+1, v); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err = f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportRelations 全量导入评价关系。
func (s *serviceImpl) ImportRelations(ctx context.Context, in ImportInput) (*ImportResult, error) {
	result := &ImportResult{}

	q, err := s.mustGetQuestionnaire(ctx, in.QuestionnaireID)
	if err != nil {
		return nil, err
	}
	// 已发布问卷禁止全量导入。
	if q.Published == publishedYes {
		return nil, bizerr.NewCode(CodeQuestionnaireNotImportable)
	}

	// empcap 必须就绪（姓名解析依赖它）。
	empSvc := s.empSvc()
	if empSvc == nil {
		return nil, bizerr.NewCode(CodeEmpcapUnbound)
	}

	// 解析 Excel。
	rows, err := parseImportRows(ctx, in.Reader)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, bizerr.NewCode(CodeImportEmpty)
	}

	// 行级校验：姓名→唯一在职员工、角色名非空、被评估人≠评价者。
	type resolved struct {
		rowNum      int
		evaluateeID int64 // EmpID
		evaluatorID int64 // EmpID
		roleName    string
	}
	resolvedRows := make([]resolved, 0, len(rows))
	// 被评估人 EmpID → 合并后的职务/部门（同一被评估人多行取首个非空值）。
	profiles := make(map[int64]evaluateeProfile)

	// 一次性批量解析全部姓名（无 N+1）：问卷为平台级，按姓名跨租户解析。
	names := make([]string, 0, len(rows)*2)
	for _, r := range rows {
		names = append(names, normalizePersonName(r.evaluateeName), normalizePersonName(r.evaluatorName))
	}
	byName, err := resolveNames(ctx, empSvc, names)
	if err != nil {
		return nil, err
	}

	// fail 记一行失败（行号 + 原因）。
	fail := func(rowNum int, reason string) {
		result.Fail++
		result.FailList = append(result.FailList, ImportFailItem{Row: rowNum, Reason: reason})
	}
	for _, r := range rows {
		// 角色名归一化。
		normRole := NormalizeRoleName(r.roleName)
		if normRole == "" {
			fail(r.rowNum, "角色不能为空")
			continue
		}
		eeName := normalizePersonName(r.evaluateeName)
		if eeName == "" {
			fail(r.rowNum, "被评估人姓名不能为空")
			continue
		}
		evalName := normalizePersonName(r.evaluatorName)
		if evalName == "" {
			fail(r.rowNum, "评价者姓名不能为空")
			continue
		}
		// 姓名 → 唯一在职员工：找不到或在职重名判行失败，并点名该姓名。
		eeRes := byName[eeName]
		if reason := nameFailReason("被评估人", eeName, eeRes); reason != "" {
			fail(r.rowNum, reason)
			continue
		}
		evalRes := byName[evalName]
		if reason := nameFailReason("评价者", evalName, evalRes); reason != "" {
			fail(r.rowNum, reason)
			continue
		}
		eeEmp, evalEmp := eeRes.emp, evalRes.emp
		if evalEmp.EmpID == eeEmp.EmpID {
			fail(r.rowNum, "被评估人与评价者不能为同一员工")
			continue
		}
		// 职务/部门：可选；超长或与同一被评估人此前行的非空值冲突判行失败。
		merged, reason := resolveRowProfile(profiles[eeEmp.EmpID], r.jobTitle, r.department)
		if reason != "" {
			fail(r.rowNum, reason)
			continue
		}
		profiles[eeEmp.EmpID] = merged
		resolvedRows = append(resolvedRows, resolved{
			rowNum: r.rowNum, evaluateeID: eeEmp.EmpID, evaluatorID: evalEmp.EmpID, roleName: normRole,
		})
	}

	// 任意行失败 → 中止整批，不写入任何数据。
	if result.Fail > 0 {
		return result, nil
	}

	// 全部通过 → 单事务写入。
	err = dao.Relation.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 先清空该问卷既有被评估人与评价关系（全量替换语义）。
		// 角色保留（避免 HR 已配的权重被清掉）——仅在被评估人/关系层做全量替换。
		if _, err := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, in.QuestionnaireID).Delete(); err != nil {
			return err
		}
		if _, err := dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().QuestionnaireId, in.QuestionnaireID).Delete(); err != nil {
			return err
		}

		// 被评估人缓存：同一 (questionnaire, emp) 复用 evaluatee.id。
		evalMap := make(map[int64]int64, len(resolvedRows))
		// 角色缓存：归一化名称 → role.id。
		roleMap := make(map[string]int64)

		for _, rr := range resolvedRows {
			// upsert 角色。
			roleID, ok := roleMap[rr.roleName]
			if !ok {
				rid, rErr := s.upsertRole(ctx, in.QuestionnaireID, rr.roleName)
				if rErr != nil {
					return rErr
				}
				roleMap[rr.roleName] = rid
				roleID = rid
			}
			// upsert 被评估人。
			evalID, ok := evalMap[rr.evaluateeID]
			if !ok {
				eid, eErr := s.upsertEvaluatee(ctx, q.ProjectId, in.QuestionnaireID, rr.evaluateeID, profiles[rr.evaluateeID])
				if eErr != nil {
					return eErr
				}
				evalMap[rr.evaluateeID] = eid
				evalID = eid
			}
			// 插入评价关系（source=import，completed=0）。
			if _, err := dao.Relation.Ctx(ctx).Data(do.Relation{
				ProjectId:       q.ProjectId,
				QuestionnaireId: in.QuestionnaireID,
				EvaluateeId:     evalID,
				EvaluatorEmpId:  rr.evaluatorID,
				RoleId:          roleID,
				Source:          SourceImport,
				Completed:       0,
			}).Insert(); err != nil {
				return err
			}
			result.Success++
		}
		return nil
	})
	if err != nil {
		// 事务失败 → 把已计的成功回滚为失败，统一回报。
		logger.Errorf(ctx, "eval-360 import relations tx failed: %v", err)
		result.Fail += result.Success
		result.Success = 0
		return result, err
	}
	return result, nil
}

// nameFailReason 把姓名解析结果转为导入行失败原因；解析成功返回空串。label 为「被评估人」/「评价者」。
func nameFailReason(label, name string, res nameResolution) string {
	switch res.outcome {
	case nameResolved:
		return ""
	case nameDuplicate:
		return fmt.Sprintf("%s姓名「%s」有 %d 名在职同名员工，无法确定是哪一位", label, name, res.activeCount)
	default:
		return fmt.Sprintf("%s「%s」找不到在职员工", label, name)
	}
}

// upsertEvaluatee 按问卷 + EmpID upsert 被评估人（唯一索引兜底）。
// 不存在时按 profile 新建；已存在时 profile 中非空且与原值不同的字段覆盖原值，空字段保留原值。
func (s *serviceImpl) upsertEvaluatee(ctx context.Context, projectID, questionnaireID, empID int64, profile evaluateeProfile) (int64, error) {
	existing, err := s.findEvaluateeByEmp(ctx, questionnaireID, empID)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		// 只更新确有变化的非空字段；DO 未赋值字段为 nil，ORM 自动忽略。
		data := do.Evaluatee{}
		changed := false
		if profile.JobTitle != "" && profile.JobTitle != existing.JobTitle {
			data.JobTitle = profile.JobTitle
			changed = true
		}
		if profile.Department != "" && profile.Department != existing.Department {
			data.Department = profile.Department
			changed = true
		}
		if changed {
			if _, err = dao.Evaluatee.Ctx(ctx).
				Where(dao.Evaluatee.Columns().Id, existing.Id).Data(data).Update(); err != nil {
				return 0, err
			}
		}
		return existing.Id, nil
	}
	id, err := dao.Evaluatee.Ctx(ctx).Data(do.Evaluatee{
		ProjectId:       projectID,
		QuestionnaireId: questionnaireID,
		EvaluateeEmpId:  empID,
		JobTitle:        profile.JobTitle,
		Department:      profile.Department,
	}).InsertAndGetId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// parseImportRows 解析 Excel 文件为导入行切片：按首行表头文案识别列（不依赖列序），
// 缺少必需列时返回 CodeImportHeaderMissing；未识别的列（如导出文件中的工号列）忽略。
func parseImportRows(ctx context.Context, reader io.Reader) (out []importRow, err error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeImportExcelParseFailed)
	}
	defer excelutil.CloseFile(ctx, f, &err)

	rows, err := f.GetRows("Sheet1")
	if err != nil {
		return nil, bizerr.WrapCode(err, CodeImportSheetReadFailed, bizerr.P("sheet", "Sheet1"))
	}
	if len(rows) == 0 {
		return []importRow{}, nil
	}
	cols, err := importColumns(rows[0])
	if err != nil {
		return nil, err
	}
	out = make([]importRow, 0, len(rows)-1)
	for i, row := range rows[1:] {
		rowNum := i + 2 // Excel 实际行号（含表头）。
		// 跳过完全空行。
		if len(row) == 0 {
			continue
		}
		out = append(out, importRow{
			rowNum:        rowNum,
			evaluateeName: cellAt(row, cols[headerEvaluateeName]),
			jobTitle:      cellAt(row, cols[headerEvaluateeJobTitle]),
			department:    cellAt(row, cols[headerEvaluateeDepartment]),
			evaluatorName: cellAt(row, cols[headerEvaluatorName]),
			roleName:      cellAt(row, cols[headerRole]),
		})
	}
	return out, nil
}

// importColumns 由表头行建立「表头文案 → 列号（1-based）」映射：表头文案去首尾空白后精确匹配，
// 同名表头取第一次出现的列。缺少任一必需列时返回 CodeImportHeaderMissing（参数 column 为列名）。
func importColumns(header []string) (map[string]int, error) {
	cols := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimSpace(h)
		if name == "" {
			continue
		}
		if _, ok := cols[name]; !ok {
			cols[name] = i + 1
		}
	}
	for _, required := range requiredImportHeaders() {
		if _, ok := cols[required]; !ok {
			return nil, bizerr.NewCode(CodeImportHeaderMissing, bizerr.P("column", required))
		}
	}
	return cols, nil
}

// cellAt 取一行中第 col 列（1-based）的单元格文本；excelize 会截掉行尾空列，
// 越界或 col=0（该可选列不存在）返回空串。
func cellAt(row []string, col int) string {
	if col < 1 || col > len(row) {
		return ""
	}
	return row[col-1]
}

// ExportRelations 导出当前问卷的评价关系 Excel（导入模板各列 + 被评估人/评价者工号，便于核对）。
// 仅导已存在的评价关系（全量快照），供未发布时修改后重新全量导入（导入按表头识别列，工号列忽略）。
func (s *serviceImpl) ExportRelations(ctx context.Context, questionnaireID int64) (data []byte, err error) {
	if _, err = s.mustGetQuestionnaire(ctx, questionnaireID); err != nil {
		return nil, err
	}

	// 取全部评价关系（含角色名、被评估人工号需 JOIN employee）。
	var rels []*struct {
		EvaluateeId    int64 `orm:"evaluatee_id"`
		EvaluatorEmpId int64 `orm:"evaluator_emp_id"`
		RoleId         int64 `orm:"role_id"`
	}
	if err = dao.Relation.Ctx(ctx).
		Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).
		OrderAsc(dao.Relation.Columns().EvaluateeId).
		OrderAsc(dao.Relation.Columns().Id).
		Scan(&rels); err != nil {
		return nil, err
	}

	empSvc := s.empSvc()
	// 角色名映射。
	roleNames := make(map[int64]string)
	if len(rels) > 0 {
		roleIDs := make([]int64, 0, len(rels))
		seen := make(map[int64]struct{})
		for _, r := range rels {
			if _, ok := seen[r.RoleId]; !ok {
				seen[r.RoleId] = struct{}{}
				roleIDs = append(roleIDs, r.RoleId)
			}
		}
		var roles []struct {
			Id   int64  `orm:"id"`
			Name string `orm:"name"`
		}
		if err = dao.Role.Ctx(ctx).
			WhereIn(dao.Role.Columns().Id, roleIDs).Scan(&roles); err != nil {
			return nil, err
		}
		for _, r := range roles {
			roleNames[r.Id] = r.Name
		}
	}

	// 被评估人工号映射（被评估人 EmpID → 工号）。
	evalEmpIDs := make(map[int64]struct{})
	for _, r := range rels {
		evalEmpIDs[r.EvaluateeId] = struct{}{}
	}
	// 被评估人工号映射：被评估人表存 evaluatee_emp_id，评价关系存 evaluator_emp_id；
	// r.EvaluateeId 是 evaluatee.id，需先取 evaluatee_emp_id 再查工号。
	eeEmpIDMap := make(map[int64]int64)              // evaluatee.id -> evaluatee_emp_id
	eeProfileMap := make(map[int64]evaluateeProfile) // evaluatee.id -> 职务/部门
	if len(evalEmpIDs) > 0 {
		var ees []struct {
			Id             int64  `orm:"id"`
			EvaluateeEmpId int64  `orm:"evaluatee_emp_id"`
			JobTitle       string `orm:"job_title"`
			Department     string `orm:"department"`
		}
		if err = dao.Evaluatee.Ctx(ctx).
			Fields(
				dao.Evaluatee.Columns().Id,
				dao.Evaluatee.Columns().EvaluateeEmpId,
				dao.Evaluatee.Columns().JobTitle,
				dao.Evaluatee.Columns().Department,
			).
			WhereIn(dao.Evaluatee.Columns().Id, keysOfMap(evalEmpIDs)).Scan(&ees); err != nil {
			return nil, err
		}
		for _, e := range ees {
			eeEmpIDMap[e.Id] = e.EvaluateeEmpId
			eeProfileMap[e.Id] = evaluateeProfile{JobTitle: e.JobTitle, Department: e.Department}
		}
	}

	// 经 empcap 批量取姓名/工号（若 empcap 未就绪，姓名/工号列留空，仅导关系骨架）。
	empIDSet := make(map[int64]struct{})
	for _, r := range rels {
		empIDSet[r.EvaluatorEmpId] = struct{}{}
		if eid, ok := eeEmpIDMap[r.EvaluateeId]; ok {
			empIDSet[eid] = struct{}{}
		}
	}
	empNoMap := make(map[int64]string)
	empNameMap := make(map[int64]string)
	if empSvc != nil && len(empIDSet) > 0 {
		ids := make([]int64, 0, len(empIDSet))
		for id := range empIDSet {
			ids = append(ids, id)
		}
		empMap, eErr := empSvc.MapByEmpIDs(ctx, ids)
		if eErr == nil {
			for id, emp := range empMap {
				if emp != nil {
					empNoMap[id] = emp.EmployeeNo
					empNameMap[id] = emp.Name
				}
			}
		}
	}

	f := excelize.NewFile()
	defer excelutil.CloseFile(ctx, f, &err)
	sheet := "Sheet1"
	for i, h := range exportHeaders() {
		if err = excelutil.SetCellValue(f, sheet, i+1, importHeaderRow, h); err != nil {
			return nil, err
		}
	}
	for i, r := range rels {
		row := importHeaderRow + 1 + i
		eeEmpID := eeEmpIDMap[r.EvaluateeId]
		profile := eeProfileMap[r.EvaluateeId]
		// 按 exportHeaders 列序写出一整行（含工号；再导入时工号列被忽略）。
		cells := []string{
			empNameMap[eeEmpID],
			empNoMap[eeEmpID],
			profile.JobTitle,
			profile.Department,
			empNameMap[r.EvaluatorEmpId],
			empNoMap[r.EvaluatorEmpId],
			roleNames[r.RoleId],
		}
		for ci, v := range cells {
			if err = excelutil.SetCellValue(f, sheet, ci+1, row, v); err != nil {
				return nil, err
			}
		}
	}

	var buf bytes.Buffer
	if err = f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// keysOfMap 提取 map[int64]struct{} 的键切片。
func keysOfMap(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
