// import_impl_test.go 覆盖评价关系全量导入/导出的语义（FB-13 职务/部门、FB-32 按姓名导入）。
//
// 与 openspec/changes/linapro-eval-360/specs/linapro-eval-360/spec.md「Excel 全量导入评价关系」
// 「关系编辑器」需求一致：
//  1. 模板列为 被评估人姓名/职务/部门/评价者姓名/角色（不含工号），导入按表头识别列；
//  2. 同一被评估人多行的职务/部门取首个非空值，留空不报错；
//  3. 同一被评估人出现不同的非空职务（或部门）→ 该行失败、整批不写入；
//  4. 导出在模板列基础上保留两个工号列，导出文件可原样再导入（工号列忽略）；
//  5. 姓名有两名及以上在职同名员工 → 该行失败并点名、整批不写入；离职同名员工不算重名。
//
// 纯函数用例无需数据库；导入/导出用例直连真实 PostgreSQL（经 testkit 自举建表），
// 员工经 testkit 的 empcap 测试实现写入真实员工表。
package relation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/xuri/excelize/v2"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	"lina-plugin-linapro-eval-360/backend/internal/service/testkit"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// profileFixture 是职务/部门用例共用的数据快照：1 个问卷 + 3 名在职员工。
type profileFixture struct {
	questionnaireID int64
	// emps 依次为被评估人 A、被评估人 B、评价者（Name/EmployeeNo 均唯一）。
	emps [3]empcap.Employee
}

// seedProfileFixture 建项目、问卷与 3 名员工，并绑定 empcap 测试实现；
// 全部数据与 empcap 绑定在 t.Cleanup 中按依赖倒序清理/还原，保证用例自包含、顺序无关。
func seedProfileFixture(t *testing.T, ctx context.Context) *profileFixture {
	t.Helper()

	prevEmpSvc := empcap.BoundEmployeeService()
	empSvc := testkit.NewEmployeeService()
	empcap.BindEmployeeService(empSvc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	projectID, err := dao.Project.Ctx(ctx).Data(do.Project{
		Name:        "FB13 职务部门 " + t.Name() + suffix,
		Description: "集成测试",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}
	questionnaireID, err := dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId: projectID,
		Title:     "FB13 问卷",
	}).InsertAndGetId()
	if err != nil {
		t.Fatalf("建问卷失败：%v", err)
	}

	fixture := &profileFixture{questionnaireID: questionnaireID}
	empIDs := make([]int64, 0, len(fixture.emps))
	for i, label := range []string{"被评A", "被评B", "评价者"} {
		profile := empcap.Profile{
			Name:       fmt.Sprintf("FB13%s%s", label, suffix),
			EmployeeNo: fmt.Sprintf("FB13-%d-%s", i, suffix),
		}
		empID, uErr := empSvc.UpsertEmployee(ctx, 1, profile)
		if uErr != nil {
			t.Fatalf("建员工 %s 失败：%v", label, uErr)
		}
		empIDs = append(empIDs, empID)
		fixture.emps[i] = empcap.Employee{EmpID: empID, Name: profile.Name, EmployeeNo: profile.EmployeeNo}
	}

	t.Cleanup(func() {
		empcap.BindEmployeeService(prevEmpSvc)
		if _, dErr := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理评价关系失败：%v", dErr)
		}
		if _, dErr := dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理被评估人失败：%v", dErr)
		}
		if _, dErr := dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().QuestionnaireId, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理角色失败：%v", dErr)
		}
		if _, dErr := dao.Questionnaire.Ctx(ctx).
			Where(dao.Questionnaire.Columns().Id, questionnaireID).Delete(); dErr != nil {
			t.Errorf("清理问卷失败：%v", dErr)
		}
		if _, dErr := dao.Project.Ctx(ctx).
			Where(dao.Project.Columns().Id, projectID).Delete(); dErr != nil {
			t.Errorf("清理项目失败：%v", dErr)
		}
		if _, dErr := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
			WhereIn("id", empIDs).Delete(); dErr != nil {
			t.Errorf("清理员工失败：%v", dErr)
		}
	})
	return fixture
}

// buildImportXLSX 按生产模板表头生成导入文件；rows 每行 5 列，顺序同 importHeaders。
func buildImportXLSX(t *testing.T, rows [][]string) *bytes.Reader {
	t.Helper()
	return buildXLSX(t, importHeaders(), rows)
}

