// Package sync 实现 Moka 驱动的员工目录全量同步任务。
// 不直接依赖 linapro-moka-hcm；plugin.go 中的生产适配器把
// hcm.HCMEmployee 转换为 sync.MokaEmployee。Moka 是员工存在性的权威；
// 每个配置的飞书应用（飞书企业）贡献写入 lark_identity 表的 open_id 映射。
package sync

import (
	"context"
	"lina-core/pkg/logger"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// Moka 员工在职状态取值。
const (
	MokaStatusDeparted = 0 // 离职
	MokaStatusActive   = 1 // 在职
)

// MokaEmployee 是同步包对 Moka 员工记录的表达。
type MokaEmployee struct {
	EmployeeNo string
	Realname   string
	// Department 是 Moka 下发的完整部门路径，形如
	// "广州星嘉/公共部门/公共部门/总经办/CEO"。租户映射只取其顶层
	// 段（第一个 "/" 之前），见 topLevelDept。
	Department     string
	OnBoardingDate string // 入职日期，格式 YYYY-MM-DD
	Status         int    // 取值见 MokaStatusActive / MokaStatusDeparted
}

// topLevelDept 从 Moka 的完整部门路径中截取顶层部门名，即第一个 "/"
// 之前的段，并去除首尾空白。dept_tenant_map 以顶层部门名（而非部门 ID）
// 作为键匹配内部租户。示例："广州星嘉/公共部门/总经办/CEO" → "广州星嘉"。
func topLevelDept(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		path = path[:i]
	}
	return strings.TrimSpace(path)
}

// LarkUser 是姓名 JOIN 步骤所需的最小飞书身份。
type LarkUser struct {
	Name       string
	EmployeeNo string // 飞书通讯录的 employee_no 扩展字段（工号），优先用于匹配
	OpenID     string
	UserID     string // 飞书 user_id（企业级）；应用无 user_id 权限或 assoc 行时为空，供考勤按 user_id 反查
	// Source 标识身份来源：own=本租户原生成员，assoc=关联组织视角成员。
	// 空值按 own 处理。取值见 empcap.IDSourceOwn / IDSourceAssoc。
	Source string
}

// MokaFetcher 抽象 Moka 的全量员工列表调用。
type MokaFetcher interface {
	ListAllEmployees(ctx context.Context) ([]MokaEmployee, error)
}

// LarkAppFetcher 枚举一个飞书企业本租户的通讯录。每个已配置的
// lark_app_id 对应一个 fetcher；AppID 标识返回的 open_id 属于哪个企业，
// 以便映射到正确的 lark_identity 行。
type LarkAppFetcher interface {
	AppID() string
	ListUsers(ctx context.Context) ([]LarkUser, error)
}

// empInfo 携带“无飞书身份”告警所用的展示字段。
type empInfo struct {
	name     string
	tenantID int
}

// Syncer 编排完整的员工目录同步。
type Syncer struct {
	employees    empcap.Service
	mokaFetcher  MokaFetcher
	larkFetchers []LarkAppFetcher
	deptMap      map[string]int
}

// NewSyncer 构造 Syncer。larkFetchers 为每个已配置的 lark_app_id 保存
// 一个 fetcher；空切片表示不执行飞书回填（仅 Moka 同步）。
func NewSyncer(employees empcap.Service, mokaFetcher MokaFetcher, larkFetchers []LarkAppFetcher) *Syncer {
	return &Syncer{employees: employees, mokaFetcher: mokaFetcher, larkFetchers: larkFetchers}
}

// Summary 保存每次运行的计数器。
type Summary struct {
	Upserted   int
	Backfilled int
	Departed   int
	Warnings   int
	Errors     int
}

