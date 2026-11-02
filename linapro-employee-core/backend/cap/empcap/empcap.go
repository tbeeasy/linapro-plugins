// Package empcap 是 linapro-employee-core 的公开门面。其他数据源插件
// （考勤、问卷）通过 go.mod 的 `replace` 指令导入它，以获得员工认证中间件、
// 每请求的员工身份（EmpContext）以及只读的员工查询。它从不导入宿主机
// 能力注册表：接缝是一个纯 Go 包加一个进程内绑定的服务，
// 与 linapro-extlogin-core/backend/cap/extidcap 对齐。
//
// 认证与授权：EmpContext 只携带身份（员工是谁）。员工能看见什么
// （数据权限）由各消费方自行决定，这里有意不涉及。
package empcap

import "context"

// OwnerPluginID 是拥有并绑定员工服务的插件。
const OwnerPluginID = "linapro-employee-core"

// 员工行上存储的在职状态值。
const (
	StatusDeparted = 0
	StatusActive   = 1
)

// lark_identity 行的身份来源取值。own=本租户原生身份（contact.User.List /
// FindByDepartment），可作登录/考勤反查凭据；assoc=关联组织视角身份
// （directory.collaboration_share_entity.list），仅为写多维表格人员字段采集，
// MUST NOT 参与登录/反查（Resolve* 一律加 own 过滤）。
const (
	IDSourceOwn   = "own"
	IDSourceAssoc = "assoc"
)

// EmpContext 是 EmpAuth 注入的每请求员工身份。
// 仅含身份 —— 无 scope、无部门、无数据权限判定。
type EmpContext struct {
	EmpID    int64
	TenantID int
}

// Department 是员工的一条部门归属（飞书原始部门 id）。
type Department struct {
	LarkDepartmentID string
	IsPrimary        bool
}

// Employee 是查询返回的读模型。飞书身份已不再是员工行的一部分；
// 它位于 lark_identity 映射表中（一个人可能跨多个飞书企业），
// 因此该模型不携带 open_id。
type Employee struct {
	EmpID       int64
	TenantID    int
	EmployeeNo  string // Moka 员工工号；非 Moka 来源时为空
	Name        string
	Status      int
	Departments []Department
}

// Active 报告该员工当前是否在职。
func (e *Employee) Active() bool { return e != nil && e.Status == StatusActive }

// Profile 是由 Moka 同步路径写入的权威员工数据。
// EmployeeNo 驱动 (tenant_id, moka_employee_no) 去重键。
type Profile struct {
	Name       string
	EmployeeNo string // Moka HR 员工工号；驱动同步路径的去重键
	// Departments 非空时整体替换该员工的部门；为空表示本次不更新部门（保留既有部门行），
	// 而不是清空部门。
	Departments []Department
	// Departed 表示 Moka 报告该员工已离职。零值 false 即在职：UpsertEmployee 会把
	// 已存在的员工恢复为在职；为 true 时不改动已存在员工的状态（置离职统一走
	// MarkDeparted），新建行则直接落离职。
	Departed bool
}