// buildXLSX 以给定表头与数据行生成 Sheet1 的 xlsx 文件。
func buildXLSX(t *testing.T, header []string, rows [][]string) *bytes.Reader {
	t.Helper()

	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("关闭测试 xlsx 失败：%v", err)
		}
	}()
	all := append([][]string{header}, rows...)
	for ri, row := range all {
		for ci, v := range row {
			cell, err := excelize.CoordinatesToCellName(ci+1, ri+1)
			if err != nil {
				t.Fatalf("计算单元格坐标失败：%v", err)
			}
			if err = f.SetCellValue("Sheet1", cell, v); err != nil {
				t.Fatalf("写测试 xlsx 失败：%v", err)
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("输出测试 xlsx 失败：%v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

// importRowOf 拼一行导入数据（不含工号）：被评估人 ee、评价者 er 取自 fixture 员工。
func importRowOf(ee, er empcap.Employee, jobTitle, department, role string) []string {
	return []string{ee.Name, jobTitle, department, er.Name, role}
}

// seedHomonym 在 tenantID 下新建一名与 name 同名的员工（工号唯一），departed=true 时置为离职；
// 员工行在 t.Cleanup 中删除，保证用例自包含。
func seedHomonym(t *testing.T, ctx context.Context, tenantID int, name string, departed bool) empcap.Employee {
	t.Helper()

	empSvc := testkit.NewEmployeeService()
	no := fmt.Sprintf("FB32-%d", time.Now().UnixNano())
	empID, err := empSvc.UpsertEmployee(ctx, tenantID, empcap.Profile{Name: name, EmployeeNo: no})
	if err != nil {
		t.Fatalf("建同名员工失败：%v", err)
	}
	t.Cleanup(func() {
		if _, dErr := g.DB().Model("plugin_linapro_employee_core_employee").Ctx(ctx).
			Where("id", empID).Delete(); dErr != nil {
			t.Errorf("清理同名员工失败：%v", dErr)
		}
	})
	if departed {
		if err = empSvc.MarkDeparted(ctx, tenantID, empID); err != nil {
			t.Fatalf("置同名员工离职失败：%v", err)
		}
	}
	return empcap.Employee{EmpID: empID, Name: name, EmployeeNo: no}
}

// relationCount 统计问卷下的评价关系条数。
func relationCount(t *testing.T, ctx context.Context, questionnaireID int64) int {
	t.Helper()

	n, err := dao.Relation.Ctx(ctx).Where(dao.Relation.Columns().QuestionnaireId, questionnaireID).Count()
	if err != nil {
		t.Fatalf("统计评价关系失败：%v", err)
	}
	return n
}

// columnOf 返回表头中某列的 1-based 列号，找不到时判测试失败。
func columnOf(t *testing.T, header []string, name string) int {
	t.Helper()

	for i, h := range header {
		if h == name {
			return i + 1
		}
	}
	t.Fatalf("表头 %v 中找不到列 %q", header, name)
	return 0
}

// evaluateeProfileOf 读取问卷下某员工作为被评估人的职务/部门（不存在返回 nil）。
func evaluateeProfileOf(t *testing.T, ctx context.Context, questionnaireID, empID int64) *evaluateeProfile {
	t.Helper()

	var row *struct {
		JobTitle   string `orm:"job_title"`
		Department string `orm:"department"`
	}
	if err := dao.Evaluatee.Ctx(ctx).
		Fields(dao.Evaluatee.Columns().JobTitle, dao.Evaluatee.Columns().Department).
		Where(dao.Evaluatee.Columns().QuestionnaireId, questionnaireID).
		Where(dao.Evaluatee.Columns().EvaluateeEmpId, empID).
		Scan(&row); err != nil {
		t.Fatalf("读取被评估人失败：%v", err)
	}
	if row == nil {
		return nil
	}
	return &evaluateeProfile{JobTitle: row.JobTitle, Department: row.Department}
}

// TestMergeEvaluateeProfile 覆盖同一被评估人多行职务/部门的合并规则。
func TestMergeEvaluateeProfile(t *testing.T) {
	cases := []struct {
		name       string
		prev       evaluateeProfile
		jobTitle   string
		department string
		want       evaluateeProfile
		wantFail   bool
	}{
		{name: "首行写入", prev: evaluateeProfile{}, jobTitle: "总监", department: "销售部", want: evaluateeProfile{"总监", "销售部"}},
		{name: "后续留空保留", prev: evaluateeProfile{"总监", "销售部"}, want: evaluateeProfile{"总监", "销售部"}},
		{name: "相同值放行", prev: evaluateeProfile{"总监", "销售部"}, jobTitle: "总监", department: "销售部", want: evaluateeProfile{"总监", "销售部"}},
		{name: "首行留空后续补齐", prev: evaluateeProfile{JobTitle: "总监"}, department: "销售部", want: evaluateeProfile{"总监", "销售部"}},
		{name: "职务冲突", prev: evaluateeProfile{"总监", ""}, jobTitle: "经理", want: evaluateeProfile{"总监", ""}, wantFail: true},
		{name: "部门冲突", prev: evaluateeProfile{"", "销售部"}, department: "市场部", want: evaluateeProfile{"", "销售部"}, wantFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := mergeEvaluateeProfile(c.prev, c.jobTitle, c.department)
			if (reason != "") != c.wantFail {
				t.Fatalf("冲突判定期望 %v，实际原因 %q", c.wantFail, reason)
			}
			if got != c.want {
				t.Errorf("合并结果期望 %+v，实际 %+v", c.want, got)
			}
		})
	}
}

// TestResolveRowProfile 覆盖单行职务/部门的归一化与长度上限（与 evaluatee 表 VARCHAR 宽度一致）。
func TestResolveRowProfile(t *testing.T) {
	got, reason := resolveRowProfile(evaluateeProfile{}, "  销售　总监 ", " 华东  销售部")
	if reason != "" || got != (evaluateeProfile{JobTitle: "销售 总监", Department: "华东 销售部"}) {
		t.Fatalf("归一化结果异常：%+v，原因 %q", got, reason)
	}
	if _, reason = resolveRowProfile(evaluateeProfile{}, strings.Repeat("职", maxJobTitleLen), ""); reason != "" {
		t.Fatalf("职务恰好 %d 字应放行，实际原因 %q", maxJobTitleLen, reason)
	}
	if _, reason = resolveRowProfile(evaluateeProfile{}, strings.Repeat("职", maxJobTitleLen+1), ""); reason == "" {
		t.Fatalf("职务超过 %d 字应判失败", maxJobTitleLen)
	}
	if _, reason = resolveRowProfile(evaluateeProfile{}, "", strings.Repeat("部", maxDepartmentLen+1)); reason == "" {
		t.Fatalf("部门超过 %d 字应判失败", maxDepartmentLen)
	}
}

// TestCellAt 覆盖 excelize 截掉行尾空列时的越界取值，以及可选列不存在（列号 0）时取空串。
func TestCellAt(t *testing.T) {
	row := []string{"张三", "销售总监"}
	testkit.Equal(t, "第 1 列", cellAt(row, 1), "张三")
	testkit.Equal(t, "第 2 列", cellAt(row, 2), "销售总监")
	testkit.Equal(t, "越界列", cellAt(row, 5), "")
	testkit.Equal(t, "列不存在", cellAt(row, 0), "")
}

// TestImportColumns 覆盖按表头识别列：列序无关、工号等未识别列忽略、表头去首尾空白、缺必需列报错。
func TestImportColumns(t *testing.T) {
	cols, err := importColumns([]string{"角色", " 被评估人姓名 ", "被评估人工号", "评价者姓名"})
	if err != nil {
		t.Fatalf("表头齐全不应报错：%v", err)
	}
	if cols[headerRole] != 1 || cols[headerEvaluateeName] != 2 || cols[headerEvaluatorName] != 4 {
		t.Fatalf("列号映射异常：%v", cols)
	}
	if cols[headerEvaluateeJobTitle] != 0 {
		t.Fatalf("缺失的可选列应映射为 0，实际 %d", cols[headerEvaluateeJobTitle])
	}

	_, err = importColumns([]string{"被评估人姓名", "被评估人工号", "评价者工号", "角色"})
	requireBizCode(t, err, CodeImportHeaderMissing)
}

// TestImportRelationsStoresEvaluateeProfile 导入后被评估人职务/部门按「首个非空值」落库，导出回填同列。
func TestImportRelationsStoresEvaluateeProfile(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, eeB, er := f.emps[0], f.emps[1], f.emps[2]
	svc := New()

	result, err := svc.ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(eeA, er, "销售总监", "", "上级"),
			// 同一被评估人第二行：职务留空不冲突，部门在此补齐。
			importRowOf(eeA, eeB, "", " 华东销售部 ", "平级"),
			// 被评估人 B 职务/部门都不填（可选）。
			importRowOf(eeB, er, "", "", "上级"),
		}),
	})
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if result.Fail != 0 || result.Success != 3 {
		t.Fatalf("期望成功 3 条失败 0 条，实际 %+v", result)
	}

	gotA := evaluateeProfileOf(t, ctx, f.questionnaireID, eeA.EmpID)
	if gotA == nil || *gotA != (evaluateeProfile{JobTitle: "销售总监", Department: "华东销售部"}) {
		t.Fatalf("被评估人 A 职务/部门期望 销售总监/华东销售部，实际 %+v", gotA)
	}
	gotB := evaluateeProfileOf(t, ctx, f.questionnaireID, eeB.EmpID)
	if gotB == nil || *gotB != (evaluateeProfile{}) {
		t.Fatalf("被评估人 B 职务/部门期望为空，实际 %+v", gotB)
	}

	// 导出：模板列 + 两个工号列，被评估人 A 的两行都回填职务/部门。
	data, err := svc.ExportRelations(ctx, f.questionnaireID)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	sheet, err := testkit.ReadXLSXSheet(data)
	if err != nil {
		t.Fatalf("解析导出文件失败：%v", err)
	}
	wantHeader := []string{"被评估人姓名", "被评估人工号", "被评估人职务", "被评估人部门", "评价者姓名", "评价者工号", "角色"}
	if len(sheet.Header) != len(wantHeader) {
		t.Fatalf("导出表头期望 %v，实际 %v", wantHeader, sheet.Header)
	}
	for i, h := range wantHeader {
		testkit.Equal(t, fmt.Sprintf("导出表头第 %d 列", i+1), sheet.Header[i], h)
	}
	var (
		colNo   = columnOf(t, sheet.Header, "被评估人工号")
		colJob  = columnOf(t, sheet.Header, "被评估人职务")
		colDept = columnOf(t, sheet.Header, "被评估人部门")
	)
	exportedA := 0
	for _, row := range sheet.Rows {
		if cellAt(row, colNo) != eeA.EmployeeNo {
			continue
		}
		exportedA++
		testkit.Equal(t, "导出职务", cellAt(row, colJob), "销售总监")
		testkit.Equal(t, "导出部门", cellAt(row, colDept), "华东销售部")
	}
	if exportedA != 2 {
		t.Fatalf("被评估人 A 期望导出 2 行，实际 %d 行", exportedA)
	}
}

