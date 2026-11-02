// lark.go 实现按应用的飞书通讯录回填：针对每个配置的飞书企业，
// 枚举该应用本租户（own）与关联组织（assoc）的通讯录，按归一化姓名把
// 用户 JOIN 到员工，并对每个 open_id 各 upsert 一行 (lark_app_id, open_id) → emp_id
// 映射（own/assoc 分别落 id_source）。同一人的跨应用/跨通道重复是预期的，
// 会为每个 open_id 生成一行 lark_identity，全部指向同一 emp_id。

package sync

import (
	"context"
	"lina-core/pkg/logger"
	"sort"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// backfillIdentities 枚举每个已配置的飞书应用，直接遍历其每个用户，按工号
// 匹配 employee，并对每个 open_id 各写一行 (lark_app_id, open_id) → emp_id 映射
// （own/assoc 分别落 id_source）。
// 它返回至少获得一条身份的 EmpID 集合，以便调用方对没有任何飞书映射的在职员工告警。
//
// employeeNoToEmpID 把工号映射到其 EmpID（工号全公司唯一，无冲突）。
//
// 按 (lark_app_id, lark_open_id) 幂等，重复安全。
func (s *Syncer) backfillIdentities(ctx context.Context, employeeNoToEmpID map[string]int64, sum *Summary) map[int64]bool {
	matched := map[int64]bool{}
	// 排障用：把 Moka 侧参与 JOIN 的工号整体打一条 debug，
	// 便于与飞书侧候选对照。
	logger.Debugf(ctx, "员工目录同步: Moka侧参与JOIN的工号(%d) = %v", len(employeeNoToEmpID), sortedJoinKeys(employeeNoToEmpID))
	for _, fetcher := range s.larkFetchers {
		appID := fetcher.AppID()
		users, err := fetcher.ListUsers(ctx)
		if err != nil {
			logger.Errorf(ctx, "员工目录同步: 枚举飞书通讯录用户失败 app=%s: %v", appID, err)
			sum.Errors++
			continue
		}

		// 排障用：收集飞书有、但匹配不到 Moka 员工的工号，
		// 循环结束后统一打一条 debug，便于核对 backfilled 少于飞书候选数的差值。
		var unmatchedEmployeeNos []string
		for _, u := range users {
			if u.OpenID == "" {
				continue
			}

			var empID int64

			// 按工号匹配
			if u.EmployeeNo != "" {
				if id, ok := employeeNoToEmpID[u.EmployeeNo]; ok && id != 0 {
					empID = id
				} else {
					unmatchedEmployeeNos = append(unmatchedEmployeeNos, u.EmployeeNo)
				}
			}

			// 匹配失败，跳过
			if empID == 0 {
				continue
			}

			// 来源缺省按 own 处理（fetcher 未透传 Source 时的向后兼容）。
			idSource := u.Source
			if idSource == "" {
				idSource = empcap.IDSourceOwn
			}
			if err := s.employees.UpsertLarkIdentity(ctx, appID, u.OpenID, u.UserID, idSource, empID); err != nil {
				logger.Errorf(ctx, "员工目录同步: 写入飞书身份映射失败 app=%s open_id=%s empID=%d: %v", appID, u.OpenID, empID, err)
				sum.Errors++
				continue
			}
			matched[empID] = true
			sum.Backfilled++
			logger.Debugf(ctx, "员工目录同步: 按工号匹配成功 app=%s open_id=%s empID=%d", appID, u.OpenID, empID)
		}
		if len(unmatchedEmployeeNos) > 0 {
			sort.Strings(unmatchedEmployeeNos)
			logger.Debugf(ctx, "员工目录同步: 飞书侧存在但匹配不到Moka员工的工号 app=%s (%d) = %v", appID, len(unmatchedEmployeeNos), unmatchedEmployeeNos)
		}
	}
	return matched
}

// sortedJoinKeys 返回 Moka 侧工号的有序切片，仅供 debug 日志对照使用。
func sortedJoinKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