// Service 是拥有方插件实现、其他插件消费的宽契约。拥有方是员工表的
// 唯一写入方；所有写入都经由该网关，从而在一处强制 EmpID 分配、
// open_id 去重以及状态状态机。
type Service interface {
	// UpsertEmployee 是 Moka 同步使用的权威档案覆盖路径。
	// 它以 (tenantID, profile.EmployeeNo) 为键创建或更新行，始终写入 profile。
	// profile.EmployeeNo 必须非空；同步是员工行的唯一写入方。
	// profile.Departed=false 时会把已离职的员工恢复为在职；为 true 时不改动已存在
	// 员工的状态（置离职由 MarkDeparted 负责），新建行直接落离职。
	UpsertEmployee(ctx context.Context, tenantID int, profile Profile) (empID int64, err error)

	// MarkDeparted 将员工状态置为离职。
	MarkDeparted(ctx context.Context, tenantID int, empID int64) error

	// GetByID 按内部 EmpID 返回一名员工（含部门）。
	GetByID(ctx context.Context, empID int64) (*Employee, error)

	// GetByEmployeeNo 按 (tenantID, Moka 员工工号) 返回一名员工。
	// 供 report-sync 和 recruit-pipeline 从工号解析员工而无需走登录路径。
	GetByEmployeeNo(ctx context.Context, tenantID int, employeeNo string) (*Employee, error)

	// MapByEmployeeNos 是按工号集合的批量只读查询：一次查询返回「工号 → 员工」映射，
	// 供消费方把一批工号一次性解析为员工（姓名/工号/EmpID/在职状态），避免在循环内逐条
	// 调 GetByEmployeeNo 造成 N+1。仅在传入的 tenantID 范围内匹配（工号在租户内唯一，
	// 与 GetByEmployeeNo 的 (tenantID, employeeNo) 语义一致）。空集合直接返回空 map；
	// 未命中的工号不入 map（调用方据此判缺失）。不排除离职员工——在职判定由消费方按
	// Status 自行决定（与 GetByEmployeeNo 行为对齐：后者返回行后由调用方判 Active()）。
	MapByEmployeeNos(ctx context.Context, tenantID int, employeeNos []string) (map[string]*Employee, error)

	// MapByEmpIDs 是按 EmpID 集合的批量只读查询：一次查询返回「EmpID → 员工」映射，
	// 供消费方把一批 EmpID 一次性解析为员工（姓名/工号），避免逐条调 GetByID 的 N+1。
	// 不带租户过滤——EmpID 是全局代理主键，跨租户唯一；与 GetByID 行为对齐。
	// 空集合直接返回空 map；未命中的 EmpID 不入 map。不排除离职员工。
	MapByEmpIDs(ctx context.Context, empIDs []int64) (map[int64]*Employee, error)

	// GetByEmployeeNoAnyTenant 按 Moka 工号跨租户返回一名员工。Moka 工号全公司
	// 唯一，故无需租户即可定位。供离职对账在部门信息缺失（离职员工部门常为空、
	// 无法从部门推导租户）时按工号回查员工行，取其在职时已落库的 tenant_id。
	// 无匹配时返回 ErrEmployeeNotFound。
	GetByEmployeeNoAnyTenant(ctx context.Context, employeeNo string) (*Employee, error)

	// MapByEmployeeNosAnyTenant 是 MapByEmployeeNos 的跨租户批量版本：一次查询返回
	// 「工号 → 员工」映射，但不按 tenantID 过滤。Moka 工号全公司唯一（与
	// GetByEmployeeNoAnyTenant 语义一致），故无需租户即可批量定位；供消费方在
	// 无法（或不应）预先确定租户时把一批工号一次性解析为员工，避免逐条调
	// GetByEmployeeNoAnyTenant 造成 N+1。空集合直接返回空 map；未命中的工号不入
	// map。不排除离职员工——在职判定由消费方按 Status 自行决定。
	MapByEmployeeNosAnyTenant(ctx context.Context, employeeNos []string) (map[string]*Employee, error)

	// MapByNamesAnyTenant 按姓名集合跨租户批量查询：一次查询返回「姓名 → 同名全部员工」映射，
	// 每个姓名下的员工按 EmpID 升序。姓名按原值精确匹配（不做大小写或空白归一，归一由消费方
	// 在传入前完成）。供消费方按姓名录入员工时一次性解析并自行判定重名，避免逐条查询的 N+1。
	// 同一自然人在多个租户各有一行（工号相同）时会全部返回，由消费方按工号去重。
	// 空集合直接返回空 map；未命中的姓名不入 map；不排除离职员工——在职判定由消费方按 Status 决定。
	MapByNamesAnyTenant(ctx context.Context, names []string) (map[string][]*Employee, error)

	// GetByName 按 (tenantID, 姓名) 返回一名员工。每个租户内姓名唯一
	// 是业务不变式；调用方依赖这一点。
	GetByName(ctx context.Context, tenantID int, name string) (*Employee, error)

	// ListActiveByTenant 返回某租户下所有在职（status=1）员工。
	// 供同步任务的离职对账阶段查找过期的在职行。
	ListActiveByTenant(ctx context.Context, tenantID int) ([]*Employee, error)

	// ResolveEmpIDByLarkIdentity 返回由飞书身份 (larkAppID, openID)
	// 映射的 EmpID。当该组合未注册时返回 ErrEmployeeNotFound ——
	// 登录按失败关闭处理，绝不自动开通。
	ResolveEmpIDByLarkIdentity(ctx context.Context, larkAppID, openID string) (empID int64, err error)

	// ResolveEmpIDByLarkUserID 返回由飞书身份 (larkAppID, userID) 映射的 EmpID。
	// user_id 是企业级标识，考勤等只按 user_id 推送（不带 open_id）的数据源
	// 用它反查员工。larkAppID 定位到具体飞书企业，与 user_id 组成双键避免
	// 跨企业 user_id 碰撞。未注册或该行 user_id 为空时返回 ErrEmployeeNotFound。
	ResolveEmpIDByLarkUserID(ctx context.Context, larkAppID, userID string) (empID int64, err error)

	// UpsertLarkIdentity 写入同步回填使用的 (larkAppID, openID) → empID 映射，
	// 并一并落库同一条通讯录记录携带的 userID（企业级 user_id，供考勤反查；
	// 应用无 user_id 权限或 assoc 行时传空字符串）与 idSource（own/assoc，
	// 见 IDSourceOwn/IDSourceAssoc）。用相同键重复执行会更新目标 empID、
	// userID 与 idSource。
	UpsertLarkIdentity(ctx context.Context, larkAppID, openID, userID, idSource string, empID int64) error

	// MapLarkOpenIDsByEmployeeNo 以 Moka 员工工号（employee.moka_employee_no）为查找键，
	// 返回「工号 → 该 app 作用域下该人全部 open_id」的映射：JOIN employee 与 lark_identity、
	// 按 larkAppID 过滤、跨所有租户、不限在职、不按 id_source 过滤（own + assoc 都取，双租户
	// 同人的多个 open_id 全返回）。工号为空的员工行不入 map。此方法供写多维表格人员字段的消费方
	// 在周期开始时构建一次内存映射，把「工号 → open_id」解析下沉到内存查找，避免逐人查库的
	// N+1（消费方经门面 LarkOpenIDResolverByEmployeeNos 复用该批量结果）。供 recruit-pipeline
	// 写「简历归属者」人员字段（归属人工号取自 Moka pushCandidate 事件 data.owners.employee_id）。
	//
	// 工号查找侧第一跳精确、无姓名归一/同名歧义；但工号→emp_id 精确后，emp_id→open_id 仍依赖
	// 姓名 JOIN 回填已落库的 lark_identity（若该 HR 飞书通讯录名与 Moka 员工名未对上则该工号返回
	// 空，消费方降级跳过该列）。open_id 按应用作用域签发，必须传入写表格的 larkAppID 作过滤，
	// 混入其它应用作用域的 id 写入即错。无任何映射时返回空 map。
	MapLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string][]string, error)

	// MapOwnLarkOpenIDsByEmployeeNo 是面向**出站消息推送**的只读寻址契约：以 Moka 工号为键，
	// 返回「工号 → 该 app 作用域下 id_source='own' 的 open_id」映射。它与 MapLarkOpenIDsByEmployeeNo
	// 的关键区别在于：推送场景 MUST 只认 own——assoc 身份为他企业视角、无法经本应用投递消息，
	// 必须排除。按 larkAppID 作用域过滤（open_id 按应用作用域签发），不暴露应用密钥，
	// 不返回 assoc 行。无任何映射时返回空 map。供 linapro-eval-360 遍历各 app 取 own 映射、
	// 用该 app 凭据当场发送飞书卡片。
	MapOwnLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string]string, error)

	// MapOwnLarkUserIDsByEmployeeNo 是 MapOwnLarkOpenIDsByEmployeeNo 的 user_id 版本，
	// 供**跨企业推送**使用：user_id 按企业签发，因此「在某身份应用下同步到的 lark_user_id」
	// 可以直接交给**同企业**的推送应用投递——换凭据即换机器人，无需换标识。
	//
	// 一次查询取出该企业全部 own 身份的「工号 + user_id」，按工号归入索引的三个集合之一
	// （见 LarkUserIDIndex）。某工号不在任何集合里，表示此人在该企业没有 own 身份。
	// 工号为空的行无法按工号寻址，直接忽略。无任何 own 行时返回三个空集合（非 nil）；
	// 数据库查询失败时原样返回错误。
	MapOwnLarkUserIDsByEmployeeNo(ctx context.Context, larkAppID string) (LarkUserIDIndex, error)
}