// TestImportRelationsRejectsConflictingEvaluateeProfile 同一被评估人不同的非空职务 → 该行失败、整批不写入。
func TestImportRelationsRejectsConflictingEvaluateeProfile(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, eeB, er := f.emps[0], f.emps[1], f.emps[2]

	result, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(eeA, er, "销售总监", "华东销售部", "上级"),
			importRowOf(eeA, eeB, "销售经理", "", "平级"),
		}),
	})
	if err != nil {
		t.Fatalf("导入不应返回错误（冲突走失败清单）：%v", err)
	}
	if result.Success != 0 || result.Fail != 1 || len(result.FailList) != 1 || result.FailList[0].Row != 3 {
		t.Fatalf("期望第 3 行失败且无成功，实际 %+v", result)
	}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, eeA.EmpID); got != nil {
		t.Fatalf("冲突时整批不应写入被评估人，实际 %+v", got)
	}
}

// TestImportRelationsReimportsExportedFile 导出文件（含两个工号列）原样再导入成功：工号列被忽略。
func TestImportRelationsReimportsExportedFile(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, eeB, er := f.emps[0], f.emps[1], f.emps[2]
	svc := New()

	if _, err := svc.ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(eeA, er, "销售总监", "华东销售部", "上级"),
			importRowOf(eeB, er, "", "", "平级"),
		}),
	}); err != nil {
		t.Fatalf("首次导入失败：%v", err)
	}
	data, err := svc.ExportRelations(ctx, f.questionnaireID)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}

	result, err := svc.ImportRelations(ctx, ImportInput{QuestionnaireID: f.questionnaireID, Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("再导入导出文件失败：%v", err)
	}
	if result.Fail != 0 || result.Success != 2 {
		t.Fatalf("再导入期望成功 2 条失败 0 条，实际 %+v", result)
	}
	if n := relationCount(t, ctx, f.questionnaireID); n != 2 {
		t.Fatalf("%s：期望 2 条评价关系，实际 %d", "再导入后评价关系条数", n)
	}
	if got := evaluateeProfileOf(t, ctx, f.questionnaireID, eeA.EmpID); got == nil ||
		*got != (evaluateeProfile{JobTitle: "销售总监", Department: "华东销售部"}) {
		t.Fatalf("再导入后被评估人 A 职务/部门期望保留，实际 %+v", got)
	}
}

