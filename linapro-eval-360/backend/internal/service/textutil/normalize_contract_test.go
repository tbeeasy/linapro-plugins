package textutil

import (
	"strings"
	"testing"
)

// TestNormalizeName_ConsistencyWithRoleName 锁定一个跨包契约：
// 角色名、项目名、问卷名共用同一归一化，任何未来改动都必须三者一致。
// 这里用一组「应当等价」的输入做双盲断言，防止规则被单点修改后分叉。
//
// 注意等价类只包含**空白差异**：全角空格与半角空格、连续与单个、首尾与内部，
// 归一化后都是单个半角空格或消失；而「无空格」与「有空格」是两个不同名称，不可混为一组。
func TestNormalizeName_ConsistencyWithRoleName(t *testing.T) {
	groups := [][]string{
		// 有争议的空白一律折叠为单个内部半角空格。
		{"上 级", "上　级", "  上 \t 级  ", "上　 级"},
		// 无空格的连续写法自成一组。
		{"上级", " 上级 ", "上级\n", "　上级"},
		// 全角/半角混排的复合名称。
		{"2026 年度测评", " 2026 年度测评 ", "2026　年度测评", "2026  \t年度测评"},
	}
	for _, group := range groups {
		want := NormalizeName(group[0])
		if want == "" {
			t.Fatalf("基准值不应为空：%q", group[0])
		}
		for _, variant := range group {
			if got := NormalizeName(variant); got != want {
				t.Fatalf("同组输入应归一化到同一结果：基准 %q → %q，变体 %q → %q",
					group[0], want, variant, got)
			}
		}
	}
}

// TestNormalizeName_SpaceIsSignificant 反向锁定：全角空格折叠为半角空格，而非删除。
// 若未来有人把 "　" 直接替换成空串，这条会红——那会让「上 级」与「上级」误判为同名。
func TestNormalizeName_SpaceIsSignificant(t *testing.T) {
	if NormalizeName("上　级") == NormalizeName("上级") {
		t.Fatal("全角空格应折叠为半角空格而非删除；有空格与无空格是不同名称")
	}
	if got := NormalizeName("上　级"); got != "上 级" {
		t.Fatalf("全角空格应转为单个半角空格，got=%q", got)
	}
}

// TestNormalizeName_LongerThanColumn 确认归一化不会截断或扩充内容
// （项目名/问卷名落库列为 VARCHAR(255)，超长由数据库拒绝，工具不越权处理）。
func TestNormalizeName_NoTruncation(t *testing.T) {
	long := strings.Repeat("测", 300)
	if got := NormalizeName(long); len([]rune(got)) != 300 {
		t.Fatalf("归一化不应改变长度，want=300 got=%d", len([]rune(got)))
	}
}
