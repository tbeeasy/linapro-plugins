// Package relation 实现 360 测评「评价关系与角色」管理端服务（eval-relation）。
// 它拥有 role / evaluatee / relation 三张表的写入，并消费 linapro-employee-core
// 的 empcap 做工号→EmpID 解析。本插件为平台级：问卷对所有租户员工通用，不做租户隔离。
//
// 设计要点（见 openspec/changes/linapro-eval-360/design.md）：
//   - 角色由导入生成：无内置角色；导入「角色」列按归一化后精确匹配 upsert，默认权重 0。
//   - 仅全量导入、仅发布前允许；发布后禁止全量导入，追加走关系编辑器弹窗。
//   - 评价者/被评估人以姓名录入（不录入工号），经 empcap.MapByNamesAnyTenant 批量解析为唯一在职员工的 EmpID；
//     在职重名或找不到即拒绝。
//   - 唯一键下沉到问卷级（含 questionnaire_id），由数据库唯一索引兜底。
package relation

import (
	"context"
	"io"

	"github.com/shopspring/decimal"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// 关系来源标记。
const (
	SourceImport = "import" // 全量导入
	SourceManual = "manual" // 关系编辑器手动新增
)

// 问卷发布状态（与 questionnaire 模块保持一致：0=未发布 1=已发布）。
const (
	publishedNo  = 0
	publishedYes = 1
)

// Service 定义评价关系与角色的租户级管理端能力。
type Service interface {
	// ListRoles 返回问卷下的全部角色（按 id 升序）。
	ListRoles(ctx context.Context, questionnaireID int64) ([]*RoleItem, error)
	// UpdateRoleWeight 修改单个角色权重（发布前由 HR 配置；发布前校验见 questionnaire 模块）。
	UpdateRoleWeight(ctx context.Context, roleID int64, weight string) error
	// RenameRole 重命名角色（改实体名，关系走 FK 不受影响）。
	RenameRole(ctx context.Context, roleID int64, name string) error
	// MergeRole 合并角色：把 fromID 名下的评价关系改派到 toID，再删除空角色 fromID。
	MergeRole(ctx context.Context, fromID, toID int64) error
	// DeleteRole 删除问卷下的一个角色。cascade=false 时其下仍有评价关系即拒绝
	// （引导先改派/删除关系）；cascade=true 时在单事务内连带删除该角色下的全部评价关系
	// （角色是问卷级的，会同时清掉各个被评估人在该角色下的关系）。
	// 任一评价关系已有作答时一律拒绝（引导先清空作答）。
	DeleteRole(ctx context.Context, roleID int64, cascade bool) error

	// GenerateImportTemplate 生成评价关系 Excel 全量导入模板（列：被评估人姓名/被评估人职务/被评估人部门/
	// 评价者姓名/角色；不含工号，职务、部门可选）。
	GenerateImportTemplate(ctx context.Context) ([]byte, error)
	// ImportRelations 全量导入评价关系：按表头识别列（工号等未识别列忽略），姓名按在职员工解析，
	// 找不到或在职重名判该行失败并点名；先校验全部行，任一行失败即中止整批不写入；
	// 全部通过后在单事务内 upsert 角色、被评估人、评价关系（已发布的问卷拒绝导入）。
	// 缺少必需列返回 CodeImportHeaderMissing。
	// 同一被评估人多行的职务/部门取首个非空值；出现不同的非空值视为冲突，判该行失败。
	// 返回成功条数与失败清单（行号+原因）。
	ImportRelations(ctx context.Context, in ImportInput) (*ImportResult, error)
	// ExportRelations 导出当前问卷的评价关系 Excel（导入模板各列 + 被评估人/评价者工号，便于核对；
	// 再导入时工号列被忽略）。
	ExportRelations(ctx context.Context, questionnaireID int64) ([]byte, error)

	// ListRelations 返回问卷下「被评估人 × 角色 × 评价者（已评/未评）」明细。
	ListRelations(ctx context.Context, questionnaireID int64) (*RelationMatrix, error)
	// SearchEmployees 按工号或姓名搜索在职员工，供关系编辑器新增评价者使用。
	SearchEmployees(ctx context.Context, keyword string) ([]*EmployeeOption, error)
	// AddRelations 经「新增评价关系」/「编辑」弹窗一次批量新增评价关系（只录入姓名，不录入工号）。
	// 姓名按在职员工跨租户解析：任一姓名在职重名（CodeEmployeeNameDuplicate）或找不到
	// （CodeEmployeeNameNotFound）即整体拒绝、零写入，错误参数 names 列出全部问题姓名；
	// 条目为空返回 CodeRelationItemsEmpty，姓名/角色为空返回 CodeEmployeeNameEmpty/CodeRoleNameEmpty，
	// 自评返回 CodeImportSelfEvaluate。全部通过后单事务写入并按条目顺序返回新评价关系 id。
	// 被评估人不存在时按条目职务/部门新建；已存在时非空职务/部门覆盖原值、空值保留原值。
	AddRelations(ctx context.Context, in AddRelationsInput) ([]int64, error)
	// UpdateRelationRole 修改一条评价关系的角色（改派）。
	UpdateRelationRole(ctx context.Context, relationID, roleID int64) error
	// DeleteRelation 删除一条评价关系（存在作答时拒绝，引导二次确认/清空作答）。
	DeleteRelation(ctx context.Context, relationID int64) error
	// DeleteEvaluatee 删除一个被评估人及其全部评价关系（受限评价者名单按评价者本人维护，不受影响）；
	// 其下任一评价关系已有作答时拒绝，引导先清空作答。
	DeleteEvaluatee(ctx context.Context, evaluateeID int64) error
}

// 编译期断言 serviceImpl 实现 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现，依赖 empcap 员工查询。
type serviceImpl struct{}

// New 构造评价关系服务。empcap 经 BoundEmployeeService() 懒解析，故无需注入。
func New() Service {
	return &serviceImpl{}
}

// empSvc 返回 empcap 绑定的员工服务，未绑定时返回 nil（fail-closed）。
func (s *serviceImpl) empSvc() empcap.Service {
	return empcap.BoundEmployeeService()
}

// ---------------------------------------------------------------------------
// 输入/输出 DTO
// ---------------------------------------------------------------------------

// RoleItem 是角色列表项；Weight 以字符串承载 NUMERIC。
type RoleItem struct {
	Id     int64  `json:"id"`
	Name   string `json:"name"`
	Weight string `json:"weight"`
}

// ImportInput 是全量导入入参。
type ImportInput struct {
	QuestionnaireID int64
	// Reader 指向上传的 Excel 文件（.xlsx）。
	Reader io.Reader
}

// ImportResult 是全量导入结果（成功条数 + 失败清单）。
type ImportResult struct {
	Success  int              `json:"success"`
	Fail     int              `json:"fail"`
	FailList []ImportFailItem `json:"failList"`
}

// ImportFailItem 是单行失败回报（行号+原因）。
type ImportFailItem struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// RelationMatrix 是「被评估人 × 角色 × 评价者」明细视图。
type RelationMatrix struct {
	QuestionnaireID int64           `json:"questionnaireId"`
	Evaluatees      []*EvaluateeRow `json:"evaluatees"`
	Roles           []*RoleItem     `json:"roles"`
}

// EvaluateeRow 是某被评估人下的评价者明细（按角色分组）。
type EvaluateeRow struct {
	EvaluateeID    int64  `json:"evaluateeId"`
	EvaluateeEmpID int64  `json:"evaluateeEmpId"`
	EvaluateeName  string `json:"evaluateeName"`
	EvaluateeNo    string `json:"evaluateeNo"`
	// EvaluateeJobTitle / EvaluateeDepartment 为被评估人职务/部门（问卷级录入文本，可空）。
	EvaluateeJobTitle   string       `json:"evaluateeJobTitle"`
	EvaluateeDepartment string       `json:"evaluateeDepartment"`
	Groups              []*RoleGroup `json:"groups"`
}

// RoleGroup 是某角色下的评价者列表（区分已评/未评）。
type RoleGroup struct {
	RoleID   int64           `json:"roleId"`
	RoleName string          `json:"roleName"`
	Items    []*RelationItem `json:"items"`
}

// RelationItem 是单条评价关系。
type RelationItem struct {
	ID             int64  `json:"id"`
	EvaluateeID    int64  `json:"evaluateeId"`
	EvaluatorEmpID int64  `json:"evaluatorEmpId"`
	EvaluatorName  string `json:"evaluatorName"`
	EvaluatorNo    string `json:"evaluatorNo"`
	RoleID         int64  `json:"roleId"`
	RoleName       string `json:"roleName"`
	Source         string `json:"source"`
	Completed      int    `json:"completed"`     // 0=未填 1=已填
	DerivedStatus  string `json:"derivedStatus"` // pending/filled/voided（派生）
}

// EmployeeOption 是员工搜索结果项（供关系编辑器选择评价者）。
type EmployeeOption struct {
	EmpID      int64  `json:"empId"`
	EmployeeNo string `json:"employeeNo"`
	Name       string `json:"name"`
	Department string `json:"department"`
}

// maxAddRelationItems 是单次批量新增评价关系的条目上限；只在服务层校验（GoFrame 的 max-length 对切片按 JSON 文本长度计算，不能用于限制条数）。
const maxAddRelationItems = 500

// AddRelationsInput 是批量新增评价关系入参。
type AddRelationsInput struct {
	QuestionnaireID int64
	Items           []AddRelationItem
}

// AddRelationItem 是一条待新增的评价关系（只录入姓名，不录入工号）。
type AddRelationItem struct {
	EvaluateeName string // 被评估人姓名（须唯一对应一名在职员工）
	// EvaluateeJobTitle / EvaluateeDepartment 为被评估人职务/部门（可空；空值不覆盖已有被评估人的原值）。
	EvaluateeJobTitle   string
	EvaluateeDepartment string
	EvaluatorName       string // 评价者姓名（须唯一对应一名在职员工）
	RoleName            string // 角色名（归一化后查已有，没有才建）
}

// weightZero 是默认角色权重（导入生成时）。
var weightZero = decimal.Zero
