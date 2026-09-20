// Package syncer 实现报表→多维表格同步算法：
// 它把 Moka 报表拍平为以 KeySpec 为键的行，并基于字段级差异
// 针对目标多维表格计算写入计划。
//
// 规划逻辑无网络/IO，仅引用 lark 值类型解析器与飞书列类型常量做强类型比对，
// 因此仍可被完整单元测试；面向 SDK 的读写位于 lark 包，编排逻辑位于 sync.go。
package syncer

import (
	"strconv"
	"strings"

	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// Row 是一行拍平后的报表/表格数据：列标题 -> 字符串化后的值。
type Row map[string]string

// ExistingRecord 是为规划而投影出的一条多维表格记录。
type ExistingRecord struct {
	RecordID string
	Fields   Row
	// PersonIDs 是人员列的结构化旁路：键为列名，值为该单元格回读到的 open_id 集合。
	// 人员列比对走 open_id 集合而非 Fields 中的姓名文本，故与 Fields 并存；无人员列时为 nil。
	PersonIDs map[string][]string
}

// PlanInput 是 Plan 的输入。
type PlanInput struct {
	Key         KeySpec
	ReportCols  []string
	Rows        []Row
	TableFields map[string]struct{}
	Existing    map[string]ExistingRecord
	// PersonCols 声明哪些列是人员类型列，需按 open_id 集合比对而非文本比对；空表示无人员列。
	PersonCols map[string]struct{}
	// ResolvePersonIDsByEmployeeNos 把 Moka 侧人员列值（工号，多个工号以逗号拼接）解析为
	// 期望写入的 open_id 集合。为 nil 时人员列退化为文本比对（不启用集合比对）。
	ResolvePersonIDsByEmployeeNos func(employeeNos []string) []string
	// FieldTypes 是目标多维表格「列名 → 飞书列类型码」，供 diffFields 做强类型比对：
	// 日期列两侧归一到毫秒、数字列两侧解析为 float64、其余列按文本。为 nil 或某列缺省时
	// 该列退化为文本比对（与旧行为一致）。
	FieldTypes map[string]int
}

// CreateOp 表示一条待新增的记录。
type CreateOp struct {
	Name   string
	Fields Row
	// Persons 是人员列的写入旁路：列名 → 已解析的 open_id 集合。人员列值不进 Fields
	// （Fields 里的工号文本无法直接写入飞书人员字段），由共享库经该旁路序列化。
	Persons map[string][]string
}

// UpdateOp 表示对一条已有记录的整体覆盖。
type UpdateOp struct {
	Name     string
	RecordID string
	Fields   Row
	// Persons 语义同 CreateOp.Persons。
	Persons map[string][]string
}

// PlanResult 是计算得到的写入计划。
type PlanResult struct {
	Creates       []CreateOp
	Updates       []UpdateOp
	Frozen        int
	SkippedNoName int
	Intersection  []string
}

// Plan 针对已有的多维表格行，为单个报表计算写入计划。
//
// 规则：
//   - 交集 = 报表列 ∩ 多维表格字段；只写入这些列，
//     因此手动维护的表格专属列永远不会被触碰。
//   - 某 Key.KeyOf(row) 值不存在已有记录 → 用所有交集字段新增该行。
//   - 找到已有记录 → 计算字段级差异：取 Moka 中的非空值，
//     跳过键组成列（Key.Fields），跳过值与已有记录等价的字段。
//     人员列按 open_id 集合比对（见 diffFields），其余列按飞书列类型强类型比对
//     （见 fieldEqual：日期比毫秒、数字比 float64、其余比文本）。
//     若差异非空，仅更新这些差异字段。
//   - 差异为空 → 冻结（跳过）。
//
// 人员列的身份解析在全链路只发生一次：Plan 内把每个人员列的工号解析为 open_id 集合，
// 既用于与回读值判等，也直接作为要写入的 open_id 集合产出到 op.Persons（不再交由下游
// 按工号二次解析）。人员列的值因此永远不进 Fields。
func Plan(in PlanInput) PlanResult {
	intersection := intersect(in.ReportCols, in.TableFields)
	out := PlanResult{Intersection: intersection}

	for _, row := range in.Rows {
		name := in.Key.KeyOf(row)
		if name == "" {
			out.SkippedNoName++
			continue
		}
		fields := project(row, intersection, in.PersonCols)
		personIDs := resolvePersonCols(row, intersection, in.PersonCols, in.ResolvePersonIDsByEmployeeNos)

		rec, ok := in.Existing[name]
		if !ok {
			out.Creates = append(out.Creates, CreateOp{Name: name, Fields: fields, Persons: personIDs})
			continue
		}
		diff, diffPersons := diffFields(fields, rec, in.Key.Fields, personIDs, in.FieldTypes)
		if len(diff) > 0 || len(diffPersons) > 0 {
			out.Updates = append(out.Updates, UpdateOp{
				Name:     name,
				RecordID: rec.RecordID,
				Fields:   diff,
				Persons:  diffPersons,
			})
			continue
		}
		out.Frozen++
	}
	return out
}

// resolvePersonCols 把一行中人员列的工号解析为 open_id 集合，返回「列名 → open_id 集合」。
// 只在列名属于交集且解析器非空时参与；解析结果为空（工号为空、未命中、解析器未启用）的列
// 不入结果，与「该列无内容不写」一致（preparePersonFields 已把这类列清空）。
func resolvePersonCols(
	row Row,
	intersection []string,
	personCols map[string]struct{},
	resolve func(employeeNos []string) []string,
) map[string][]string {
	if len(personCols) == 0 || resolve == nil {
		return nil
	}
	var out map[string][]string
	for _, col := range intersection {
		if _, isPerson := personCols[col]; !isPerson {
			continue
		}
		empNo := row[col]
		if empNo == "" {
			continue
		}
		ids := resolve(splitEmployeeNos(empNo))
		if len(ids) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string][]string, len(personCols))
		}
		out[col] = ids
	}
	return out
}

