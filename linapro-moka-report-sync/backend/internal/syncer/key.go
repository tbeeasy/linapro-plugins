package syncer

import "strings"

// KeySpec 描述如何从一行数据中计算唯一匹配键：
// Fields 列出参与拼接的列名（按顺序），Sep 为拼接分隔符。
// 仅用于记录匹配，不作为字段写入 Bitable。
type KeySpec struct {
	Fields []string
	Sep    string
}

// KeyOf 按 Fields 顺序取值、逐列 TrimSpace 后用 Sep 拼接；任一列值为空时返回 ""，
// 调用方应将空键行视为无键行跳过。
func (s KeySpec) KeyOf(row Row) string {
	parts := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		v := strings.TrimSpace(row[f])
		if v == "" {
			return ""
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, s.Sep)
}
