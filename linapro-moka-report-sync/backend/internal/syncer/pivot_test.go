package syncer

import (
	"testing"
)

// TestPivot_Basic 验证基本转置场景：两行月份数据 → 两个指标行。
func TestPivot_Basic(t *testing.T) {
	// 源报表：招聘漏斗图（月份列）为行，指标为列
	cols := []string{"招聘漏斗图", "简历收集数", "入职人数"}
	rows := []Row{
		{"招聘漏斗图": "5月", "简历收集数": "50", "入职人数": "8"},
		{"招聘漏斗图": "6月", "简历收集数": "60", "入职人数": "3"},
	}

	outCols, outRows, conflicts := Pivot(cols, rows, "招聘漏斗图", "招聘漏斗图")

	if len(conflicts) != 0 {
		t.Fatalf("无重复月份，冲突应为空，got %v", conflicts)
	}
	// 期望列：招聘漏斗图, 5月, 6月
	wantCols := []string{"招聘漏斗图", "5月", "6月"}
	if len(outCols) != len(wantCols) {
		t.Fatalf("列数期望 %d，got %d: %v", len(wantCols), len(outCols), outCols)
	}
	for i, c := range wantCols {
		if outCols[i] != c {
			t.Errorf("列[%d] 期望 %q，got %q", i, c, outCols[i])
		}
	}

	// 期望行：简历收集数 和 入职人数
	if len(outRows) != 2 {
		t.Fatalf("期望 2 行，got %d", len(outRows))
	}
	// 第一行：简历收集数
	r0 := outRows[0]
	if r0["招聘漏斗图"] != "简历收集数" {
		t.Errorf("第1行 招聘漏斗图 期望 '简历收集数'，got %q", r0["招聘漏斗图"])
	}
	if r0["5月"] != "50" {
		t.Errorf("第1行 5月 期望 '50'，got %q", r0["5月"])
	}
	if r0["6月"] != "60" {
		t.Errorf("第1行 6月 期望 '60'，got %q", r0["6月"])
	}

	// 第二行：入职人数
	r1 := outRows[1]
	if r1["招聘漏斗图"] != "入职人数" {
		t.Errorf("第2行 招聘漏斗图 期望 '入职人数'，got %q", r1["招聘漏斗图"])
	}
	if r1["5月"] != "8" {
		t.Errorf("第2行 5月 期望 '8'，got %q", r1["5月"])
	}
	if r1["6月"] != "3" {
		t.Errorf("第2行 6月 期望 '3'，got %q", r1["6月"])
	}
}

// TestPivot_AllNonHeaderColsBecomeMetrics 验证除 headerColumn 外的所有列均转为指标行。
func TestPivot_AllNonHeaderColsBecomeMetrics(t *testing.T) {
	cols := []string{"月份", "A", "B", "C", "D", "E", "F", "G", "H"}
	rows := []Row{
		{"月份": "1月", "A": "1", "B": "2", "C": "3", "D": "4", "E": "5", "F": "6", "G": "7", "H": "8"},
	}

	_, outRows, conflicts := Pivot(cols, rows, "月份", "月份")

	if len(conflicts) != 0 {
		t.Fatalf("无重复，冲突应为空")
	}
	// 8 个指标列 → 8 行
	if len(outRows) != 8 {
		t.Fatalf("期望 8 行，got %d", len(outRows))
	}
	// 按源列顺序：A、B、C、D、E、F、G、H
	wantMetrics := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for i, want := range wantMetrics {
		if outRows[i]["月份"] != want {
			t.Errorf("行[%d] 期望指标 %q，got %q", i, want, outRows[i]["月份"])
		}
	}
}

