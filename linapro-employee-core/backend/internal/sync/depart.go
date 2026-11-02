// depart.go 实现全量同步的离职对账：把 Moka 报离职的员工标记为离职。
// 离职员工按工号一次批量回查（MapByEmployeeNosAnyTenant），不在循环内逐条查库；
// 只有真正需要改状态的员工才逐条调用 MarkDeparted（离职人数通常很少）。

package sync

import (
	"context"

	"lina-core/pkg/logger"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// reconcileDeparted 一切以 Moka 为准：只把 Moka 显式报告为离职
// （Status != MokaStatusActive）的员工标记为离职。Moka 未返回的人
// 不在员工范畴内，不做任何处理。
//
// 离职员工部门信息常为空，无法从部门推导租户，故按工号（全公司唯一）
// 跨租户回查员工行，取其在职时已落库的 tenant_id。回查是一次批量查询；
// 批量查询失败时返回 error，由调用方中止本轮对账（下一轮会重试）。
func (s *Syncer) reconcileDeparted(
	ctx context.Context,
	mokaList []MokaEmployee,
	sum *Summary,
) error {
	departedNos := make([]string, 0)
	seen := make(map[string]struct{})
	for _, me := range mokaList {
		if me.Status == MokaStatusActive || me.EmployeeNo == "" {
			continue
		}
		if _, dup := seen[me.EmployeeNo]; dup {
			continue
		}
		seen[me.EmployeeNo] = struct{}{}
		departedNos = append(departedNos, me.EmployeeNo)
	}
	if len(departedNos) == 0 {
		return nil
	}

	byNo, err := s.employees.MapByEmployeeNosAnyTenant(ctx, departedNos)
	if err != nil {
		return err
	}
	// 按 Moka 列表顺序处理，保证日志与标记顺序稳定；未落库的工号直接跳过。
	for _, no := range departedNos {
		emp, ok := byNo[no]
		if !ok || emp.Status == empcap.StatusDeparted {
			continue // 本地无此员工，或已是离职态，无需重复标记
		}
		if err := s.employees.MarkDeparted(ctx, emp.TenantID, emp.EmpID); err != nil {
			logger.Errorf(ctx, "员工目录同步-离职对账: 标记员工离职失败 empID=%d: %v", emp.EmpID, err)
			sum.Errors++
			continue
		}
		sum.Departed++
	}
	return nil
}
