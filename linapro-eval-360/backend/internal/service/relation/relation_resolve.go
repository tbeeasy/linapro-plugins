// relation_resolve.go 实现评价关系录入时「按姓名解析员工」的规则。
//
// 全量导入与新增评价关系都只录入姓名、不录入工号：先把整批姓名经 empcap.MapByNamesAnyTenant
// 一次查询解析（无 N+1），再逐个姓名判定结果：
//   - 只看在职员工，离职的同名员工不参与判断；
//   - 同一自然人在多个租户各有一行（工号相同）视为同一人，取 EmpID 最小的一行；
//   - 去重后 0 人 → 找不到；≥2 人 → 重名，调用方必须拒绝提交并点名该姓名。

package relation

import (
	"context"
	"strconv"
	"strings"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// nameOutcome 是按姓名解析员工的结果类别。
type nameOutcome int

// 按姓名解析的结果类别。
const (
	nameResolved  nameOutcome = iota // 恰好匹配一名在职员工
	nameNotFound                     // 无在职员工使用该姓名
	nameDuplicate                    // 有两名及以上在职员工使用该姓名
)

// nameResolution 是单个姓名的解析结果。
type nameResolution struct {
	// emp 仅在 outcome=nameResolved 时非空。
	emp     *empcap.Employee
	outcome nameOutcome
	// activeCount 为去重后的在职同名人数，用于重名提示。
	activeCount int
}

// normalizePersonName 归一化录入的员工姓名：仅去首尾空白（含全角空格），内部字符原样保留，
// 与员工表姓名按原值精确匹配。
func normalizePersonName(name string) string {
	return strings.TrimSpace(name)
}

// pickActiveByName 从同名员工行（按 EmpID 升序）中判定解析结果：只保留在职员工，
// 按「工号非空用工号、否则用 EmpID」去重后计数；同一工号的多行保留第一行（EmpID 最小）。
func pickActiveByName(rows []*empcap.Employee) nameResolution {
	var (
		seen   = make(map[string]struct{}, len(rows))
		picked *empcap.Employee
		count  int
	)
	for _, emp := range rows {
		if !emp.Active() {
			continue
		}
		key := "id:" + strconv.FormatInt(emp.EmpID, 10)
		if emp.EmployeeNo != "" {
			key = "no:" + emp.EmployeeNo
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		count++
		if picked == nil {
			picked = emp
		}
	}
	switch count {
	case 0:
		return nameResolution{outcome: nameNotFound}
	case 1:
		return nameResolution{emp: picked, outcome: nameResolved, activeCount: 1}
	default:
		return nameResolution{outcome: nameDuplicate, activeCount: count}
	}
}

// resolveNames 把一批已归一化的姓名一次性解析为「姓名 → 解析结果」；空姓名被忽略、不入结果。
// 只发起一次 empcap 批量查询，查询失败原样返回错误。
func resolveNames(ctx context.Context, empSvc empcap.Service, names []string) (map[string]nameResolution, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		unique = append(unique, n)
	}
	out := make(map[string]nameResolution, len(unique))
	if len(unique) == 0 {
		return out, nil
	}
	byName, err := empSvc.MapByNamesAnyTenant(ctx, unique)
	if err != nil {
		return nil, err
	}
	for _, n := range unique {
		out[n] = pickActiveByName(byName[n])
	}
	return out, nil
}
