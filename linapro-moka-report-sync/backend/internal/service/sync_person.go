// sync_person.go 实现人员字段的来源解析：把 Bitable 人员字段的值换成工号，交给 Plan 统一解析为 open_id。
//   - 工号来源（personFieldEmpNoSources）：直接搬运工号列的值；
//   - 姓名来源（personFieldNameSources）：先按员工目录把姓名换成工号，再与工号来源合流。
//
// 姓名匹配口径与 linapro-lark-push 的姓名收件人一致：去首尾空白后精确匹配、只认在职员工、
// 同一自然人跨租户的多行按工号去重算一人。0 人或唯一一人无工号时留空跳过；≥2 人重名时同样
// 留空跳过，并由 RunOnce 在周期末汇总告警——重名时静默取其一就是写错人。
// 姓名只换成工号，open_id 解析仍在 Plan 内一次完成，不维护第二条身份链路。

package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"lina-core/pkg/logger"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"linapro-lark-sdk/larkalert"
	"linapro-lark-sdk/larkim"

	"lina-plugin-linapro-moka-report-sync/backend/internal/config"
	"lina-plugin-linapro-moka-report-sync/backend/internal/syncer"
)

const (
	// alertKindPersonNameDuplicate 是人员姓名重名告警的种类，同时用作防抖键，取值必须稳定。
	alertKindPersonNameDuplicate = "person_name_duplicate"
	// maxListedNames 限制单条日志或告警中每组列出的姓名数，避免大面积未命中时刷出超长正文。
	maxListedNames = 20
)

// nameResolution 是一个映射本轮姓名解析的结果。
type nameResolution struct {
	// employeeNos 是「姓名 → 唯一在职员工工号」，只含可解析的姓名。
	employeeNos map[string]string
	// duplicates 是去重后命中多名在职员工的姓名，按出现顺序。
	duplicates []string
	// unresolved 是没有在职员工、或唯一在职员工没有工号的姓名，按出现顺序。
	unresolved []string
}

// resolvePersonNames 为一个映射解析本轮姓名来源列中出现的全部姓名。
//
// 每个映射只发起一次批量查询（与行数无关），判定在内存完成。员工能力未绑定或查询失败时
// 返回零值：姓名来源列本轮全部留空跳过，不阻断该映射其余列与其他映射。
func resolvePersonNames(
	ctx context.Context,
	label string,
	rows []syncer.Row,
	fieldTypes map[string]int,
	m config.ReportMapping,
) nameResolution {
	for _, field := range conflictingPersonFields(m.PersonFieldEmpNoSources, m.PersonFieldNameSources) {
		logger.Warningf(ctx, "%s %s 人员字段 %q 同时声明了工号来源与姓名来源，以工号来源为准", logTag, label, field)
	}
	names := collectPersonNames(rows, fieldTypes, m.PersonFieldEmpNoSources, m.PersonFieldNameSources)
	if len(names) == 0 {
		return nameResolution{}
	}
	byName, err := empcap.EmployeesByNamesAnyTenant(ctx, names)
	if err != nil {
		logger.Warningf(ctx, "%s %s 按姓名查询员工失败，姓名来源人员列本轮跳过: %v", logTag, label, err)
		return nameResolution{}
	}
	res := judgePersonNames(names, byName)
	if len(res.unresolved) > 0 {
		logger.Warningf(ctx, "%s %s 以下姓名找不到唯一的在职员工（不存在、已离职或无工号），对应人员列留空: %s",
			logTag, label, joinNames(res.unresolved))
	}
	if len(res.duplicates) > 0 {
		logger.Warningf(ctx, "%s %s 以下姓名有多名在职员工重名，对应人员列留空: %s",
			logTag, label, joinNames(res.duplicates))
	}
	return res
}