// LarkUserIDIndex 是某个飞书企业的「工号 → user_id」寻址索引，由 MapOwnLarkUserIDsByEmployeeNo 返回。
//
// 该企业每个有 own 身份的工号恰好落在下面三个集合之一（互不相交）；三个都不在，表示此人在该企业
// 没有 own 身份。消费方按收件人本人的工号逐个查，不得用整个企业的情况推断某个人：例如企业里有别人
// 缺 user_id，不代表此人也缺。需要人数时直接取集合长度。
//
// 例：某企业查出 (E1,u1) (E2,空) (E3,u3a) (E3,u3b) 四行，则 ByEmployeeNo={E1:u1}，
// BlankEmployeeNos={E2}，CollidedEmployeeNos={E3}；不在其中的 E5 即「不在该企业」。
type LarkUserIDIndex struct {
	// ByEmployeeNo 是可投递的人：工号 → 企业级 user_id。
	ByEmployeeNo map[string]string
	// BlankEmployeeNos 是有 own 身份但 lark_user_id 为空的工号：身份应用缺「获取用户 user_id」
	// 权限，或该行早于授权、等待下次同步回填。整个企业都在这里通常是部署缺陷，须告警。
	BlankEmployeeNos map[string]struct{}
	// CollidedEmployeeNos 是同一工号对应多个不同 user_id 的工号（一人多个账号或工号填重）。
	// 不知道该发给哪个账号，推送时宁可标记失败也不取其一，避免送错人。
	CollidedEmployeeNos map[string]struct{}
}

// empCtxKey 是 EmpContext 的私有上下文键。
type empCtxKey struct{}

// WithEmpContext 返回携带员工身份的 ctx 副本。
func WithEmpContext(ctx context.Context, c EmpContext) context.Context {
	return context.WithValue(ctx, empCtxKey{}, c)
}

// EmpFromContext 提取 EmpAuth 注入的员工身份。
func EmpFromContext(ctx context.Context) (EmpContext, bool) {
	c, ok := ctx.Value(empCtxKey{}).(EmpContext)
	return c, ok
}
