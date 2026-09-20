// Package job 的差异规划器：命中唯一键后做字段级比对，仅当存在差异列时产出只含差异列的
// 更新，无差异则冻结（跳过）。供需求2（面试同步）与需求3（报表回写）共用。
//
// 本文件是纯函数（无 SDK 网络/IO），仅引用 lark 值类型与飞书列类型常量，因此可完整单测。
// 借鉴同仓 linapro-moka-report-sync 的 syncer.Plan 形状，但因语义差异不复用其实现：本规划器
// **不做** report-sync 式的 v=="" 空值过滤——空串=显式清列，须参与比差（见 design 决策1）。
package job

import (
	"strconv"
	"strings"

	lark "linapro-lark-sdk/larkbitable"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// existingRow 是规划器视角的一条现有飞书记录：文本投影 fields + 人员列 open_id 集合旁路。
// personIDs 由 ListRecordsByID 回读的结构化人员值（Persons）投影而来（见 personIDsOf）。
type existingRow struct {
	recordID  string
	fields    lark.Row
	personIDs map[string][]string
}

// desiredRow 是规划器视角的一条期望写入行：唯一键 + 文本值字段 + 人员列期望 open_id 集合。
// fields 的值在此阶段仍为字符串（typeFields 尚未执行），比对时才按飞书列类型解析，与回读侧
// （恒为 string）对称。人员列不进 fields，只走 persons 旁路。
type desiredRow struct {
	key     string
	fields  lark.WriteRow
	persons map[string][]string
}

// planResult 是差异规划结果：新建、更新（仅含差异列）、以及被冻结（无差异）的行数。
type planResult struct {
	creates []lark.CreateOp
	updates []lark.UpdateOp
	frozen  int
}

// planDiff 对每条 desired 行按唯一键匹配现有记录做字段级差异比对：
//   - 未命中 → 新建（全列 + 人员旁路）；
//   - 命中且存在差异列或差异人员列 → 仅含差异的更新；
//   - 命中且无任何差异 → 冻结（frozen++）。
//
// keyCols 为唯一键组成列，比差时跳过（不写回键列）；fieldTypes 决定各列的强类型比对方式。
func planDiff(desired []desiredRow, existing map[string]existingRow, keyCols []string, fieldTypes map[string]int) planResult {
	skip := make(map[string]struct{}, len(keyCols))
	for _, c := range keyCols {
		skip[c] = struct{}{}
	}

	var out planResult
	for _, d := range desired {
		rec, ok := existing[d.key]
		if !ok {
			out.creates = append(out.creates, lark.CreateOp{Fields: d.fields, Persons: d.persons})
			continue
		}
		diff := diffFields(d.fields, rec, skip, fieldTypes)
		diffPersons := diffPersonCols(d.persons, rec.personIDs)
		if len(diff) > 0 || len(diffPersons) > 0 {
			out.updates = append(out.updates, lark.UpdateOp{RecordID: rec.recordID, Fields: diff, Persons: diffPersons})
			continue
		}
		out.frozen++
	}
	return out
}

// diffFields 返回 desired 中与现有记录不等的非键、非人员列（值仍为字符串）。
// 比对范围恒定为「desired 行的字段键」∩ 非键列，不做 v=="" 空值过滤：空串=显式清列。
func diffFields(desired lark.WriteRow, rec existingRow, skip map[string]struct{}, fieldTypes map[string]int) lark.WriteRow {
	var out lark.WriteRow
	for k, v := range desired {
		if _, isKey := skip[k]; isKey {
			continue
		}
		dv, ok := v.(string)
		if !ok {
			continue // 规划阶段 desired 值应为字符串；防御性跳过非字符串。
		}
		if fieldEqual(dv, rec.fields[k], fieldTypes[k]) {
			continue
		}
		if out == nil {
			out = lark.WriteRow{}
		}
		out[k] = dv
	}
	return out
}

// diffPersonCols 返回 desired 中 open_id 集合相对现有记录发生变化的人员列。
// 仅比对 desired 中出现的人员列：期望集合为空（未解析/降级）的列本轮不写、不清列
// （与「无法解析即不写该列」语义一致）。集合判等去重、顺序无关、忽略空 id。
func diffPersonCols(desired, existing map[string][]string) map[string][]string {
	var out map[string][]string
	for col, ids := range desired {
		if personSet(ids).equal(existing[col]) {
			continue
		}
		if out == nil {
			out = make(map[string][]string, len(desired))
		}
		out[col] = ids
	}
	return out
}

// fieldEqual 按飞书列类型分派做强类型比对，不做纯字符串比对：
//   - TypeDateTime：两侧归一到 int64 毫秒相等（见 parseDateMillis，消化回读侧科学计数法）；
//   - TypeNumber：两侧 ParseFloat → float64 相等（回读 "90" 与报表 "90.0" 等价）；
//   - 其它/文本：两侧 TrimSpace 后字符串相等。
//
// 强类型分派天然消化科学计数法（同源字符串 parse 回同一数值），故无需注入 Stringify。
// 任一侧无法按类型解析时退化为文本比对，保证不因解析失败误判。
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
// recruit 触及的日期均为毫秒级（写侧 typeFields 也按毫秒写），无秒/毫秒歧义。
func parseDateMillis(s string) (int64, bool) {
	if ms, ok := lark.ParseEpochMillisUTC(s); ok {
		return ms, true
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return int64(f), true
	}
	return 0, false
}

// buildExistingRows 把 ListRecordsByID 回读的 ExistingRecord 映射投影为规划器用的 existingRow，
// 以 keyOf(fields) 的返回值为键；keyOf 返回空串的记录跳过（键列为空）。
// 人员列结构化值 Persons 经 personIDsOf 投影为 open_id 集合旁路。
func buildExistingRows(records map[string]lark.ExistingRecord, keyOf func(lark.Row) string) map[string]existingRow {
	out := make(map[string]existingRow, len(records))
	for recordID, rec := range records {
		key := keyOf(rec.Fields)
		if key == "" {
			continue
		}
		out[key] = existingRow{
			recordID:  recordID,
			fields:    rec.Fields,
			personIDs: personIDsOf(rec.Persons),
		}
	}
	return out
}

// personIDsOf 把共享库回读的结构化人员值投影为「列名 → open_id 切片」。
// 只取每个人员的 id（open_id），丢弃姓名等展示字段：人员列判等只由 id 集合决定。
// 无人员列或某列无有效 id 时返回 nil，与「无该列」语义一致，交给集合比对判等。
func personIDsOf(persons map[string][]*larkbitablesdk.Person) map[string][]string {
	if len(persons) == 0 {
		return nil
	}
	out := make(map[string][]string, len(persons))
	for col, ps := range persons {
		ids := make([]string, 0, len(ps))
		for _, p := range ps {
			if p == nil || p.Id == nil || *p.Id == "" {
				continue
			}
			ids = append(ids, *p.Id)
		}
		if len(ids) > 0 {
			out[col] = ids
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// personSet 是人员列的比较契约：一组飞书用户 id（open_id），去重、顺序无关。
// 只用 id 表达身份，不带姓名：姓名是展示名而非标识，同名、改名、双租户同人多身份都会让姓名
// 漂移，用它判等会把同一个人误判为变更。写侧真正落库的也正是这组 id，故以 id 比对能与写入
// 结果严格对称。语义照抄 linapro-moka-report-sync 的 syncer.personSet（决策2：不跨插件共享）。
type personSet []string

// equal 报告两个 id 集合是否等价：去重后成员完全一致即为相等，与顺序无关。
// 空串 id 视为无效成员并忽略，使「无人员」与「仅含空 id」判定一致。
// 双方均为空集合时返回 true，据此让未变更的人员列走冻结分支。
func (s personSet) equal(other personSet) bool {
	left := s.unique()
	right := other.unique()
	if len(left) != len(right) {
		return false
	}
	for id := range left {
		if _, ok := right[id]; !ok {
			return false
		}
	}
	return true
}

// unique 把切片归约为去重后的 id 集合，并丢弃空串成员。
func (s personSet) unique() map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, id := range s {
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}