// TestPivot_StableOrder 验证指标顺序和目标列（月份）顺序按源报表出现顺序稳定。
func TestPivot_StableOrder(t *testing.T) {
	cols := []string{"月份", "指标X", "指标Y"}
	rows := []Row{
		{"月份": "3月", "指标X": "30", "指标Y": "300"},
		{"月份": "1月", "指标X": "10", "指标Y": "100"},
		{"月份": "2月", "指标X": "20", "指标Y": "200"},
	}

	outCols, outRows, _ := Pivot(cols, rows, "月份", "月份")

	// 列顺序：月份, 3月, 1月, 2月（按源行出现顺序）
	wantCols := []string{"月份", "3月", "1月", "2月"}
	if len(outCols) != len(wantCols) {
		t.Fatalf("列数期望 %d，got %d", len(wantCols), len(outCols))
	}
	for i, c := range wantCols {
		if outCols[i] != c {
			t.Errorf("列[%d] 期望 %q，got %q", i, c, outCols[i])
		}
	}

	// 行顺序：指标X, 指标Y（按源列出现顺序）
	if len(outRows) != 2 {
		t.Fatalf("期望 2 行，got %d", len(outRows))
	}
	if outRows[0]["月份"] != "指标X" {
		t.Errorf("第1行期望指标 '指标X'，got %q", outRows[0]["月份"])
	}
	if outRows[1]["月份"] != "指标Y" {
		t.Errorf("第2行期望指标 '指标Y'，got %q", outRows[1]["月份"])
	}

	// 值与位置正确
	if outRows[0]["3月"] != "30" || outRows[0]["1月"] != "10" || outRows[0]["2月"] != "20" {
		t.Errorf("指标X 各月值不正确：%v", outRows[0])
	}
}

// TestPivot_DuplicateHeaderValue 验证 headerColumn 值重复时后值覆盖前值并返回冲突。
func TestPivot_DuplicateHeaderValue(t *testing.T) {
	cols := []string{"月份", "指标A"}
	rows := []Row{
		{"月份": "5月", "指标A": "旧值"},
		{"月份": "5月", "指标A": "新值"}, // 重复月份，值不同
	}

	_, outRows, conflicts := Pivot(cols, rows, "月份", "月份")

	// 应有一条冲突记录
	if len(conflicts) != 1 {
		t.Fatalf("期望 1 条冲突，got %d", len(conflicts))
	}
	c := conflicts[0]
	if c.HeaderValue != "5月" {
		t.Errorf("冲突 HeaderValue 期望 '5月'，got %q", c.HeaderValue)
	}
	if c.Metric != "指标A" {
		t.Errorf("冲突 Metric 期望 '指标A'，got %q", c.Metric)
	}
	if c.Kept != "新值" {
		t.Errorf("冲突 Kept 期望 '新值'（后值），got %q", c.Kept)
	}
	if c.Dropped != "旧值" {
		t.Errorf("冲突 Dropped 期望 '旧值'（前值），got %q", c.Dropped)
	}

	// 输出行中应保留后值
	if len(outRows) != 1 {
		t.Fatalf("期望 1 行（指标A），got %d", len(outRows))
	}
	if outRows[0]["5月"] != "新值" {
		t.Errorf("输出中 5月 应为 '新值'，got %q", outRows[0]["5月"])
	}
}

// TestPivot_SameValueDuplicate 验证 headerColumn 值重复但值相同时不产生冲突。
func TestPivot_SameValueDuplicate(t *testing.T) {
	cols := []string{"月份", "指标A"}
	rows := []Row{
		{"月份": "5月", "指标A": "100"},
		{"月份": "5月", "指标A": "100"}, // 重复但值相同
	}

	_, _, conflicts := Pivot(cols, rows, "月份", "月份")

	if len(conflicts) != 0 {
		t.Fatalf("相同值重复不应产生冲突，got %v", conflicts)
	}
}

