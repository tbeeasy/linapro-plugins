package syncer

import "strings"

// Conflict 记录一个字段冲突：共享同一 uniqueField 的两行源数据携带了
// 不同的非空值。先出现的值胜出，另一个值会被上报，以便调用方记录日志
// 而不是静默丢弃数据。
type Conflict struct {
	Key     string
	Field   string
	Kept    string
	Dropped string
}

// CoalesceRows 把共享同一 uniqueField 值的源行合并为单行，
// 从而保证规划阶段不会为同一键生成重复记录。
//
// 行按“先出现者优先”的顺序合并：每个字段的第一个非空值胜出；
// 后续行只填充合并行中仍为空的字段。后续行中与已设置字段值不同的
// 非空值构成 Conflict：保留已有值并上报分歧。
//
// 没有 uniqueField 值的行原样返回（每行一条），不产生冲突。
// 输出保持先出现键的顺序。
func CoalesceRows(rows []Row, uniqueField string) ([]Row, []Conflict) {
	var conflicts []Conflict
	order := make([]string, 0, len(rows))
	groups := make(map[string][]Row)
	for _, r := range rows {
		key := strings.TrimSpace(r[uniqueField])
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}

	out := make([]Row, 0, len(order))
	for _, key := range order {
		g := groups[key]
		if key == "" {
			out = append(out, g...)
			continue
		}
		merged, cs := mergeGroup(g, uniqueField, key)
		conflicts = append(conflicts, cs...)
		out = append(out, merged)
	}
	return out, conflicts
}

// mergeGroup 把共享同一键的行折叠为单行（字段合并规则见 CoalesceRows），
// 并上报分歧。
func mergeGroup(rows []Row, uniqueField, key string) (Row, []Conflict) {
	merged := make(Row, len(rows[0]))
	merged[uniqueField] = key
	var conflicts []Conflict
	for _, r := range rows {
		for k, v := range r {
			if v == "" {
				continue
			}
			existing, ok := merged[k]
			if !ok || existing == "" {
				merged[k] = v
				continue
			}
			if existing != v {
				conflicts = append(conflicts, Conflict{
					Key:     key,
					Field:   k,
					Kept:    existing,
					Dropped: v,
				})
			}
		}
	}
	return merged, conflicts
}
