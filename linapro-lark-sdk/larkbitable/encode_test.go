package larkbitable

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	larkbitablesdk "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
)

// TestParseEpochMillisUTC 覆盖 UTC 日期解析：毫秒/秒时间戳、多种 ISO 布局、无法识别输入。
// 飞书日期字段要求毫秒级 Unix 时间戳，解析错误会触发 DatetimeFieldConvFail，故逐形态锁定行为。
func TestParseEpochMillisUTC(t *testing.T) {
	wantMillis := time.Date(2026, 8, 27, 10, 30, 0, 0, time.UTC).UnixMilli()
	cases := []struct {
		name   string
		in     string
		want   int64
		wantOK bool
	}{
		{"毫秒时间戳原样返回", "1787653800000", 1787653800000, true},
		{"秒时间戳乘1000", "1787653800", 1787653800000, true},
		{"Zulu毫秒ISO", "2026-08-27T10:30:00.000Z", wantMillis, true},
		{"RFC3339带偏移", "2026-08-27T18:30:00+08:00", wantMillis, true},
		{"无时区ISO按UTC解析", "2026-08-27T10:30:00", wantMillis, true},
		{"空格分隔日期时间", "2026-08-27 10:30:00", wantMillis, true},
		{"纯日期", "2026-08-27", time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC).UnixMilli(), true},
		{"空串失败", "", 0, false},
		{"非时间字符串失败", "视频面试", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseEpochMillisUTC(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("millis: got %d want %d", got, tc.want)
			}
		})
	}
}

// TestParseEpochMillisCST 覆盖东八区日期解析：纯日期锚定东八区零点、带时分秒、毫秒/秒
// 时间戳。与 UTC 版对比，同一纯日期字符串应得到相差 8 小时的毫秒值。
func TestParseEpochMillisCST(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   int64
		wantOK bool
	}{
		{"纯日期锚定东八区零点", "2025-10-06", time.Date(2025, 10, 6, 0, 0, 0, 0, cstZone).UnixMilli(), true},
		{"东八区日期时间", "2025-10-06 12:34:56", time.Date(2025, 10, 6, 12, 34, 56, 0, cstZone).UnixMilli(), true},
		{"斜杠日期", "2025/10/06", time.Date(2025, 10, 6, 0, 0, 0, 0, cstZone).UnixMilli(), true},
		{"毫秒时间戳原样返回", "1759680000000", 1759680000000, true},
		{"秒时间戳乘1000", "1759680000", 1759680000000, true},
		// 回归：2001-09-09 前的日期毫秒值不足 13 位（1985-04-14 约 4.8e11、12 位），
		// 归一后再次经本函数不得被误判为秒而重复乘 1000（见 NormalizeColumns→rowToFields 双次解析）。
		{"归一后旧日期毫秒幂等", strconv.FormatInt(time.Date(1985, 4, 14, 0, 0, 0, 0, cstZone).UnixMilli(), 10), time.Date(1985, 4, 14, 0, 0, 0, 0, cstZone).UnixMilli(), true},
		{"纯日期旧日期解析", "1985-04-14", time.Date(1985, 4, 14, 0, 0, 0, 0, cstZone).UnixMilli(), true},
		{"空串失败", "", 0, false},
		{"非时间字符串失败", "无", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseEpochMillisCST(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("millis: got %d want %d", got, tc.want)
			}
		})
	}
	// 同一纯日期，CST 比 UTC 早 8 小时到达零点，故 CST 毫秒值应比 UTC 小 8 小时。
	utc, _ := ParseEpochMillisUTC("2025-10-06")
	cst, _ := ParseEpochMillisCST("2025-10-06")
	if utc-cst != int64(8*time.Hour/time.Millisecond) {
		t.Fatalf("UTC 与 CST 纯日期毫秒差应为 8h，got utc=%d cst=%d diff=%d", utc, cst, utc-cst)
	}
}

// TestParseNumberPlain 覆盖普通数字解析。
func TestParseNumberPlain(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   float64
		wantOK bool
	}{
		{"整数", "3", 3, true},
		{"小数", "3.14", 3.14, true},
		{"科学计数", "1e3", 1000, true},
		{"空串失败", "", 0, false},
		// Sscanf("%g") 解析前导数字后忽略结尾 "%"，与原 recruit parseFloat 行为一致。
		{"含百分号取前导数字", "50%", 50, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseNumberPlain(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("num: got %v want %v", got, tc.want)
			}
		})
	}
}

