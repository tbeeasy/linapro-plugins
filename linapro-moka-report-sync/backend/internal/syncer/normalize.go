package syncer

import "strconv"

// ParseMillis 把日期单元格的字符串归一为毫秒级 Unix 时间戳字符串。
// 返回 ok=false 表示该字符串无法被识别为日期，调用方应保留原值。
type ParseMillis func(value string) (millis int64, ok bool)

// NormalizeColumns 对需要规一的列，把 Moka 侧的原始字符串归一为与 Bitable 回读一致的字符串。
// 当前唯一实现是日期：把时间字符串（如 "2025-06-01"）解析为东八区毫秒级时间戳字符串
// （如 "1759680000000"），使规划阶段与 Bitable 回读值保持一致：写侧 rowToFields 把日期写成
// 毫秒数字，读侧 cellToString 把毫秒读回字符串；Moka 侧若保留原始日期文本，会和毫秒字符串
// 比较永远不同，导致同值被误判为差异而反复重写。后续若出现其他需要规一的列，只需在此按列
// 追加对应归一逻辑，函数名与签名无需变动。
//
// 规则与写侧 rowToFields 的日期语义对称：
//   - 空值、无法解析的值保留原值，避免把未知形态误改成空或误判为日期；
//   - 仅当列在 cols 中才处理，文本、数字、人员等列不受影响；
//   - 幂等：毫秒数字串再经本函数时会被当作毫秒时间戳原样返回，结果保持稳定。
func NormalizeColumns(rows []Row, cols []string, parse ParseMillis) {
	if parse == nil || len(cols) == 0 {
		return
	}
	for _, row := range rows {
		for _, col := range cols {
			v := row[col]
			if v == "" {
				continue
			}
			if ms, ok := parse(v); ok {
				row[col] = strconv.FormatInt(ms, 10)
			}
		}
	}
}