// RunFullSync 执行完整同步。
func (s *Syncer) RunFullSync(ctx context.Context, deptTenantMap map[string]int) (*Summary, error) {
	if len(deptTenantMap) == 0 {
		return nil, gerror.New("员工目录同步: 部门-租户映射为空")
	}
	s.deptMap = deptTenantMap
	sum := &Summary{}

	mokaList, err := s.mokaFetcher.ListAllEmployees(ctx)
	if err != nil {
		return nil, gerror.Wrap(err, "员工目录同步: 拉取Moka员工列表失败")
	}
	logger.Infof(ctx, "员工目录同步: 从Moka拉取到 %d 名员工", len(mokaList))

	// 先按入职日期排序，同一入职日期内再按工号排序。
	// OnBoardingDate 为 YYYY-MM-DD，字典序等价于日期先后。
	sort.Slice(mokaList, func(i, j int) bool {
		if mokaList[i].OnBoardingDate != mokaList[j].OnBoardingDate {
			return mokaList[i].OnBoardingDate < mokaList[j].OnBoardingDate
		}
		return mokaList[i].EmployeeNo < mokaList[j].EmployeeNo
	})

	// employeeNoToEmpID 把 Moka 工号映射到其 EmpID（跨所有租户）。
	// 工号全公司唯一，无冲突问题。
	employeeNoToEmpID := map[string]int64{}
	activeMeta := map[int64]empInfo{}

	for _, me := range mokaList {
		if me.EmployeeNo == "" {
			logger.Warningf(ctx, "员工目录同步: 工号为空 name=%q — 已跳过", me.Realname)
			sum.Warnings++
			continue
		}
		dept := topLevelDept(me.Department)
		tenantID, ok := s.deptMap[dept]
		if !ok {
			// 离职员工部门常为空、无法映射租户，这类在主循环跳过是正常的：
			// 其离职态由 reconcileDeparted 阶段 1 按工号回查兜底，无需在此告警。
			// 仅对在职员工的未映射部门告警——那才是需要人工关注的配置缺口。
			if me.Status == MokaStatusActive {
				logger.Warningf(ctx, "员工目录同步: 工号=%q 部门=%q (来自 %q) 未在映射中 — 已跳过", me.EmployeeNo, dept, me.Department)
				sum.Warnings++
			}
			continue
		}
		// Departed 透传 Moka 在职状态：在职员工被恢复为在职（重新入职、误标离职纠正），
		// 离职员工的状态改动仍交给 reconcileDeparted 的 MarkDeparted。
		empID, err := s.employees.UpsertEmployee(ctx, tenantID, empcap.Profile{
			Name:       me.Realname,
			EmployeeNo: me.EmployeeNo,
			Departed:   me.Status != MokaStatusActive,
		})
		if err != nil {
			logger.Errorf(ctx, "员工目录同步: 写入员工失败 工号=%q: %v", me.EmployeeNo, err)
			sum.Errors++
			continue
		}
		sum.Upserted++
		if me.Status == MokaStatusActive {
			activeMeta[empID] = empInfo{name: me.Realname, tenantID: tenantID}
			// 工号映射
			employeeNoToEmpID[me.EmployeeNo] = empID
		}
	}

	matched := s.backfillIdentities(ctx, employeeNoToEmpID, sum)
	for empID, info := range activeMeta {
		if !matched[empID] {
			// 飞书侧没有匹配到该员工，记录告警（仅输出姓名供人工识别）
			logger.Warningf(ctx, "员工目录同步: 员工缺少飞书身份 name=%q tenant=%d",
				info.name, info.tenantID)
			sum.Warnings++
		}
	}

	if err := s.reconcileDeparted(ctx, mokaList, sum); err != nil {
		return sum, gerror.Wrap(err, "员工目录同步: 离职对账失败")
	}

	logger.Infof(ctx, "员工目录同步完成: upserted=%d backfilled=%d departed=%d warnings=%d errors=%d",
		sum.Upserted, sum.Backfilled, sum.Departed, sum.Warnings, sum.Errors)
	return sum, nil
}