// TestPivot_DecoupledIndexColumn 验证源列名（headerColumn）与输出索引列名（indexColumn）
// 解耦：源报表日期列叫 "公共日期"（值 "2026-01"…），目标表指标标签列叫 "招聘漏斗图"。
func TestPivot_DecoupledIndexColumn(t *testing.T) {
	cols := []string{"入职数", "公共日期", "复试人数"}
	rows := []Row{
		{"公共日期": "2026-01", "入职数": "2", "复试人数": "4"},
		{"公共日期": "2026-02", "入职数": "5", "复试人数": "6"},
	}

	outCols, outRows, conflicts := Pivot(cols, rows, "公共日期", "招聘漏斗图")

	if len(conflicts) != 0 {
		t.Fatalf("无重复月份，冲突应为空，got %v", conflicts)
	}
	// 输出首列为 indexColumn（招聘漏斗图），其后为各月份；源列 公共日期 不出现在输出。
	wantCols := []string{"招聘漏斗图", "2026-01", "2026-02"}
	if len(outCols) != len(wantCols) {
		t.Fatalf("列数期望 %d，got %d: %v", len(wantCols), len(outCols), outCols)
	}
	for i, c := range wantCols {
		if outCols[i] != c {
			t.Errorf("列[%d] 期望 %q，got %q", i, c, outCols[i])
		}
	}

	// 指标名写入 indexColumn（招聘漏斗图）字段，而非源列 公共日期。
	if len(outRows) != 2 {
		t.Fatalf("期望 2 行（入职数/复试人数），got %d", len(outRows))
	}
	if outRows[0]["招聘漏斗图"] != "入职数" {
		t.Errorf("第1行 招聘漏斗图 期望 '入职数'，got %q", outRows[0]["招聘漏斗图"])
	}
	if _, ok := outRows[0]["公共日期"]; ok {
		t.Errorf("输出行不应含源列 公共日期，got %v", outRows[0])
	}
	if outRows[0]["2026-01"] != "2" || outRows[0]["2026-02"] != "5" {
		t.Errorf("入职数各月值不正确：%v", outRows[0])
	}
	if outRows[1]["招聘漏斗图"] != "复试人数" || outRows[1]["2026-01"] != "4" || outRows[1]["2026-02"] != "6" {
		t.Errorf("复试人数行不正确：%v", outRows[1])
	}
}

// TestPivot_IndexColumnEmpty 验证 indexColumn 为空字符串时返回 nil（不回退到 headerColumn）。
func TestPivot_IndexColumnEmpty(t *testing.T) {
	cols := []string{"月份", "指标A"}
	rows := []Row{{"月份": "1月", "指标A": "10"}}

	outCols, outRows, conflicts := Pivot(cols, rows, "月份", "")

	if outCols != nil || outRows != nil || conflicts != nil {
		t.Fatalf("indexColumn 为空时应返回 nil, nil, nil")
	}
}

// TestPivot_HeaderColumnMissing 验证 headerColumn 不在列集合中时返回 nil。
func TestPivot_HeaderColumnMissing(t *testing.T) {
	cols := []string{"A", "B"}
	rows := []Row{
		{"A": "v1", "B": "v2"},
	}

	outCols, outRows, conflicts := Pivot(cols, rows, "不存在列", "招聘漏斗图")

	if outCols != nil || outRows != nil || conflicts != nil {
		t.Fatalf("headerColumn 不存在时应返回 nil, nil, nil，got cols=%v rows=%v", outCols, outRows)
	}
}

// TestPivot_HeaderColumnEmpty 验证 headerColumn 为空字符串时返回 nil。
func TestPivot_HeaderColumnEmpty(t *testing.T) {
	cols := []string{"A", "B"}
	rows := []Row{{"A": "v1", "B": "v2"}}

	outCols, outRows, conflicts := Pivot(cols, rows, "", "招聘漏斗图")

	if outCols != nil || outRows != nil || conflicts != nil {
		t.Fatalf("headerColumn 为空时应返回 nil, nil, nil")
	}
}

// TestPivot_EmptyHeaderValuesSkipped 验证 headerColumn 行值为空的行被跳过。
func TestPivot_EmptyHeaderValuesSkipped(t *testing.T) {
	cols := []string{"月份", "指标A"}
	rows := []Row{
		{"月份": "", "指标A": "10"}, // 空月份行，应跳过
		{"月份": "1月", "指标A": "20"},
	}

	outCols, outRows, _ := Pivot(cols, rows, "月份", "月份")

	// 只有 1月 出现在列中
	if len(outCols) != 2 { // 月份 + 1月
		t.Fatalf("期望列：月份+1月（共2列），got %v", outCols)
	}
	if outRows[0]["1月"] != "20" {
		t.Errorf("1月 值期望 '20'，got %q", outRows[0]["1月"])
	}
}
