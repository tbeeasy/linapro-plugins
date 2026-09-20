package syncer

import "slices"

// PivotConflict 记录 pivot 变换中 headerColumn 值重复导致的覆盖：
// 同一月份出现在多行时，后出现的值覆盖先出现的，并通过此结构上报供调用方告警。
type PivotConflict struct {
	HeaderValue string // 重复的 headerColumn 取值（如 "5月"）
	Metric      string // 发生冲突的指标列名
	Kept        string // 保留的值（后出现的覆盖值）
	Dropped     string // 被丢弃的值（先出现的旧值）
}

// Pivot 把「行值为 headerColumn 取值、列为各指标」的拍平报表转为
// 「行为各指标、列为各 headerColumn 取值」的转置形态。
//
// headerColumn（源列名）与 indexColumn（输出索引列名）解耦：源报表读哪一列的行值当
// 目标列头由 headerColumn 决定；转置后承载指标名的那一列叫什么、去和目标表哪一列对齐，
// 由 indexColumn 决定。二者常不同名——如源报表的日期列叫 "公共日期"（值 "2026-01"…），
// 而目标表的指标标签列叫 "招聘漏斗图"，此时 headerColumn="公共日期"、indexColumn="招聘漏斗图"。
//
// 输入：
//   - cols：源报表的有序列名列表（FlattenReport 产出）
//   - rows：源报表的行列表，每行以列名为键
//   - headerColumn：「列头列」名称，其行值（如 "2026-01"/"2026-02"）将成为目标列头
//   - indexColumn：转置后「指标名列」的输出列名，须与目标表的指标标签列同名
//
// 输出：
//   - outCols：转置后的列名列表，第一个元素为 indexColumn，其后按源报表出现顺序
//     排列各 headerColumn 取值（月份）
//   - outRows：转置后的行列表，每行对应一个指标；indexColumn 字段存储指标名，
//     其余字段为该指标在各月份的值
//   - conflicts：headerColumn 值重复时的覆盖记录（后值覆盖前值）
//
// 当 headerColumn 或 indexColumn 为空、或源报表不含 headerColumn 列时，三个返回值均为 nil。
func Pivot(cols []string, rows []Row, headerColumn, indexColumn string) (outCols []string, outRows []Row, conflicts []PivotConflict) {
	if headerColumn == "" || indexColumn == "" {
		return nil, nil, nil
	}

	// 确认 headerColumn 确实在列集合中存在。
	if !slices.Contains(cols, headerColumn) {
		return nil, nil, nil
	}

	// 按源报表列顺序收集指标列名（除 headerColumn 外的全部列）。
	// 同时排除 indexColumn：若它恰好也是源列名（与 headerColumn 不同），排除可避免与输出
	// 索引列同名冲突；indexColumn 通常不在源列中，此判断多为无副作用的兜底。
	metricCols := make([]string, 0, len(cols)-1)
	for _, c := range cols {
		if c != headerColumn && c != indexColumn {
			metricCols = append(metricCols, c)
		}
	}

	// 按源报表行顺序收集 headerColumn 的各取值（去重，保持首次出现顺序）。
	headerVals := make([]string, 0, len(rows))
	headerSeen := make(map[string]bool, len(rows))
	for _, r := range rows {
		v := r[headerColumn]
		if v == "" {
			continue
		}
		if !headerSeen[v] {
			headerSeen[v] = true
			headerVals = append(headerVals, v)
		}
	}

	// 构建目标列列表：indexColumn（承载指标名）+ 各月份列。
	outCols = make([]string, 0, 1+len(headerVals))
	outCols = append(outCols, indexColumn)
	outCols = append(outCols, headerVals...)

	// 构建「指标 → 月份 → 值」的中间映射，并记录冲突。
	// metricData[指标名][月份值] = 单元格值
	metricData := make(map[string]map[string]string, len(metricCols))
	for _, m := range metricCols {
		metricData[m] = make(map[string]string, len(headerVals))
	}

	for _, r := range rows {
		hv := r[headerColumn]
		if hv == "" {
			continue
		}
		for _, m := range metricCols {
			newVal := r[m]
			if existing, ok := metricData[m][hv]; ok && existing != "" {
				// headerColumn 值重复：后值覆盖前值，记录冲突。
				if newVal != "" && newVal != existing {
					conflicts = append(conflicts, PivotConflict{
						HeaderValue: hv,
						Metric:      m,
						Kept:        newVal,
						Dropped:     existing,
					})
					metricData[m][hv] = newVal
				}
			} else {
				metricData[m][hv] = newVal
			}
		}
	}

	// 按指标顺序生成输出行，每行 indexColumn 字段存储指标名。
	outRows = make([]Row, 0, len(metricCols))
	for _, m := range metricCols {
		row := make(Row, 1+len(headerVals))
		row[indexColumn] = m
		for _, hv := range headerVals {
			row[hv] = metricData[m][hv]
		}
		outRows = append(outRows, row)
	}

	return outCols, outRows, conflicts
}
