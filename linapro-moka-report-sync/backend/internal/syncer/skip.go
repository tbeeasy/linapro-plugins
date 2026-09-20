package syncer

// skip.go 实现映射级别的「行过滤」：把满足条件的报表行在进入 Coalesce/Normalize/Plan
// 之前丢弃。设计要点：
//   - kind 走封闭的命名注册表 skippers（与 derive.go 的 derivers 同构）。每个 kind 是一个纯
//     函数 Skipper；新增比较方向（如 after/equals）只需注册一个纯函数，管线主体不动。
//   - 语义约束「失败即保留」：源列值为空、源列值或阈值任一无法按 format 解析时，该规则对该行
//     判定为「无法判定」（ok=false），调用方保留该行并记 warn。宁可多同步一行（有幂等冻结兜底、
//     代价有界可观察），也不因格式变更静默丢数据。
//   - 时区锚定：解析统一按东八区零点锚定为毫秒时间戳，不依赖进程本地时区，保证同一份配置在不同
//     部署时区下产出相同判定。

import (
	"strings"
	"time"

	"github.com/gogf/gf/v2/os/gtime"
)

// Skipper 是一个行过滤纯函数：给定一行与一条规则，返回是否应跳过该行（skip）
// 以及规则是否可判定（ok）。ok=false 表示「无法判定」（源列为空、源值或阈值解析失败），
// 调用方据此保留该行。
type Skipper func(row Row, rule SkipRule) (skip bool, ok bool)

// SkipRule 声明一条行过滤规则（与 config.SkipRule 同构，syncer 包不依赖 config）。
type SkipRule struct {
	Kind   string // 比较类型，从 skippers 注册表解析（当前支持 "before"）
	Source string // 源列名（可为派生列）
	Format string // 源列值与阈值的解析格式，用 gtime 方言（Y=年 m=月 d=日）
	Value  string // 阈值字面量，按 Format 解析
}

// SkipWarning 描述一条规则在本轮过滤中「无法判定」的聚合情况，供调用方记 warn。
// 出现该告警通常意味着规则未按预期生效（如 format 与源数据形状不匹配、阈值格式非法、
// source 列名写错），受影响的行都被保留。
type SkipWarning struct {
	Rule Rule   // 触发告警的规则标识（kind/source/format/value）
	Rows int    // 因该规则「无法判定」而被保留的行数
	Why  string // 无法判定的原因
}

// Rule 是 SkipWarning 里回传给调用方的规则标识快照（值拷贝，避免调用方改写）。
type Rule struct {
	Kind   string
	Source string
	Format string
	Value  string
}

// skippers 是封闭的命名注册表，每个 kind 对应一个纯函数。
// 新增一种比较方向只需在此注册一个纯函数，ApplySkipRules 主体不需变动。
var skippers = map[string]Skipper{
	"before": skipBefore,
}

// ApplySkipRules 对 rows 逐行按 rules 判定：任一规则 skip=true 即丢弃该行（规则取并集）。
// 返回保留的行 kept、被丢弃的行数 dropped，以及需要调用方记 warn 的聚合信息 warnings。
// rules 为空时原样返回，不做任何过滤。
//
// 「无法判定」的行会被保留，并按规则聚合进 warnings（规则标识 + 保留行数 + 原因），
// 使「规则未生效」可从日志定位。
func ApplySkipRules(rows []Row, rules []SkipRule) (kept []Row, dropped int, warnings []SkipWarning) {
	if len(rules) == 0 {
		return rows, 0, nil
	}

	// 逐规则统计「无法判定」的行数与未知 kind，本轮结束后聚合成告警。
	undecided := make([]int, len(rules))
	kept = make([]Row, 0, len(rows))

	for _, row := range rows {
		drop := false
		for i, rule := range rules {
			fn, exists := skippers[rule.Kind]
			if !exists {
				// 未知 kind：不丢行，留待下方聚合成一条告警。
				continue
			}
			skip, ok := fn(row, rule)
			if !ok {
				undecided[i]++
				continue
			}
			if skip {
				drop = true
				break
			}
		}
		if drop {
			dropped++
			continue
		}
		kept = append(kept, row)
	}

	for i, rule := range rules {
		snap := Rule{Kind: rule.Kind, Source: rule.Source, Format: rule.Format, Value: rule.Value}
		if _, exists := skippers[rule.Kind]; !exists {
			warnings = append(warnings, SkipWarning{Rule: snap, Rows: len(rows), Why: "未知的 kind，规则未生效，全部行已保留"})
			continue
		}
		if undecided[i] > 0 {
			warnings = append(warnings, SkipWarning{
				Rule: snap,
				Rows: undecided[i],
				Why:  "源列值或阈值无法按 format 解析（或为空），该规则对这些行未生效，行已保留",
			})
		}
	}
	return kept, dropped, warnings
}

// skipBefore 判定源列时间是否早于阈值：早于则跳过（skip=true）。
// 源列值与阈值都按 rule.Format 解析为东八区毫秒时间戳后比较；源值为空或任一侧解析失败
// 返回 ok=false（无法判定，调用方保留该行）。
func skipBefore(row Row, rule SkipRule) (skip bool, ok bool) {
	src := strings.TrimSpace(row[rule.Source])
	if src == "" {
		return false, false
	}
	srcMs, ok1 := parseByFormat(src, rule.Format)
	thMs, ok2 := parseByFormat(rule.Value, rule.Format)
	if !ok1 || !ok2 {
		return false, false
	}
	return srcMs < thMs, true
}

// parseByFormat 按 gtime 方言 format 解析 value，成功时返回东八区零点锚定的毫秒时间戳。
// 经 gtime.StrToTimeFormat 解析（gf 内部把 Y→2006、m→01、d→02）；解析失败或空值返回 ok=false。
//
// gtime 解析时使用进程本地时区（time.Local）。为保证同一份配置在不同部署时区下产出相同的
// 时间戳，这里取解析出的年月日分量，按东八区（东八区零点）重新锚定，不沿用 gtime 的本地时区，
// 与既有 larkbitable.ParseEpochMillisCST 的东八区语义保持一致。
func parseByFormat(value, format string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" || format == "" {
		return 0, false
	}
	t, err := gtime.StrToTimeFormat(value, format)
	if err != nil || t == nil || t.IsZero() {
		return 0, false
	}
	cst := time.FixedZone("CST", 8*3600)
	anchored := time.Date(t.Year(), time.Month(t.Month()), t.Day(), 0, 0, 0, 0, cst)
	return anchored.UnixMilli(), true
}
