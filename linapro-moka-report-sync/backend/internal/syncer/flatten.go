package syncer

import (
	"fmt"
	"strings"
)

// FlattenReport 把 Moka 报表（表头 + 以 dataIndex 为键的行）转换为
// 以列标题为键的行，以及有序的叶子列标题列表。
//
// 表头可能携带子级（多级表头）；FlattenReport 会防御性地遍历到叶子，
// 避免多余的子级导致列丢失。只有叶子携带映射到行值的 dataIndex。
func FlattenReport(data *ReportData) (cols []string, rows []Row) {
	if data == nil {
		return nil, nil
	}
	type leaf struct {
		title     string
		dataIndex string
	}
	var leaves []leaf
	var walk func(h ReportHeader, prefix string)
	walk = func(h ReportHeader, prefix string) {
		title := h.Title
		if prefix != "" {
			title = prefix + "-" + h.Title
		}
		if len(h.Children) > 0 {
			for _, c := range h.Children {
				walk(c, title)
			}
			return
		}
		leaves = append(leaves, leaf{title: title, dataIndex: h.DataIndex})
	}
	for _, h := range data.Headers {
		walk(h, "")
	}

	cols = make([]string, 0, len(leaves))
	for _, l := range leaves {
		cols = append(cols, l.title)
	}

	rows = make([]Row, 0, len(data.Rows))
	for _, raw := range data.Rows {
		r := make(Row, len(leaves))
		for _, l := range leaves {
			r[l.title] = stringify(raw[l.dataIndex])
		}
		rows = append(rows, r)
	}
	return cols, rows
}

// Stringify 把 JSON/SDK 解码得到的单元格值归一化为去首尾空白的字符串。
// 导出该函数是为了让 Bitable 读取方以一致方式转换记录字段值。
func Stringify(v any) string {
	return stringify(v)
}

// stringify 把 JSON 解码得到的单元格值归一化为去首尾空白的字符串。
// Moka 用 "-" 作为空字段的占位符；这些会被转换为 ""。
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(t)
		if s == "-" {
			return ""
		}
		return s
	case []string:
		// 来自 Bitable 读取方（textCellToString）抽取的富文本分段切片：
		// 逐段归一后按空串拼接，保证 "-" 占位符在每段里也能归零。
		parts := make([]string, 0, len(t))
		for _, seg := range t {
			parts = append(parts, stringify(seg))
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}