// TestParseNumberLoose 覆盖容忍百分号与千分位的数字解析。
func TestParseNumberLoose(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   float64
		wantOK bool
	}{
		{"普通整数", "42", 42, true},
		{"千分位", "1,234", 1234, true},
		{"百分号除以100", "50%", 0.5, true},
		{"千分位加小数", "12,345.67", 12345.67, true},
		{"空串失败", "", 0, false},
		{"非数字失败", "abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseNumberLoose(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("num: got %v want %v", got, tc.want)
			}
		})
	}
}

// TestRowToFieldsDefault 验证 rowToFields 的 verbatim 序列化：写入方交出的强类型值（日期
// int64 毫秒、数字 float64、文本 string）按 Go 值类型原样写入，共享库不再按 fieldTypes 解析。
func TestRowToFieldsDefault(t *testing.T) {
	c := NewClient("id", "secret")
	fieldTypes := map[string]int{
		"面试时间": larkbitablesdk.TypeDateTime,
		"轮次":   larkbitablesdk.TypeText,
		"应约人数": larkbitablesdk.TypeNumber,
	}
	out := c.rowToFields(context.Background(), WriteRow{
		"面试时间": int64(1787653800000),
		"轮次":   "一面",
		"应约人数": float64(3),
	}, nil, nil, fieldTypes)

	if got, ok := out["面试时间"].(int64); !ok || got != 1787653800000 {
		t.Fatalf("面试时间: got %v (%T) want int64 1787653800000", out["面试时间"], out["面试时间"])
	}
	if got, ok := out["轮次"].(string); !ok || got != "一面" {
		t.Fatalf("轮次: got %v want string 一面", out["轮次"])
	}
	if got, ok := out["应约人数"].(float64); !ok || got != 3 {
		t.Fatalf("应约人数: got %v want float64 3", out["应约人数"])
	}

	// 文本值即使为空串也 verbatim 写入（与旧文本列语义一致，日期/数字的空值跳列由写入方负责）。
	emptyOut := c.rowToFields(context.Background(), WriteRow{"轮次": ""}, nil, nil, fieldTypes)
	if got, ok := emptyOut["轮次"].(string); !ok || got != "" {
		t.Fatalf("空文本应 verbatim 写入空串，got %v (%T)", emptyOut["轮次"], emptyOut["轮次"])
	}
}

// TestRowToFieldsAttachments 验证附件列编码为 file_token 对象数组，空 token 被过滤。
func TestRowToFieldsAttachments(t *testing.T) {
	c := NewClient("id", "secret")
	out := c.rowToFields(
		context.Background(),
		WriteRow{"姓名": "张三"},
		map[string][]string{"简历": {"tok_a", "", "tok_b"}},
		nil,
		map[string]int{"姓名": larkbitablesdk.TypeText, "简历": larkbitablesdk.TypeText},
	)
	cells, ok := out["简历"].([]map[string]any)
	if !ok {
		t.Fatalf("简历列应为 []map[string]any，got %T", out["简历"])
	}
	if len(cells) != 2 {
		t.Fatalf("空 token 应被过滤，期望 2 个附件单元格，got %d", len(cells))
	}
	if cells[0]["file_token"] != "tok_a" || cells[1]["file_token"] != "tok_b" {
		t.Fatalf("附件 token 顺序或内容不符: %v", cells)
	}
}