// conflictingPersonFields 返回同时出现在工号来源与姓名来源中的人员字段名（升序）。
func conflictingPersonFields(empNoSources, nameSources map[string]string) []string {
	var out []string
	for field := range nameSources {
		if _, ok := empNoSources[field]; ok {
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}

// collectPersonNames 收集姓名来源人员列中出现的姓名：去首尾空白、跳过空值、按出现顺序去重。
// 只看目标表中确为人员类型、且未被工号来源覆盖的字段。
func collectPersonNames(
	rows []syncer.Row,
	fieldTypes map[string]int,
	empNoSources map[string]string,
	nameSources map[string]string,
) []string {
	fields := make([]string, 0, len(nameSources))
	for field := range nameSources {
		if fieldTypes[field] != larkbitablesdk.TypeUser {
			continue
		}
		if _, overridden := empNoSources[field]; overridden {
			continue
		}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return nil
	}
	// 字段按名排序，使姓名的出现顺序（进而日志与告警正文）每轮稳定。
	sort.Strings(fields)

	var (
		names []string
		seen  = make(map[string]struct{})
	)
	for _, row := range rows {
		for _, field := range fields {
			name := strings.TrimSpace(row[nameSources[field]])
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

// judgePersonNames 按姓名逐个判定员工：恰好一名在职且有工号时取其工号，否则归入重名或未解析。
func judgePersonNames(names []string, byName map[string][]*empcap.Employee) nameResolution {
	res := nameResolution{employeeNos: make(map[string]string, len(names))}
	for _, name := range names {
		employeeNo, matched := pickActiveEmployeeNo(byName[name])
		switch {
		case matched > 1:
			res.duplicates = append(res.duplicates, name)
		case matched == 1 && employeeNo != "":
			res.employeeNos[name] = employeeNo
		default:
			res.unresolved = append(res.unresolved, name)
		}
	}
	return res
}

// pickActiveEmployeeNo 返回同名员工行中去重后的在职人数，及恰好一人时其工号（可能为空）。
// 同一工号的多行是同一自然人在不同租户的数据，算一人；工号为空时退回 EmpID 去重。
func pickActiveEmployeeNo(rows []*empcap.Employee) (string, int) {
	var (
		seen       = make(map[string]struct{}, len(rows))
		employeeNo string
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
		employeeNo = emp.EmployeeNo
	}
	return employeeNo, len(seen)
}

// preparePersonFields 把人员字段的值换成工号，交给 Plan 统一解析为 open_id：
// 人员列不能直接把工号或姓名文本交给共享库（飞书人员字段只接受用户 id）。
//   - personFieldEmpNoSources 声明的字段取工号来源列的值（优先）；
//   - personFieldNameSources 声明的字段取姓名来源列的值，经 nameToEmpNo 换成工号，换不到为空；
//   - 两边都未声明的人员字段直接清空，不参与本轮写入。
//
// 工号能否解析到 open_id 不在此判断：Plan 内 resolvePersonCols 会对每个人员列解析一次，解析
// 为空的列自然不写（project 已把人员列排除出 Fields、resolvePersonCols 跳过空集合），与在此
// 预清空等价。故这里只做「取工号」这一步搬运，不调用 open_id 解析器预解析——保证全链路
// 对同一工号只解析一次。
//
// 每行先按原值算出全部人员字段的新值再统一回写，避免来源列本身也是人员字段（如姓名来源
// 「面谈人 1 → 面谈人 1」）时，结果受 map 遍历顺序影响。
func preparePersonFields(
	rows []syncer.Row,
	fieldTypes map[string]int,
	empNoSources map[string]string,
	nameSources map[string]string,
	nameToEmpNo map[string]string,
) {
	for _, row := range rows {
		resolved := make(map[string]string)
		for fieldName := range row {
			if fieldTypes[fieldName] != larkbitablesdk.TypeUser {
				continue
			}
			if empNoCol, ok := empNoSources[fieldName]; ok {
				resolved[fieldName] = row[empNoCol]
				continue
			}
			if nameCol, ok := nameSources[fieldName]; ok {
				resolved[fieldName] = nameToEmpNo[strings.TrimSpace(row[nameCol])]
				continue
			}
			// 未声明来源的人员字段，本轮跳过。
			resolved[fieldName] = ""
		}
		for fieldName, value := range resolved {
			row[fieldName] = value
		}
	}
}

// notifyNameDuplicates 把本轮所有映射的重名姓名汇总成一条运维告警。
// larkalert 按「配置键 + 种类」防抖，逐映射发送会让同一轮后续映射的告警被吞掉，故只发一条。
func (r *Runner) notifyNameDuplicates(ctx context.Context, results []MappingResult) {
	detail := buildNameDuplicateDetail(results)
	if detail == "" {
		return
	}
	larkalert.Notify(ctx, r.alertEnv(), larkalert.Event{
		ConfigKey: config.AlertConfigKey,
		Kind:      alertKindPersonNameDuplicate,
		Detail:    detail,
	})
}

// alertEnv 由 Runner 持有的宿主服务派生告警运行时依赖；服务缺失时对应能力为 nil，
// larkalert 会退化为不发送或不防抖。
func (r *Runner) alertEnv() larkalert.Env {
	env := larkalert.Env{
		NewClient: func(appID, appSecret string) larkalert.TextSender {
			return larkim.NewClient(appID, appSecret)
		},
	}
	if r.services != nil {
		env.Config = r.services.HostConfig()
		env.Cache = r.services.Cache()
	}
	return env
}

// buildNameDuplicateDetail 拼装重名告警正文：每个有重名的映射一行；没有重名时返回空串。
func buildNameDuplicateDetail(results []MappingResult) string {
	var lines []string
	for _, res := range results {
		if len(res.DuplicateNames) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s：%s", res.Label, joinNames(res.DuplicateNames)))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Moka 报表人员字段按姓名解析时遇到在职员工重名，对应单元格未写入：\n" + strings.Join(lines, "\n")
}

// joinNames 用顿号拼接姓名，超过 maxListedNames 时截断并注明总数。
func joinNames(names []string) string {
	if len(names) <= maxListedNames {
		return strings.Join(names, "、")
	}
	return fmt.Sprintf("%s 等 %d 个", strings.Join(names[:maxListedNames], "、"), len(names))
}
