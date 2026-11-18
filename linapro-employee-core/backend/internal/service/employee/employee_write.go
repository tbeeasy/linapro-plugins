// employee_write.go 实现员工写入网关：Moka 同步的权威档案覆盖、离职标记与新员工建档。
//
// 三个任职日期是每轮快照覆盖语义（design D26）：Moka 未提供时必须把列写成 NULL，
// 否则员工重新入职后旧的离职日期会留在行上，产生「在职 + 有离职日期」的矛盾行。
//
// 为什么档案覆盖用 map 而不是 do.Employee：GoFrame 对 DO 结构体会自动开启
// OmitNilData，nil 字段整列跳过，表达不了「把这一列写成 NULL」。新建行走的是
// INSERT，省略的列直接落列默认值 NULL，因此建档可以用 DO。
package employee

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-core/pkg/logger"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
)

// UpsertEmployee 是 Moka 同步的权威档案覆盖路径。
// 它按 (tenantID, profile.EmployeeNo) 查找；找到则覆盖档案，否则新建行。
// profile.EmployeeNo 必须非空（同步是唯一写入方）。档案更新与部门替换
// 在同一事务中执行。
//
// 在职状态：profile.Departed=false 时把已存在的员工恢复为在职（覆盖重新入职、
// 误标离职被 Moka 纠正等场景）；profile.Departed=true 时不改动已存在员工的状态，
// 置离职统一由 MarkDeparted 负责。新建行按 profile.Departed 直接落在职或离职。
func (s *Service) UpsertEmployee(ctx context.Context, tenantID int, profile empcap.Profile) (int64, error) {
	if profile.EmployeeNo == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	existing, err := s.findLiveByEmployeeNo(ctx, tenantID, profile.EmployeeNo)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return s.create(ctx, tenantID, profile)
	}
	if err = dao.Employee.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		cols := dao.Employee.Columns()
		// 覆盖 Name：Moka 是姓名的权威来源，姓名可能变更（纠正录入错误、
		// 法定改名、曾用名规范化等），且姓名还是飞书回填的 JOIN 键，本地
		// 不跟随更新会导致匹配失效。不写 MokaEmployeeNo：它是本次查询键，
		// 库中值必然已等于 profile.EmployeeNo，再写属恒等空操作。
		updateData := map[string]any{
			cols.Name:             profile.Name,
			cols.OnBoardingDate:   parseDate(ctx, profile.OnBoardingDate),
			cols.EndProbationDate: parseDate(ctx, profile.EndProbationDate),
			cols.LeaveDate:        parseDate(ctx, profile.LeaveDate),
		}
		// Moka 报在职则恢复在职；否则不写 Status，
		// 离职交给 MarkDeparted，避免在本路径里静默改离职。
		if !profile.Departed {
			updateData[cols.Status] = empcap.StatusActive
		}
		if _, uerr := dao.Employee.Ctx(ctx).
			Where(do.Employee{Id: existing.Id}).
			Data(updateData).
			Update(); uerr != nil {
			return uerr
		}
		return s.replaceDepts(ctx, tenantID, existing.Id, profile.Departments)
	}); err != nil {
		return 0, err
	}
	return existing.Id, nil
}

// MarkDeparted 将某员工状态置为离职，并一并写入离职日期。
// leaveDate 为空表示 Moka 未提供离职日期，落 NULL（不能跳过该列，
// 否则重新入职留下的旧日期会与本轮对账结果不一致）。
//
// 与 UpsertEmployee 同理，这里用 map 承载 Data 才能把 leave_date 写成 NULL。
func (s *Service) MarkDeparted(ctx context.Context, tenantID int, empID int64, leaveDate string) error {
	cols := dao.Employee.Columns()
	_, err := dao.Employee.Ctx(ctx).
		Where(do.Employee{TenantId: tenantID, Id: empID}).
		Data(map[string]any{
			cols.Status:    empcap.StatusDeparted,
			cols.LeaveDate: parseDate(ctx, leaveDate),
		}).
		Update()
	return err
}