// TestRowToFieldsUser 验证人员字段（TypeUser）的编码只走 persons 旁路：旁路传入一个/多个
// open_id 时写为 [{id}] 对象数组（写侧仅支持 id，不带 name）；旁路为空时跳列（不阻断整行）。
// Row 中出现的同名人员列是普通文本（如工号），必须被忽略——人员字段不接受纯字符串。
func TestRowToFieldsUser(t *testing.T) {
	fieldTypes := map[string]int{"面试官": larkbitablesdk.TypeUser, "轮次": larkbitablesdk.TypeText}

	// 单个 open_id：写单元素数组，仅含 Id（人员字段写侧只支持 id，不带 name）。
	out := c0(t).rowToFields(context.Background(), WriteRow{"轮次": "一面"},
		nil, map[string][]string{"面试官": {"ou_zhang"}}, fieldTypes)
	persons, ok := out["面试官"].([]*larkbitablesdk.Person)
	if !ok || len(persons) != 1 {
		t.Fatalf("应编码为 1 元素人员数组，got %T len=%d", out["面试官"], len(persons))
	}
	if persons[0].Id == nil || *persons[0].Id != "ou_zhang" || persons[0].Name != nil {
		t.Fatalf("人员元素应仅含 Id，got %+v", persons[0])
	}
	if got, ok := out["轮次"].(string); !ok || got != "一面" {
		t.Fatalf("轮次应原样写入，got %v", out["轮次"])
	}

	// 多个 open_id（双租户同人多身份）：全部写入。
	multi := c0(t).rowToFields(context.Background(), WriteRow{},
		nil, map[string][]string{"面试官": {"ou_li_a", "ou_li_b"}}, fieldTypes)
	if persons, ok := multi["面试官"].([]*larkbitablesdk.Person); !ok || len(persons) != 2 {
		t.Fatalf("应编码为 2 元素人员数组，got %T len=%d", multi["面试官"], len(persons))
	}

	// 旁路为空集合 / 空 id：跳列（飞书人员字段不接受空数组，也绝不回退 Row 里的文本）。
	if empty := c0(t).rowToFields(context.Background(), WriteRow{"面试官": "GZ000040"},
		nil, map[string][]string{"面试官": {}}, fieldTypes); len(empty) != 0 {
		t.Fatalf("空 id 集合应跳列，但输出为: %v", empty)
	}
	if empty := c0(t).rowToFields(context.Background(), WriteRow{"面试官": "GZ000040"},
		nil, map[string][]string{"面试官": {"", ""}}, fieldTypes); len(empty) != 0 {
		t.Fatalf("空串 id 应跳列，但输出为: %v", empty)
	}
	// 未传旁路：Row 中的人员列裸值一律忽略（防止工号/姓名写进人员字段被飞书整批拒绝）。
	if skipped := c0(t).rowToFields(context.Background(), WriteRow{"面试官": "GZ000040"}, nil, nil, fieldTypes); len(skipped) != 0 {
		t.Fatalf("无旁路时人员列应跳过，但输出为: %v", skipped)
	}
}

// c0 构造一个无 HTTP 替身的裸 Client，供纯编码（rowToFields）用例使用。
func c0(t *testing.T) *Client {
	t.Helper()
	return NewClient("id", "secret")
}

// TestEncoderUserIDTypeDefault 验证 Encoder 的 UserIDType 默认回退为 open_id，
// 显式设置为 user_id 时保留。人员字段编码的 id 类型必须与 Bitable 请求的 user_id_type 一致，
// 否则飞书会拒绝（open_id 应用内唯一、不可跨应用交叉）。
func TestEncoderUserIDTypeDefault(t *testing.T) {
	def := DefaultEncoder().withDefaults()
	if def.UserIDType != UserIDTypeOpenID {
		t.Fatalf("默认 UserIDType 应为 open_id，got %q", def.UserIDType)
	}
	custom := (Encoder{UserIDType: UserIDTypeUserID}).withDefaults()
	if custom.UserIDType != UserIDTypeUserID {
		t.Fatalf("显式 UserIDType 应保留，got %q", custom.UserIDType)
	}
}

// TestCellToStringDefault 验证文本字段按类型回读：纯字符串、富文本段数组扁平化。
func TestCellToStringDefault(t *testing.T) {
	c := NewClient("id", "secret")
	// 文本列按 TypeText 分发：纯字符串 TrimSpace。
	if got := c.cellToString("  hi ", larkbitablesdk.TypeText); got != "hi" {
		t.Fatalf("纯字符串应 TrimSpace，got %q", got)
	}
	rich := []any{
		map[string]any{"text": "第一段"},
		map[string]any{"text": "第二段"},
	}
	if got := c.cellToString(rich, larkbitablesdk.TypeText); got != "第一段第二段" {
		t.Fatalf("富文本应扁平化拼接，got %q", got)
	}
	// 未知类型（fieldTypes 缺键为 0）走 CellFallback：nil 兜底为空串。
	if got := c.cellToString(nil, 0); got != "" {
		t.Fatalf("nil 应兜底为空串，got %q", got)
	}
}

