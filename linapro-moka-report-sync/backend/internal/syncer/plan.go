// Package syncer 实现报表→多维表格同步算法：
// 它把 Moka 报表拍平为以工号为键的行，并基于字段级差异
// 针对目标多维表格计算写入计划。
//
// 这里的规划逻辑是纯函数（无 SDK、无 I/O），因此可被完整单元测试；
// 面向 SDK 的读写位于 lark 包，编排逻辑位于 sync.go。
package syncer

import "strings"

// Row 是一行拍平后的报表/表格数据：列标题 -> 字符串化后的值。
type Row map[string]string

// ExistingRecord 是为规划而投影出的一条多维表格记录。
type ExistingRecord struct {
	RecordID string
	Fields   Row
}

// PlanInput 是 Plan 的输入。
type PlanInput struct {
	UniqueField string
	ReportCols  []string
	Rows        []Row
	TableFields map[string]struct{}
	Existing    map[string]ExistingRecord
}

// CreateOp 表示一条待新增的记录。
type CreateOp struct {
	Name   string
	Fields Row
}

// UpdateOp 表示对一条已有记录的整体覆盖。
type UpdateOp struct {
	Name     string
	RecordID string
	Fields   Row
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
//   - 某 uniqueField 值不存在已有记录 → 用所有交集字段新增该行。
//   - 找到已有记录 → 计算字段级差异：取 Moka 中的非空值，
//     跳过 UniqueField，跳过值与已有记录一致的字段。
//     若差异非空，仅更新这些差异字段。
//   - 差异为空 → 冻结（跳过）。
func Plan(in PlanInput) PlanResult {
	intersection := intersect(in.ReportCols, in.TableFields)
	out := PlanResult{Intersection: intersection}

	for _, row := range in.Rows {
		name := strings.TrimSpace(row[in.UniqueField])
		if name == "" {
			out.SkippedNoName++
			continue
		}
		fields := project(row, intersection)

		rec, ok := in.Existing[name]
		if !ok {
			out.Creates = append(out.Creates, CreateOp{Name: name, Fields: fields})
			continue
		}
		diff := diffFields(fields, rec.Fields, in.UniqueField)
		if len(diff) > 0 {
			out.Updates = append(out.Updates, UpdateOp{
				Name:     name,
				RecordID: rec.RecordID,
				Fields:   diff,
			})
			continue
		}
		out.Frozen++
	}
	return out
}

// diffFields 返回待写入的 mokaFields 子集：与对应已有值不同的非空值，
// 且排除 uniqueField。
func diffFields(mokaFields, existingFields Row, uniqueField string) Row {
	out := make(Row)
	for k, v := range mokaFields {
		if k == uniqueField || v == "" {
			continue
		}
		if existingFields[k] == v {
			continue
		}
		out[k] = v
	}
	return out
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

func project(row Row, cols []string) Row {
	out := make(Row, len(cols))
	for _, c := range cols {
		out[c] = row[c]
	}
	return out
}
