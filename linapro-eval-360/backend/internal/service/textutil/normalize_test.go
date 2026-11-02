package textutil

import "testing"

func TestNormalizeName_CollapseAndTrim(t *testing.T) {
	if got := NormalizeName("  2026 年度   测评  "); got != "2026 年度 测评" {
		t.Fatalf("内部空白未折叠或首尾未去，got=%q", got)
	}
}

func TestNormalizeName_FullWidthSpace(t *testing.T) {
	// 全角空格应转为半角并参与折叠。
	if got := NormalizeName("年度　 测评"); got != "年度 测评" {
		t.Fatalf("全角空格未归一，got=%q", got)
	}
}

func TestNormalizeName_TabsAndNewlines(t *testing.T) {
	if got := NormalizeName("年度\t测评\n问卷"); got != "年度 测评 问卷" {
		t.Fatalf("tab/换行未折叠，got=%q", got)
	}
}

func TestNormalizeName_CasePreserved(t *testing.T) {
	// 大小写不折叠：两者归一化后应保持不同，从而不视为重名。
	if NormalizeName("Leader") == NormalizeName("leader") {
		t.Fatal("大小写不应被折叠")
	}
}

func TestNormalizeName_BlankBecomesEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n", "　　"} {
		if got := NormalizeName(in); got != "" {
			t.Fatalf("纯空白应归一化为空串，输入 %q got=%q", in, got)
		}
	}
}

func TestNormalizeName_Idempotent(t *testing.T) {
	// 归一化应幂等：再归一化一次结果不变（查重前可能被多次调用）。
	once := NormalizeName("  年度   测评  ")
	if twice := NormalizeName(once); twice != once {
		t.Fatalf("归一化不幂等，once=%q twice=%q", once, twice)
	}
}

func TestNormalizeEmployeeNo(t *testing.T) {
	if got := NormalizeEmployeeNo("  E-001　"); got != "E-001" {
		t.Fatalf("工号首尾空白/全角空格未去除，got=%q", got)
	}
	// 工号内部空白不折叠。
	if got := NormalizeEmployeeNo("E 001"); got != "E 001" {
		t.Fatalf("工号内部空白不应被折叠，got=%q", got)
	}
	// 大小写不折叠。
	if NormalizeEmployeeNo("e001") == NormalizeEmployeeNo("E001") {
		t.Fatal("工号大小写不应被折叠")
	}
}