// TestImportRelationsRejectsDuplicateName 被评估人姓名有两名在职同名员工（另一租户）→ 该行失败、
// 失败原因点名该姓名与人数，整批不写入。
func TestImportRelationsRejectsDuplicateName(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, eeB, er := f.emps[0], f.emps[1], f.emps[2]
	seedHomonym(t, ctx, 2, eeA.Name, false)

	result, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(eeB, er, "", "", "上级"),
			importRowOf(eeA, er, "", "", "上级"),
		}),
	})
	if err != nil {
		t.Fatalf("重名不应返回错误（走失败清单）：%v", err)
	}
	if result.Success != 0 || result.Fail != 1 || len(result.FailList) != 1 || result.FailList[0].Row != 3 {
		t.Fatalf("期望仅第 3 行失败且无成功，实际 %+v", result)
	}
	wantReason := fmt.Sprintf("被评估人姓名「%s」有 2 名在职同名员工，无法确定是哪一位", eeA.Name)
	testkit.Equal(t, "重名失败原因", result.FailList[0].Reason, wantReason)
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("%s：期望 0 条评价关系，实际 %d", "重名时整批不写入", n)
	}
}

// TestImportRelationsIgnoresDepartedHomonym 离职的同名员工不算重名，按姓名解析到唯一在职员工。
func TestImportRelationsIgnoresDepartedHomonym(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, er := f.emps[0], f.emps[2]
	seedHomonym(t, ctx, 1, er.Name, true)

	result, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader:          buildImportXLSX(t, [][]string{importRowOf(eeA, er, "", "", "上级")}),
	})
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if result.Fail != 0 || result.Success != 1 {
		t.Fatalf("期望成功 1 条，实际 %+v", result)
	}
	var evaluatorEmpID int64
	v, err := dao.Relation.Ctx(ctx).Where(dao.Relation.Columns().QuestionnaireId, f.questionnaireID).
		Value(dao.Relation.Columns().EvaluatorEmpId)
	if err != nil {
		t.Fatalf("读取评价关系失败：%v", err)
	}
	evaluatorEmpID = v.Int64()
	if evaluatorEmpID != er.EmpID {
		t.Fatalf("评价者期望解析为在职员工 %d，实际 %d", er.EmpID, evaluatorEmpID)
	}
}