// splitEmployeeNos 把 Moka 侧人员列值拆成工号列表：多个工号以逗号拼接，逐个 trim 后跳过空段。
// 这是消费方唯一的输入格式归一（身份解析与合并去重都在 empcap owner 侧完成）。
func splitEmployeeNos(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// diffFields 返回待写入的 mokaFields 子集与待写入的人员列集合：
//   - 非人员列：与已有记录对应值不等价（按 fieldEqual 强类型比对）、且非空、
//     且不属于键组成列 keyFields 的值。fieldTypes 决定每列的比对方式（日期比毫秒、
//     数字比 float64、其余比文本）；某列类型缺省时退化为文本比对。
//   - 人员列：按 open_id 集合比对（见下），不等价时把**已解析的 open_id 集合**放进
//     persons 返回值——写侧直接写这组 id，不再按工号二次解析。
//
// 人员列必须按 open_id 集合比对，不能按文本比对：Moka 侧该列值是工号、Bitable 回读侧
// rec.Fields 中是open_id，二者不在同一空间，按文本比永远不等，会让人员列每轮被误判为变更
// 并空转重写。故拿 Plan 已解析出的期望 open_id 集合（expected）与回读记录 rec.PersonIDs
// 中该列的 open_id 集合做集合判等；仅在不等价时纳入写入。expected 中不含的列（工号为空或
// 解析不到 open_id）一律跳过，与既有「无法解析即不写该列」语义一致。
func diffFields(
	mokaFields Row,
	rec ExistingRecord,
	keyFields []string,
	expected map[string][]string,
	fieldTypes map[string]int,
) (Row, map[string][]string) {
	skip := make(map[string]struct{}, len(keyFields))
	for _, f := range keyFields {
		skip[f] = struct{}{}
	}
	out := make(Row)
	for k, v := range mokaFields {
		if _, isKey := skip[k]; isKey || v == "" {
			continue
		}
		if _, isPerson := expected[k]; isPerson {
			continue // 人员列只走 persons 旁路，见下方循环。
		}
		if fieldEqual(v, rec.Fields[k], fieldTypes[k]) {
			continue
		}
		out[k] = v
	}

	var persons map[string][]string
	for col, ids := range expected {
		if personSet(ids).equal(rec.PersonIDs[col]) {
			continue
		}
		if persons == nil {
			persons = make(map[string][]string, len(expected))
		}
		persons[col] = ids
	}
	return out, persons
}

func intersect(reportCols []string, tableFields map[string]struct{}) []string {
	out := make([]string, 0, len(reportCols))
	for _, c := range reportCols {
		if _, ok := tableFields[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// project 按交集列投影出一行的写入字段。人员列被排除：它们的值只经 op.Persons 旁路
// 以 open_id 集合写入，放进 Fields 会以工号文本写出，与飞书人员字段类型不符。
// 只有 Plan 能识别人员列（personCols 来自消费方声明），故在此处排除。
func project(row Row, cols []string, personCols map[string]struct{}) Row {
	out := make(Row, len(cols))
	for _, c := range cols {
		if _, isPerson := personCols[c]; isPerson {
			continue
		}
		out[c] = row[c]
	}
	return out
}

// fieldEqual 按飞书列类型分派做强类型比对，不做纯字符串比对：
//   - TypeDateTime：两侧归一到 int64 毫秒相等（见 parseDateMillis，消化回读侧科学计数法）；
//   - TypeNumber：两侧 ParseFloat → float64 相等（回读 "90" 与报表 "90.0" 等价）；
//   - 其它/文本（含 fieldType 缺省为 0）：两侧 TrimSpace 后字符串相等。
//
// 强类型分派天然消化科学计数法（同源字符串 parse 回同一数值）。任一侧无法按类型解析时
// 退化为文本比对，保证不因解析失败误判。日期列的 CST 语义已由 normalizeColumns 在 Plan 前
// 归一为毫秒串，故此处只需按数值比对（与 recruit 的 fieldEqual 行为一致）。
func fieldEqual(desired, existing string, fieldType int) bool {
	switch fieldType {
	case larkbitablesdk.TypeDateTime:
		dm, dok := parseDateMillis(desired)
		em, eok := parseDateMillis(existing)
		if dok && eok {
			return dm == em
		}
	case larkbitablesdk.TypeNumber:
		df, derr := strconv.ParseFloat(strings.TrimSpace(desired), 64)
		ef, eerr := strconv.ParseFloat(strings.TrimSpace(existing), 64)
		if derr == nil && eerr == nil {
			return df == ef
		}
	}
	return strings.TrimSpace(desired) == strings.TrimSpace(existing)
}

// parseDateMillis 把日期字符串归一化为毫秒。先走共享库 ParseEpochMillisUTC（认纯数字/ISO），
// 失败时再按 float 解析后取整——飞书日期列回读为数字，经 %v 字符串化后大整数会呈科学计数法
// （如 "1.7065224e+12"），ParseEpochMillisUTC 的 ParseInt 无法识别，故此处兜底。
// 比对两侧此时均为毫秒级整数串（Moka 侧经 normalizeColumns 归一、回读侧本就是毫秒），无秒/毫秒歧义。
func parseDateMillis(s string) (int64, bool) {
	if ms, ok := lark.ParseEpochMillisUTC(s); ok {
		return ms, true
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return int64(f), true
	}
	return 0, false
}