// TestCellToStringTextIgnoresName 验证 TypeText 分支只读富文本段的 "text" 字段，不嗅探 "name"。
// 文本列即使值里出现人员结构（含 "name" 无 "text"），也不会按姓名处理，而是按段 %v 兜底。
func TestCellToStringTextIgnoresName(t *testing.T) {
	c := NewClient("id", "secret")
	personLike := []any{
		map[string]any{"id": "ou_zhang", "name": "张三"},
	}
	if got := c.cellToString(personLike, larkbitablesdk.TypeText); got == "张三" {
		t.Fatalf("TypeText 不应把人员结构按姓名嗅探处理，got %q", got)
	}
}

// TestCellToStringPersonCellTypeUser 验证人员字段按 TypeUser 回读：取 name、按出现顺序去重后
// 逗号连接。这是修复「人员列每轮同步被误判为变更并反复重写」的关键：双租户同名多身份写入
// 多个 open_id 后，回读会得到重复姓名（如 [张三,张三]），必须去重为 "张三" 才能与 Moka 明文
// 姓名比较相等；不去重则回读串永远不等于明文，触发无休止的重写与 99992402。
//
// 按类型分发意味着人员单元格只在 TypeUser 列被这样处理——文本列即使出现相同结构，
// 也走 TypeText 分支、不适用本条语义。
func TestCellToStringPersonCellTypeUser(t *testing.T) {
	c := NewClient("id", "secret")

	// 单人：取 name。
	single := []any{
		map[string]any{"id": "ou_zhang", "name": "张三"},
	}
	if got := c.cellToString(single, larkbitablesdk.TypeUser); got != "张三" {
		t.Fatalf("单人应回读为姓名，got %q", got)
	}

	// 同名多身份（双租户）：去重为单个姓名，方能与 Moka 明文 "张三" 比较相等。
	dupe := []any{
		map[string]any{"id": "ou_zhang_a", "name": "张三"},
		map[string]any{"id": "ou_zhang_b", "name": "张三"},
	}
	if got := c.cellToString(dupe, larkbitablesdk.TypeUser); got != "张三" {
		t.Fatalf("同名多身份应去重为单姓名，got %q", got)
	}

	// 多个不同人：按出现顺序去重后逗号连接。
	multi := []any{
		map[string]any{"id": "ou_a", "name": "张三"},
		map[string]any{"id": "ou_b", "name": "李四"},
		map[string]any{"id": "ou_c", "name": "张三"},
	}
	if got := c.cellToString(multi, larkbitablesdk.TypeUser); got != "张三,李四" {
		t.Fatalf("多人应去重后逗号连接，got %q", got)
	}
}