// TestImportRelationsRejectsUnknownName 姓名找不到在职员工 → 该行失败并点名。
func TestImportRelationsRejectsUnknownName(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA := f.emps[0]

	result, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildImportXLSX(t, [][]string{
			importRowOf(eeA, empcap.Employee{Name: "不存在的人FB32"}, "", "", "上级"),
		}),
	})
	if err != nil {
		t.Fatalf("找不到员工不应返回错误（走失败清单）：%v", err)
	}
	if result.Fail != 1 || len(result.FailList) != 1 {
		t.Fatalf("期望 1 行失败，实际 %+v", result)
	}
	testkit.Equal(t, "找不到员工原因", result.FailList[0].Reason, "评价者「不存在的人FB32」找不到在职员工")
}

// TestImportRelationsRejectsMissingHeader 缺少必需列（旧版只有工号没有角色列）→ 返回表头缺失错误。
func TestImportRelationsRejectsMissingHeader(t *testing.T) {
	ctx := testkit.Setup(t)
	f := seedProfileFixture(t, ctx)
	eeA, er := f.emps[0], f.emps[2]

	_, err := New().ImportRelations(ctx, ImportInput{
		QuestionnaireID: f.questionnaireID,
		Reader: buildXLSX(t, []string{"被评估人姓名", "评价者姓名"}, [][]string{
			{eeA.Name, er.Name},
		}),
	})
	requireBizCode(t, err, CodeImportHeaderMissing)
	if n := relationCount(t, ctx, f.questionnaireID); n != 0 {
		t.Fatalf("%s：期望 0 条评价关系，实际 %d", "缺列时不写入", n)
	}
}
