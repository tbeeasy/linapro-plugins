// Package v1 定义 360 测评管理端「评价关系与角色」API 的请求/响应 DTO。
package v1

import "github.com/gogf/gf/v2/frame/g"

// ListRolesReq 列出问卷下的全部角色。
type ListRolesReq struct {
	g.Meta          `path:"/relation/roles" method:"get" tags:"Eval360 Relation" summary:"角色列表" dc:"列出问卷下的全部角色（含权重）。" permission:"eval:360:relation:manage"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ListRolesRes 是角色列表响应。
type ListRolesRes struct {
	List []*RoleItem `json:"list" dc:"角色列表" eg:"[]"`
}

// RoleItem 是角色列表项（与 service 层同构）。
type RoleItem struct {
	Id     int64  `json:"id" dc:"角色 id" eg:"1"`
	Name   string `json:"name" dc:"角色名（由导入生成）" eg:"上级"`
	Weight string `json:"weight" dc:"角色权重（NUMERIC，字符串承载，发布前须各>0 且和=100%）" eg:"40"`
}

// UpdateRoleWeightReq 修改单个角色权重。
type UpdateRoleWeightReq struct {
	g.Meta `path:"/relation/role/{id}/weight" method:"put" tags:"Eval360 Relation" summary:"改角色权重" dc:"修改单个角色权重（发布前由 HR 配置）。" permission:"eval:360:relation:manage"`
	Id     int64  `json:"id" in:"path" v:"required|min:1#缺少角色 id|角色 id 非法" dc:"角色 id"`
	Weight string `json:"weight" v:"required#请填写权重" dc:"权重（NUMERIC，字符串承载）"`
}

// UpdateRoleWeightRes 是空响应。
type UpdateRoleWeightRes struct{}

// RenameRoleReq 重命名角色。
type RenameRoleReq struct {
	g.Meta `path:"/relation/role/{id}/rename" method:"put" tags:"Eval360 Relation" summary:"重命名角色" dc:"重命名角色（关系走 FK 不受影响）。" permission:"eval:360:relation:manage"`
	Id     int64  `json:"id" in:"path" v:"required|min:1#缺少角色 id|角色 id 非法" dc:"角色 id"`
	Name   string `json:"name" v:"required#请填写角色名" dc:"角色名"`
}

// RenameRoleRes 是空响应。
type RenameRoleRes struct{}

// MergeRoleReq 合并角色（from → to）。
type MergeRoleReq struct {
	g.Meta `path:"/relation/role/merge" method:"post" tags:"Eval360 Relation" summary:"合并角色" dc:"把 fromID 名下评价关系改派到 toID 后删除空角色 fromID。" permission:"eval:360:relation:manage"`
	FromId int64 `json:"fromId" v:"required|min:1#缺少源角色 id|源角色 id 非法" dc:"源角色 id"`
	ToId   int64 `json:"toId" v:"required|min:1#缺少目标角色 id|目标角色 id 非法" dc:"目标角色 id"`
}

// MergeRoleRes 是空响应。
type MergeRoleRes struct{}

// DeleteRoleReq 删除角色（cascade=false 时其下仍有评价关系则拒绝）。
type DeleteRoleReq struct {
	g.Meta `path:"/relation/role/{id}" method:"delete" tags:"Eval360 Relation" summary:"删除角色" dc:"删除一个角色。cascade=true 时在单事务内连带删除该角色下的全部评价关系（角色为问卷级，会同时清掉各被评估人在该角色下的关系）；cascade=false 时其下仍有评价关系则拒绝。任一关系已有作答时一律拒绝，需先清空作答。" permission:"eval:360:relation:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少角色 id|角色 id 非法" dc:"角色 id"`
	// Cascade 是否连带删除该角色下的全部评价关系。
	Cascade bool `json:"cascade" dc:"是否连带删除该角色下的全部评价关系" eg:"false"`
}

// DeleteRoleRes 是空响应。
type DeleteRoleRes struct{}

// ImportTemplateReq 下载评价关系全量导入模板。
type ImportTemplateReq struct {
	g.Meta `path:"/relation/import-template" method:"get" tags:"Eval360 Relation" summary:"导入模板" dc:"下载评价关系 Excel 全量导入模板。" permission:"eval:360:relation:manage"`
}

// ImportTemplateRes 是空响应（走文件流）。
type ImportTemplateRes struct{}

// ImportRelationsReq 全量导入评价关系。
type ImportRelationsReq struct {
	g.Meta          `path:"/relation/import" method:"post" mime:"multipart/form-data" tags:"Eval360 Relation" summary:"全量导入评价关系" dc:"仅全量模式、仅发布前允许；任一行失败中止整批。" permission:"eval:360:relation:manage"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ImportRelationsRes 是导入结果。
type ImportRelationsRes struct {
	Success  int              `json:"success" dc:"成功条数" eg:"28"`
	Fail     int              `json:"fail" dc:"失败条数" eg:"2"`
	FailList []ImportFailItem `json:"failList" dc:"失败清单" eg:"[]"`
}

// ImportFailItem 是单行失败回报。
type ImportFailItem struct {
	Row    int    `json:"row" dc:"Excel 行号（含表头）" eg:"3"`
	Reason string `json:"reason" dc:"失败原因" eg:"评价者工号 E999 找不到或已离职"`
}

// ExportRelationsReq 导出评价关系 Excel。
type ExportRelationsReq struct {
	g.Meta          `path:"/relation/export" method:"get" tags:"Eval360 Relation" summary:"导出评价关系" dc:"导出当前问卷评价关系 Excel（未发布时可修改后重新全量导入）。" permission:"eval:360:relation:manage"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ExportRelationsRes 是空响应（走文件流）。
type ExportRelationsRes struct{}

// ListRelationsReq 列出评价关系明细（被评估人×角色×评价者）。
type ListRelationsReq struct {
	g.Meta          `path:"/relation" method:"get" tags:"Eval360 Relation" summary:"评价关系明细" dc:"被评估人×角色×评价者（已评/未评）明细。" permission:"eval:360:relation:manage"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id"`
}

// ListRelationsRes 是评价关系明细响应。
type ListRelationsRes struct {
	Matrix *RelationMatrix `json:"matrix" dc:"评价关系明细" eg:"{...}"`
}

// RelationMatrix 透传 service 层 DTO。
type RelationMatrix struct {
	QuestionnaireID int64           `json:"questionnaireId" dc:"问卷 id" eg:"10"`
	Evaluatees      []*EvaluateeRow `json:"evaluatees" dc:"被评估人明细行" eg:"[]"`
	Roles           []*RoleItem     `json:"roles" dc:"本问卷角色列表" eg:"[]"`
}

// EvaluateeRow 是某被评估人下的评价者明细。
type EvaluateeRow struct {
	EvaluateeID         int64        `json:"evaluateeId" dc:"被评估人内部 id（问卷级）" eg:"5"`
	EvaluateeEmpID      int64        `json:"evaluateeEmpId" dc:"被评估人 EmpID" eg:"1001"`
	EvaluateeName       string       `json:"evaluateeName" dc:"被评估人姓名" eg:"张三"`
	EvaluateeNo         string       `json:"evaluateeNo" dc:"被评估人工号" eg:"E001"`
	EvaluateeJobTitle   string       `json:"evaluateeJobTitle" dc:"被评估人职务（问卷级录入文本，未填写为空串）" eg:"销售总监"`
	EvaluateeDepartment string       `json:"evaluateeDepartment" dc:"被评估人部门（问卷级录入文本，未填写为空串）" eg:"华东销售部"`
	Groups              []*RoleGroup `json:"groups" dc:"按角色分组的评价者列表" eg:"[]"`
}

// RoleGroup 是某角色下的评价者列表。
type RoleGroup struct {
	RoleID   int64           `json:"roleId" dc:"角色 id" eg:"1"`
	RoleName string          `json:"roleName" dc:"角色名" eg:"上级"`
	Items    []*RelationItem `json:"items" dc:"该角色下评价者明细" eg:"[]"`
}

// RelationItem 是单条评价关系。
type RelationItem struct {
	ID             int64  `json:"id" dc:"评价关系 id" eg:"21"`
	EvaluateeID    int64  `json:"evaluateeId" dc:"被评估人内部 id（问卷级）" eg:"5"`
	EvaluatorEmpID int64  `json:"evaluatorEmpId" dc:"评价者 EmpID" eg:"1002"`
	EvaluatorName  string `json:"evaluatorName" dc:"评价者姓名" eg:"李四"`
	EvaluatorNo    string `json:"evaluatorNo" dc:"评价者工号" eg:"E002"`
	RoleID         int64  `json:"roleId" dc:"角色 id" eg:"1"`
	RoleName       string `json:"roleName" dc:"角色名" eg:"上级"`
	Source         string `json:"source" dc:"来源：import=全量导入|manual=编辑器手填" eg:"import"`
	Completed      int    `json:"completed" dc:"是否已填：0=未填|1=已填" eg:"1"`
	DerivedStatus  string `json:"derivedStatus" dc:"派生状态：pending=未评|filled=已评|voided=已作废" eg:"filled"`
}

// SearchEmployeesReq 搜索在职员工（关系编辑器选评价者）。
type SearchEmployeesReq struct {
	g.Meta  `path:"/relation/search-employees" method:"get" tags:"Eval360 Relation" summary:"搜索员工" dc:"按工号或姓名搜索在职员工。" permission:"eval:360:relation:manage"`
	Keyword string `json:"keyword" v:"required#请输入关键字" dc:"工号或姓名关键字"`
}

// SearchEmployeesRes 是员工搜索结果。
type SearchEmployeesRes struct {
	List []*EmployeeOption `json:"list" dc:"员工列表" eg:"[]"`
}

// EmployeeOption 是员工搜索结果项。
type EmployeeOption struct {
	EmpID      int64  `json:"empId" dc:"内部 EmpID" eg:"1002"`
	EmployeeNo string `json:"employeeNo" dc:"工号" eg:"E002"`
	Name       string `json:"name" dc:"姓名" eg:"李四"`
	Department string `json:"department" dc:"部门（暂留空）" eg:""`
}

// AddRelationReq 批量新增评价关系（「新增评价关系」/「编辑」弹窗，只录入姓名不录入工号）。
type AddRelationReq struct {
	g.Meta          `path:"/relation" method:"post" tags:"Eval360 Relation" summary:"新增评价关系" dc:"一次批量新增评价关系（被评估人姓名/职务/部门、评价者姓名、角色，不录入工号）。姓名按在职员工跨租户精确匹配：任一姓名存在在职同名员工或找不到在职员工时整体拒绝、不写入任何数据，错误提示列出全部问题姓名；离职的同名员工不计入重名。评价关系仅新增不更新；被评估人已存在时，非空的职务/部门覆盖原值，留空保留原值。" permission:"eval:360:relation:manage"`
	QuestionnaireId int64 `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"问卷 id" eg:"10"`
	// Items 的条目上限（500）由服务层校验：GoFrame 的 max-length 对切片按 JSON 文本长度计算，不能用来限制条数。
	Items []*AddRelationItem `json:"items" v:"required#请至少填写一条评价关系" dc:"Relations to add, 1 to 500 items per request (more than 500 is rejected with EVAL360_RELATION_ITEMS_TOO_MANY). Nothing is written unless every item passes validation; all items are then written in one transaction." eg:"[{\"evaluateeName\":\"张三\",\"evaluatorName\":\"李四\",\"roleName\":\"上级\"}]"`
}

// AddRelationItem 是一条待新增的评价关系。
type AddRelationItem struct {
	EvaluateeName       string `json:"evaluateeName" v:"required#请填写被评估人姓名" dc:"Evaluatee name; must match exactly one active employee" eg:"张三"`
	EvaluateeJobTitle   string `json:"evaluateeJobTitle" v:"max-length:100#被评估人职务不能超过 100 个字" dc:"Evaluatee job title (optional, at most 100 characters); when empty, an existing evaluatee keeps its current value" eg:"销售总监"`
	EvaluateeDepartment string `json:"evaluateeDepartment" v:"max-length:200#被评估人部门不能超过 200 个字" dc:"Evaluatee department (optional, at most 200 characters); when empty, an existing evaluatee keeps its current value" eg:"华东销售部"`
	EvaluatorName       string `json:"evaluatorName" v:"required#请填写评价者姓名" dc:"Evaluator name; must match exactly one active employee" eg:"李四"`
	RoleName            string `json:"roleName" v:"required#请填写角色" dc:"Role name; normalized, then the existing role is reused or a new one is created" eg:"上级"`
}

// AddRelationRes 返回新评价关系 id（与 items 顺序一致）。
type AddRelationRes struct {
	Ids []int64 `json:"ids" dc:"IDs of the new relations, in the same order as the request items" eg:"[21,22]"`
}

// UpdateRelationRoleReq 修改评价关系的角色（改派）。
type UpdateRelationRoleReq struct {
	g.Meta `path:"/relation/{id}/role" method:"put" tags:"Eval360 Relation" summary:"改派评价关系角色" dc:"修改一条评价关系的角色。" permission:"eval:360:relation:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少评价关系 id|评价关系 id 非法" dc:"评价关系 id"`
	RoleId int64 `json:"roleId" v:"required|min:1#缺少角色 id|角色 id 非法" dc:"目标角色 id"`
}

// UpdateRelationRoleRes 是空响应。
type UpdateRelationRoleRes struct{}

// DeleteRelationReq 删除评价关系。
type DeleteRelationReq struct {
	g.Meta `path:"/relation/{id}" method:"delete" tags:"Eval360 Relation" summary:"删除评价关系" dc:"删除一条评价关系（存在作答时拒绝）。" permission:"eval:360:relation:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少评价关系 id|评价关系 id 非法" dc:"评价关系 id"`
}

// DeleteRelationRes 是空响应。
type DeleteRelationRes struct{}

// DeleteEvaluateeReq 删除一个被评估人及其全部评价关系。
type DeleteEvaluateeReq struct {
	g.Meta `path:"/relation/evaluatee/{id}" method:"delete" tags:"Eval360 Relation" summary:"删除被评估人" dc:"删除一个被评估人及其全部评价关系（受限评价者名单按评价者本人维护，不受影响）；其下任一评价关系已有作答时拒绝。" permission:"eval:360:relation:manage"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少被评估人 id|被评估人 id 非法" dc:"被评估人内部 id（问卷级）" eg:"5"`
}

// DeleteEvaluateeRes 是空响应。
type DeleteEvaluateeRes struct{}
