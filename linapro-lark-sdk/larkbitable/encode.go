package larkbitable

import (
	"context"
	"fmt"
	"lina-core/pkg/logger"
	"strconv"
	"strings"
	"time"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// Encoder 承载调用方差异化的值编解码语义，由若干可替换的纯函数字段组成：
//   - CellFallback：读取记录时，非文本类型单元格的兜底字符串化。
//   - UserIDType：人员字段写入时使用的用户 id 类型识别符（open_id/user_id/union_id），
//     默认 "open_id"。调用方经 CreateOp/UpdateOp.Persons 传入的 id 类型必须与本字段一致。
//
// 日期/数字不在 Encoder 中解析：写入方在自身边界把日期解析为 int64 毫秒、数字解析为 float64，
// 直接放进 Row（值为强类型），共享库只做序列化（见 rowToFields）——这样整条链路对同一日期/
// 数字只解析一次，也易于判断「最终写入飞书的到底是什么」。导出的 ParseEpochMillisCST/UTC、
// ParseNumberLoose/Plain 作为纯 helper 供写入方在边界调用。
//
// 人员字段同样不在 Encoder 中编码，也不做任何身份解析：写入的 open_id 由调用方在自身边界解析
// 完毕后经 CreateOp/UpdateOp.Persons 旁路传入（见 rowToFields 的 persons 渲染循环）。
//
// 用函数字段而非接口：皆为纯函数，函数字段最轻，调用侧组合直观。
type Encoder struct {
	CellFallback func(any) string
	UserIDType   string
}

// DefaultEncoder 返回默认组合：通用单元格兜底。
// 不预设 UserIDType：该参数仅在写入「人员字段」时用于指定用户 ID 的解析格式，
// 与其它字段无关。留空时下发处会省略 user_id_type 查询参数（飞书语义为可选，
// 不传即不校验），避免空串触发 99992402 field validation failed。
func DefaultEncoder() Encoder {
	return Encoder{
		CellFallback: defaultCellFallback,
	}
}

// withDefaults 把 Encoder 中为 nil 的函数字段回退到默认实现，保证调用方只覆盖关心的语义。
func (e Encoder) withDefaults() Encoder {
	if e.CellFallback == nil {
		e.CellFallback = defaultCellFallback
	}
	if e.UserIDType == "" {
		e.UserIDType = UserIDTypeOpenID
	}
	return e
}

// rowToFields 将 Row（及可选的附件列、人员列旁路）转换为 SDK 字段映射：Row 的值已是写入方
// 在自身边界解析好的强类型（日期 int64 毫秒、数字 float64、文本 string），此处按 Go 值类型
// verbatim 序列化（int64/float64 编码为 JSON 数字、string 为文本），不再做任何日期/数字解析。
// 附件列渲染为 {file_token} 对象数组；人员列渲染为 [{id}] 对象数组，id 取自 persons 旁路。
//
// fieldTypes 仅用于人员列守卫与错配诊断日志，不再驱动解析：Row 中出现的 TypeUser 列一律跳过
// （见下方分支），因为人员字段值必须是用户 id，而 Row 承载的是普通文本（如工号、姓名），
// 直接写进去会被飞书整批拒绝。id 由调用方解析完毕后经 persons 旁路传入，共享库不做身份解析。
func (c *Client) rowToFields(ctx context.Context, row WriteRow, attachments, persons map[string][]string, fieldTypes map[string]int) map[string]any {
	out := make(map[string]any, len(row)+len(attachments)+len(persons))
	for k, v := range row {
		if fieldTypes[k] == larkbitablesdk.TypeUser {
			// 人员列不走 Row：Row 里是工号/姓名等文本，飞书人员字段只接受用户 id 对象数组，
			// 写入文本会触发 99992402。调用方应把已解析的 open_id 集合放进 persons 旁路
			// （见下方渲染循环）；此处出现的裸值是无视旁路的编码错误，打日志便于定位。
			logger.Errorf(ctx, "lark: 人员字段 %q 的裸值 %v 被忽略——人员列须经 Persons 旁路传入已解析的用户 id", k, v)
			continue
		}
		// 其余列 verbatim 写入：写入方已按飞书列类型交出强类型值（日期 int64/数字 float64/
		// 文本 string），共享库不再按 fieldTypes 二次解析。
		out[k] = v
	}
	for col, tokens := range attachments {
		if len(tokens) == 0 {
			continue
		}
		cells := make([]map[string]any, 0, len(tokens))
		for _, t := range tokens {
			if t == "" {
				continue
			}
			cells = append(cells, map[string]any{"file_token": t})
		}
		if len(cells) > 0 {
			out[col] = cells
		}
	}
	for col, ids := range persons {
		// 与日期/数字/附件「无内容即跳列」一致：空 id 集合不写该列，避免整批因人员字段失败。
		if len(ids) == 0 {
			continue
		}
		list := make([]*larkbitablesdk.Person, 0, len(ids))
		for _, id := range ids {
			if id == "" {
				continue
			}
			// 人员字段写侧仅支持 id，不带 name（官方文档字段值只含 id）。
			list = append(list, larkbitablesdk.NewPersonBuilder().Id(id).Build())
		}
		if len(list) > 0 {
			// 排障：人员列写入结果打日志，确认解析出的 id 与期望一致（user_id_type 决定其语义）。
			logger.Debugf(ctx, "lark: 人员字段 %q -> ids=%v (user_id_type=%s)", col, ids, c.encoder.UserIDType)
			out[col] = list
		}
	}
	return out
}

// cellToString 从 Bitable 字段值提取纯字符串，按字段类型分派，与写侧 rowToFields 的
// TypeUser/TypeText 语义一一对齐：
//   - TypeUser：人员单元格回读 [{id,name,...}]，取 name、按出现顺序去重后用逗号连接。
//     去重是关键：多选人员列在双租户下同一人可能写入多个 open_id（同名多身份），
//     回读会得到重复姓名；不去重则回读串（如 "张三张三"）永远不等于 Moka 明文姓名
//     "张三"，导致人员列每轮同步都被误判为变更并反复重写。
//   - 其余类型（含 TypeText 文本列及一切其它/未知类型）：统一经 textCellToString 做结构
//     扁平化，最终交给注入的 CellFallback 规范化。这样 string 也不能绕过 CellFallback ——
//     注入方（如 report-sync 的 Stringify）的 trim / "-"→"" 对文本叶值同样生效，
//     使读回与写侧（FlattenReport）的规范化严格对称。
//
// 调用方传入字段类型而非让本函数按键名嗅探：类型是我们在 ListFields 时已拿到的权威事实。
func (c *Client) cellToString(v any, fieldType int) string {
	switch fieldType {
	case larkbitablesdk.TypeUser: // 人员/成员字段，有独立姓名语义
		return personCellToString(v)
	default: // TypeText 及一切其它类型：结构扁平化后交 CellFallback 规范化
		return textCellToString(v, c.encoder.CellFallback)
	}
}

// textCellToString 把任意单元格值规范化为文本叶值，再交给注入的 CellFallback 做最终规整。
// 用 switch type 而非 if ok 分支：纯字符串、富文本段数组、其余标量各自成 case，
// 未来新增值形态只需追加 case，不破坏既有路径。
//   - []any：富文本段数组，逐段抽取 "text"，把抽取后的真实分段切片（[]string）交给 fallback，
//     使注入方能对每个分段做归一（逐段 trim、"-"→""），而不是只能拿到一个压平的字符串；
//   - 其余（含 string）：原样交给 fallback。
func textCellToString(v any, fallback func(any) string) string {
	switch t := v.(type) {
	case []any: // 富文本段数组：逐段抽取 "text"
		segs := make([]string, 0, len(t))
		for _, seg := range t {
			if m, ok := seg.(map[string]any); ok {
				if txt, ok := m["text"].(string); ok {
					segs = append(segs, txt)
					continue
				}
			}
			segs = append(segs, fmt.Sprintf("%v", seg))
		}
		return fallback(segs)
	default: // 纯字符串与其它标量统一交给 fallback
		return fallback(v)
	}
}

// personCellToString 人员单元格值字符串化：取各人姓名，按出现顺序去重后逗号连接。
// 逗号连接兼容单/多人单元格；去重保证同名多身份稳定回读为单个姓名，供规划器与 Moka
// 明文姓名做字符串比较。实现上复用 personCellToPersons：遍历飞书原始段、按官方 Person
// 结构取出后投影姓名，遍历逻辑只此一份，姓名串与结构化通道不会对同一段解析不一致。
func personCellToString(v any) string {
	persons := personCellToPersons(v)
	names := make([]string, 0, len(persons))
	seen := make(map[string]struct{}, len(persons))
	for _, p := range persons {
		if p == nil || p.Name == nil || *p.Name == "" {
			continue
		}
		if _, dup := seen[*p.Name]; dup {
			continue
		}
		seen[*p.Name] = struct{}{}
		names = append(names, *p.Name)
	}
	return strings.TrimSpace(strings.Join(names, ","))
}

// personCellToPersons 把人员单元格的飞书原始值（[]any of map）按官方 Person 结构转换。
// 每个段对象的 id/name/en_name/email/avatar_url 取出装进字段；不去重：消费方可能需要
// 同名的多个 id（双租户同人多个 open_id）。段非 map 则跳过；整体无人员段时返回 nil。
// 空串字段不调用对应 setter（Builder 内部以 *Set 标记决定是否写入，与官方 omitempty 一致：
// 未 set 的字段 Build 后为 nil 指针，表示「无」）。
func personCellToPersons(v any) []*larkbitablesdk.Person {
	t, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]*larkbitablesdk.Person, 0, len(t))
	for _, seg := range t {
		m, ok := seg.(map[string]any)
		if !ok {
			continue
		}
		b := larkbitablesdk.NewPersonBuilder()
		if s := anyString(m["id"]); s != "" {
			b.Id(s)
		}
		if s := anyString(m["name"]); s != "" {
			b.Name(s)
		}
		if s := anyString(m["en_name"]); s != "" {
			b.EnName(s)
		}
		if s := anyString(m["email"]); s != "" {
			b.Email(s)
		}
		if s := anyString(m["avatar_url"]); s != "" {
			b.AvatarUrl(s)
		}
		out = append(out, b.Build())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// anyString 把 any 转成 string：非 string 或空串返回空串，由调用方决定是否据此跳过 setter。
func anyString(v any) string {
	s, _ := v.(string)
	return s
}

// defaultCellFallback 是通用单元格兜底：nil→空串，其余按 %v 格式化后 TrimSpace。
// 兼容 textCellToString 抽取出的富文本分段切片（[]string），把各段按空串拼接后 TrimSpace。
func defaultCellFallback(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []string: // 来自 textCellToString 的富文本分段
		return strings.TrimSpace(strings.Join(t, ""))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

// cstZone 是固定的东八区（UTC+8）。Moka 报表日期是不带时区的中国本地墙上时间，故形如
// "2025-10-06" 的纯日期在转毫秒前锚定到东八区零点。
var cstZone = time.FixedZone("CST", 8*3600)

// mokaDateLayouts 是 Moka 报表单元格接受的日期/时间布局，按序尝试；纯日期按东八区零点解释。
var mokaDateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

// ParseEpochMillisUTC 将时间字符串归一化为毫秒级 Unix 时间戳，ISO 字符串按 UTC 解析。
// 兼容三种形态：毫秒时间戳（>10 位数字，原样返回）、秒时间戳（≤10 位数字，乘 1000）、
// ISO 8601 字符串（多布局，无时区者按 UTC）。无法识别返回 ok=false。
func ParseEpochMillisUTC(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// 纯数字：按位数区分秒/毫秒时间戳。
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if len(s) <= 10 {
			return n * 1000, true
		}
		return n, true
	}
	// ISO 字符串：尝试多种常见布局（无时区者按 UTC 解析）。
	layouts := []string{
		"2006-01-02T15:04:05.000Z07:00",
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UnixMilli(), true
		}
	}
	return 0, false
}

