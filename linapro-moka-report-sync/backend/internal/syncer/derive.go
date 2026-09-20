package syncer

import (
	"regexp"
	"strings"
	"time"
)

// Deriver 是一个列派生纯函数：接收源列值列表与 kind 相关参数，
// 返回目标列名 → 新值的映射。无法解析时目标列返回空串（不写脏值）。
type Deriver func(sources []string, params map[string]any) map[string]string

// DeriveRule 声明一条列派生规则。
type DeriveRule struct {
	Kind    string            // 派生类型：date / split / regex
	Sources []string          // 源列名
	Targets map[string]string // 目标列名 → kind 相关参数（date: layout；regex: 捕获组序号字符串）
	Params  map[string]any    // 额外参数（split: by；regex: pattern）
}

// derivers 是封闭的命名注册表，每个 kind 对应一个纯函数。
// 新增一种派生只需在此注册一个纯函数，管线主体不需变动。
var derivers = map[string]Deriver{
	"date":  deriveDate,
	"split": deriveSplit,
	"regex": deriveRegex,
}

// ApplyDerivedColumns 对 rows 中的每行，按 rules 顺序执行列派生，
// 把派生出的目标列名追加进 cols 并返回（去重，保持首次出现顺序）。
// 原有列不会被移除；若目标列与已有列同名，以派生结果覆盖。
func ApplyDerivedColumns(rows []Row, cols []string, rules []DeriveRule) []string {
	if len(rules) == 0 {
		return cols
	}
	// 用于追加新列名（去重）。
	colSet := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		colSet[c] = struct{}{}
	}
	outCols := append([]string(nil), cols...)

	for _, rule := range rules {
		fn, ok := derivers[rule.Kind]
		if !ok {
			continue
		}
		for _, row := range rows {
			srcs := make([]string, 0, len(rule.Sources))
			for _, s := range rule.Sources {
				srcs = append(srcs, row[s])
			}
			result := fn(srcs, rule.Params)
			for target, val := range result {
				row[target] = val
				if _, seen := colSet[target]; !seen {
					colSet[target] = struct{}{}
					outCols = append(outCols, target)
				}
			}
		}
		// 确保 targets 的键出现在列集合中（即使源值全为空、result 为空）。
		for target := range rule.Targets {
			if _, seen := colSet[target]; !seen {
				colSet[target] = struct{}{}
				outCols = append(outCols, target)
			}
		}
	}
	return outCols
}

// deriveDate 解析源列值为日期，并按各目标列的 layout 格式化输出。
// 源值通过 params["parse"] 注入的 ParseMillis 函数解析（复用 larkbitable.ParseEpochMillisCST）；
// 若未注入则退回内置布局列表。解析失败时所有目标列返回空串。
// targets 的 key 是目标列名，value 是 Go time layout（如 "2006"/"01"）。
func deriveDate(sources []string, params map[string]any) map[string]string {
	if len(sources) == 0 || sources[0] == "" {
		return nil
	}
	src := strings.TrimSpace(sources[0])

	// 优先用注入的 ParseMillis（由 ApplyDerivedColumns 调用方通过 params["parse"] 传入）。
	var t time.Time
	if pf, ok := params["parse"]; ok {
		if parseFn, ok := pf.(func(string) (int64, bool)); ok {
			if ms, ok := parseFn(src); ok {
				t = time.UnixMilli(ms).UTC()
			}
		}
	}
	// 退回内置布局（冗余保障，parseFn 应由外部注入覆盖）。
	if t.IsZero() {
		t = parseDateFallback(src)
	}
	if t.IsZero() {
		return nil
	}

	targets, _ := params["targets"].(map[string]string)
	if len(targets) == 0 {
		return nil
	}
	out := make(map[string]string, len(targets))
	// 转为东八区时间以便年月等字段与 Moka 原始值一致。
	cst := time.FixedZone("CST", 8*3600)
	tCST := t.In(cst)
	for col, layout := range targets {
		out[col] = tCST.Format(layout)
	}
	return out
}

// parseDateFallback 按常见中国本地日期布局解析，无时区信息时默认东八区。
var fallbackLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
	"2006年01月02日",
	"2006年01月",
	"2006-01",
	"2006/01",
}

func parseDateFallback(s string) time.Time {
	cst := time.FixedZone("CST", 8*3600)
	for _, layout := range fallbackLayouts {
		if t, err := time.ParseInLocation(layout, s, cst); err == nil {
			return t
		}
	}
	return time.Time{}
}

// deriveSplit 按 params["by"] 分隔符切分源值，按顺序填入 targets 的键（按 targets 的 key 顺序）。
// targets 此处 key 是目标列名，value 忽略（用 sources 顺序对应 targets 顺序）。
// 为保证顺序确定，targets key 与切分结果按 params["order"] []string 声明的顺序对应；
// 若未声明则按 Go map 迭代顺序（不确定），调用方应提供 order。
// 分割结果不足时剩余目标列留空。
func deriveSplit(sources []string, params map[string]any) map[string]string {
	if len(sources) == 0 || sources[0] == "" {
		return nil
	}
	by, _ := params["by"].(string)
	if by == "" {
		return nil
	}
	parts := strings.Split(sources[0], by)

	// 目标列顺序由 params["order"] 提供。
	order, _ := params["order"].([]string)
	if len(order) == 0 {
		return nil
	}
	out := make(map[string]string, len(order))
	for i, col := range order {
		if i < len(parts) {
			out[col] = strings.TrimSpace(parts[i])
		} else {
			out[col] = ""
		}
	}
	return out
}

// deriveRegex 按 params["pattern"] 正则的捕获组提取值，填入 targets。
// targets 的 key 是目标列名，value 是捕获组序号字符串（"1"、"2"...）。
// 不匹配或捕获组越界时对应目标列返回空串。
func deriveRegex(sources []string, params map[string]any) map[string]string {
	if len(sources) == 0 || sources[0] == "" {
		return nil
	}
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	m := re.FindStringSubmatch(sources[0])
	if m == nil {
		return nil
	}
	targets, _ := params["targets"].(map[string]string)
	if len(targets) == 0 {
		return nil
	}
	out := make(map[string]string, len(targets))
	for col, idxStr := range targets {
		idx := 0
		for _, c := range idxStr {
			if c >= '0' && c <= '9' {
				idx = idx*10 + int(c-'0')
			}
		}
		if idx > 0 && idx < len(m) {
			out[col] = m[idx]
		} else {
			out[col] = ""
		}
	}
	return out
}