// create 在单个事务中插入新员工及其部门，从而在部分失败时
// 员工行绝不会被持久化而缺少其部门行。
//
// 用 DO 承载 INSERT：三个任职日期为空时字段为 nil 被 ORM 跳过，
// 该列取列默认值 NULL，与快照覆盖的「空即 NULL」语义一致。
func (s *Service) create(ctx context.Context, tenantID int, profile empcap.Profile) (int64, error) {
	var newID int64
	if err := dao.Employee.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		id, err := dao.Employee.Ctx(ctx).Data(do.Employee{
			TenantId:         tenantID,
			Name:             profile.Name,
			MokaEmployeeNo:   profile.EmployeeNo,
			Status:           profileStatus(profile),
			OnBoardingDate:   parseDate(ctx, profile.OnBoardingDate),
			EndProbationDate: parseDate(ctx, profile.EndProbationDate),
			LeaveDate:        parseDate(ctx, profile.LeaveDate),
		}).InsertAndGetId()
		if err != nil {
			return err
		}
		newID = id
		return s.replaceDepts(ctx, tenantID, id, profile.Departments)
	}); err != nil {
		return 0, err
	}
	return newID, nil
}

// replaceDepts 在调用方的事务内替换某员工的部门行
// （删除 + 批量插入共享该 ctx 事务）。
//
// depts 为空时直接返回、不删除既有部门行：当前同步数据源（Moka）不下发部门，
// 每轮都以空部门调用，若照删会让每名员工每轮多一次无意义的 DELETE，且会抹掉
// 将来由其他来源写入的部门。因此「空」语义为「本次不更新部门」，而非「清空部门」。
func (s *Service) replaceDepts(ctx context.Context, tenantID int, empID int64, depts []empcap.Department) error {
	if len(depts) == 0 {
		return nil
	}
	if _, err := dao.EmployeeDept.Ctx(ctx).Where(do.EmployeeDept{EmpId: empID}).Delete(); err != nil {
		return err
	}
	rows := make([]do.EmployeeDept, 0, len(depts))
	for _, d := range depts {
		rows = append(rows, do.EmployeeDept{
			TenantId:         tenantID,
			EmpId:            empID,
			LarkDepartmentId: d.LarkDepartmentID,
			IsPrimary:        boolToInt(d.IsPrimary),
		})
	}
	_, err := dao.EmployeeDept.Ctx(ctx).Data(rows).Insert()
	return err
}

// profileStatus 返回新建员工行应落的在职状态。
func profileStatus(p empcap.Profile) int {
	if p.Departed {
		return empcap.StatusDeparted
	}
	return empcap.StatusActive
}

// dateLayout 是任职日期在 empcap 契约与库之间的统一格式。
// 日期无时区语义，只按日历日传递。
const dateLayout = "2006-01-02"

// parseDate 把 empcap 契约里的日期字符串转为可写入 DATE 列的值：
// 空字符串或非法格式返回 nil（落库为 NULL），合法日期按 UTC 解析。
//
// 按 UTC 而非 time.Local 解析：DATE 列没有时区，用本地时区构造的时刻一旦
// 被驱动或下游按 UTC 折算，可能整体退一天（+08 的 2021-03-01 00:00 折成
// UTC 是 2021-02-28 16:00）。UTC 下日期分量与输入一致，折算不会偏移。
//
// 非法格式只记 warning 不报错——单个员工日期异常不应中断整轮同步。
func parseDate(ctx context.Context, value string) *time.Time {
	if value == "" {
		return nil
	}
	t, err := time.ParseInLocation(dateLayout, value, time.UTC)
	if err != nil {
		// 日志文本遵循后端 i18n 治理规则用英文（用户可见文案不得硬编码中文）。
		logger.Warningf(ctx, "employee employment date is malformed, storing NULL: value=%q: %v", value, err)
		return nil
	}
	return &t
}