// TestPersonCellToPersons 验证人员单元格按官方 Person 结构转换：每个段对象的
// id/name/en_name/email/avatar_url 都进对应字段；姓名串（Fields 投影）由此派生，
// 两者遍历一致。不去重：双租户同名多 open_id 时 Persons 保留全部 id。
func TestPersonCellToPersons(t *testing.T) {
	cell := []any{
		map[string]any{
			"id":         "ou_zhang",
			"name":       "张三",
			"en_name":    "Zhang San",
			"email":      "zhangsan@feishu.cn",
			"avatar_url": "https://example.com/a.png",
		},
		map[string]any{
			"id":      "ou_zhang_b",
			"name":    "张三", // 同名多身份（双租户）
			"email":   "",
			"en_name": "",
		},
	}
	persons := personCellToPersons(cell)
	if len(persons) != 2 {
		t.Fatalf("应转换出 2 个 Person，got %d", len(persons))
	}
	// 第一人：全字段就位。
	p0 := persons[0]
	if p0.Id == nil || *p0.Id != "ou_zhang" {
		t.Fatalf("第一人 Id 应为 ou_zhang，got %v", p0.Id)
	}
	if p0.Name == nil || *p0.Name != "张三" {
		t.Fatalf("第一人 Name 应为 张三，got %v", p0.Name)
	}
	if p0.EnName == nil || *p0.EnName != "Zhang San" {
		t.Fatalf("第一人 EnName 应为 Zhang San，got %v", p0.EnName)
	}
	if p0.Email == nil || *p0.Email != "zhangsan@feishu.cn" {
		t.Fatalf("第一人 Email 应为 zhangsan@feishu.cn，got %v", p0.Email)
	}
	if p0.AvatarUrl == nil || *p0.AvatarUrl != "https://example.com/a.png" {
		t.Fatalf("第一人 AvatarUrl 应为头像链接，got %v", p0.AvatarUrl)
	}
	// 第二人：同名多身份，open_id 不同且都保留（不去重）；空串字段为 nil 指针。
	p1 := persons[1]
	if p1.Id == nil || *p1.Id != "ou_zhang_b" {
		t.Fatalf("第二人 Id 应为 ou_zhang_b，got %v", p1.Id)
	}
	if p1.Name == nil || *p1.Name != "张三" {
		t.Fatalf("第二人 Name 应为 张三（同名多身份保留），got %v", p1.Name)
	}
	if p1.EnName != nil {
		t.Fatalf("第二人 EnName 空串应未 set（nil 指针），got %v", p1.EnName)
	}
	if p1.Email != nil {
		t.Fatalf("第二人 Email 空串应未 set（nil 指针），got %v", p1.Email)
	}
	// 姓名串投影：由 Persons 派生，同名去重为单个姓名。
	if got := personCellToString(cell); got != "张三" {
		t.Fatalf("姓名串应由 Persons 派生并去重，got %q", got)
	}
}

// TestCellToStringInjectedFallback 验证注入的 CellFallback 生效（如 report-sync 的 "-" 归零）。
// 合并后 string 也走 CellFallback："-" 经注入兜底归一为空串；数字类型同样走 CellFallback。
func TestCellToStringInjectedFallback(t *testing.T) {
	c := NewClient("id", "secret").WithEncoder(Encoder{
		CellFallback: func(v any) string {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s == "-" { // 模拟 report-sync 的 Stringify：占位符 "-" 归一为空
				return ""
			}
			return s
		},
	})
	// 数字类型走 CellFallback。
	if got := c.cellToString(float64(42), larkbitablesdk.TypeNumber); got != "42" {
		t.Fatalf("注入兜底应把 42 格式化为 \"42\"，got %q", got)
	}
	// 字符串也走 CellFallback："-" 归零。
	if got := c.cellToString("-", larkbitablesdk.TypeText); got != "" {
		t.Fatalf("字符串 \"-\" 经注入兜底应归一为空串，got %q", got)
	}
}

// TestCellToStringInjectedFallbackSegments 验证富文本段数组交给注入的 CellFallback 时，
// 注入方拿到的是抽取后的真实分段切片（[]string），而非压平的单字符串——从而能对每个分段
// 做归一（逐段 trim、逐段 "-"→""），这正是 report-sync 的 Stringify 需要的。用可识别 []string
// 的兜底（模拟真实 Stringify）验证逐段处理生效。
func TestCellToStringInjectedFallbackSegments(t *testing.T) {
	c := NewClient("id", "secret").WithEncoder(Encoder{
		CellFallback: func(v any) string {
			if segs, ok := v.([]string); ok {
				parts := make([]string, 0, len(segs))
				for _, seg := range segs {
					seg = strings.TrimSpace(seg)
					if seg == "-" {
						seg = ""
					}
					parts = append(parts, seg)
				}
				return strings.TrimSpace(strings.Join(parts, ""))
			}
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s == "-" {
				return ""
			}
			return s
		},
	})
	rich := []any{
		map[string]any{"text": " 第一段 "},
		map[string]any{"text": "-"},
		map[string]any{"text": "第二段"},
	}
	// 首段 trim、中段 "-" 归零、末段保留；拼接后不含多余空白。
	if got := c.cellToString(rich, larkbitablesdk.TypeText); got != "第一段第二段" {
		t.Fatalf("富文本应逐段归一后拼接，got %q", got)
	}
}