// ParseEpochMillisCST 将 Moka 报表时间字符串归一化为毫秒级 Unix 时间戳，按东八区墙上时间
// 解析（ParseInLocation + Moka 布局）。用于解析 Moka 源侧的原始值：既可能是日期文本
// （如 "1985-04-14"），也可能是 epoch 数字。无法识别返回 ok=false。
func ParseEpochMillisCST(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		// 纯数字视为 epoch，按数值量级区分秒/毫秒，而非按位数：
		// 任何合理日期（远至公元 2286 年）的秒级时间戳都 < 1e10，绝对值 ≥ 1e10 的必是毫秒。
		// 按量级判定可正确处理 2001-09-09 前的旧日期毫秒值（不足 13 位、约 4.8e11），
		// 不会像旧的「按位数」判定那样把 10~12 位的毫秒值误当作秒再乘一次 1000。
		const secToMillisCutoff int64 = 1e10
		if n > -secToMillisCutoff && n < secToMillisCutoff {
			return n * 1000, true // 秒级 epoch
		}
		return n, true // 毫秒级 epoch
	}
	for _, layout := range mokaDateLayouts {
		if ts, err := time.ParseInLocation(layout, s, cstZone); err == nil {
			return ts.UnixMilli(), true
		}
	}
	return 0, false
}

// ParseNumberPlain 解析普通数字字符串为 float64（%g 语义）。
func ParseNumberPlain(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var n float64
	_, err := fmt.Sscanf(s, "%g", &n)
	return n, err == nil
}

// ParseNumberLoose 解析数字字符串为 float64，容忍千分位 "," 与结尾百分号 "%"（除以 100）。
func ParseNumberLoose(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	pct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	s = strings.ReplaceAll(s, ",", "")
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	if pct {
		n /= 100
	}
	return n, true
}
