// employee_larkuserid_test.go 覆盖 buildLarkUserIDIndex：查询行按工号归入「可投递 / 缺 user_id / 碰撞」
// 三个互不相交的集合，工号为空的行忽略，无行时返回非 nil 的空集合。

package employee

import (
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// assertSet 断言 set 恰好等于 want。
func assertSet(t *testing.T, name string, set map[string]struct{}, want ...string) {
	t.Helper()
	if len(set) != len(want) {
		t.Fatalf("%s want %v, got %v", name, want, set)
	}
	for _, no := range want {
		if _, ok := set[no]; !ok {
			t.Fatalf("%s want %v, got %v", name, want, set)
		}
	}
}

// TestBuildLarkUserIDIndex_ThreeSets 验证命中入 ByEmployeeNo、user_id 为空入 BlankEmployeeNos、
// 工号为空的行被忽略。
func TestBuildLarkUserIDIndex_ThreeSets(t *testing.T) {
	rows := []larkUserIDRow{
		{EmployeeNo: "E1", UserID: "u1"},  // 命中
		{EmployeeNo: "E2", UserID: ""},    // own 行但 user_id 空
		{EmployeeNo: "", UserID: "u3"},    // 工号空，无法寻址，忽略
		{EmployeeNo: "E4", UserID: "u4"},  // 命中
		{EmployeeNo: "E5", UserID: "u5 "}, // 尾部空白不清洗，按原样入 map
		{EmployeeNo: "E6", UserID: ""},    // own 行空 user_id
		{EmployeeNo: "E6", UserID: ""},    // 同一人多条空行只记一次
	}
	idx := buildLarkUserIDIndex(rows)

	want := map[string]string{"E1": "u1", "E4": "u4", "E5": "u5 "}
	if len(idx.ByEmployeeNo) != len(want) {
		t.Fatalf("ByEmployeeNo want %v, got %v", want, idx.ByEmployeeNo)
	}
	for no, uid := range want {
		if idx.ByEmployeeNo[no] != uid {
			t.Fatalf("%s want %q, got %q", no, uid, idx.ByEmployeeNo[no])
		}
	}
	assertSet(t, "BlankEmployeeNos", idx.BlankEmployeeNos, "E2", "E6")
	assertSet(t, "CollidedEmployeeNos", idx.CollidedEmployeeNos)
}

// TestBuildLarkUserIDIndex_CollisionNotSilentlyResolved 验证同一工号出现不同 user_id 时移出
// ByEmployeeNo 并记入碰撞集合——推送场景下取首条等于送错人。
func TestBuildLarkUserIDIndex_CollisionNotSilentlyResolved(t *testing.T) {
	rows := []larkUserIDRow{
		{EmployeeNo: "E1", UserID: "u1a"},
		{EmployeeNo: "E1", UserID: "u1b"},
		{EmployeeNo: "E2", UserID: "u2"},
	}
	idx := buildLarkUserIDIndex(rows)

	if _, ok := idx.ByEmployeeNo["E1"]; ok {
		t.Fatalf("碰撞工号不应出现在 ByEmployeeNo 中: %v", idx.ByEmployeeNo)
	}
	assertSet(t, "CollidedEmployeeNos", idx.CollidedEmployeeNos, "E1")
	if got := idx.ByEmployeeNo["E2"]; got != "u2" {
		t.Fatalf("未碰撞的工号应照常命中, got %q", got)
	}
}

// TestBuildLarkUserIDIndex_DuplicateSameUserIDIsNotCollision 验证同一工号同一 user_id 的重复行（多部门
// JOIN 产物）不算碰撞。
func TestBuildLarkUserIDIndex_DuplicateSameUserIDIsNotCollision(t *testing.T) {
	rows := []larkUserIDRow{
		{EmployeeNo: "E1", UserID: "u1"},
		{EmployeeNo: "E1", UserID: "u1"},
		{EmployeeNo: "E1", UserID: "u1"},
	}
	idx := buildLarkUserIDIndex(rows)

	if got := idx.ByEmployeeNo["E1"]; got != "u1" {
		t.Fatalf("重复同值行应保留命中, got %q", got)
	}
	assertSet(t, "CollidedEmployeeNos", idx.CollidedEmployeeNos)
}

// TestBuildLarkUserIDIndex_CollisionNotRevived 验证碰撞判定后，后续再出现首条 user_id
// 的行也不会让该工号重新进 ByEmployeeNo。
func TestBuildLarkUserIDIndex_CollisionNotRevived(t *testing.T) {
	rows := []larkUserIDRow{
		{EmployeeNo: "E1", UserID: "u1a"},
		{EmployeeNo: "E1", UserID: "u1b"},
		{EmployeeNo: "E1", UserID: "u1a"},
		{EmployeeNo: "E1", UserID: "u1b"},
	}
	idx := buildLarkUserIDIndex(rows)

	if _, ok := idx.ByEmployeeNo["E1"]; ok {
		t.Fatal("碰撞工号不应被后续行复活")
	}
	assertSet(t, "CollidedEmployeeNos", idx.CollidedEmployeeNos, "E1")
}

// TestBuildLarkUserIDIndex_SetsDisjoint 验证三个集合互不相交：同一工号既有空行又有有效行时以有效行为准，
// 既有空行又碰撞时只记碰撞，与行的先后顺序无关。
func TestBuildLarkUserIDIndex_SetsDisjoint(t *testing.T) {
	rows := []larkUserIDRow{
		{EmployeeNo: "E1", UserID: ""},
		{EmployeeNo: "E1", UserID: "u1"},
		{EmployeeNo: "E2", UserID: "u2a"},
		{EmployeeNo: "E2", UserID: ""},
		{EmployeeNo: "E2", UserID: "u2b"},
	}
	idx := buildLarkUserIDIndex(rows)

	if got := idx.ByEmployeeNo["E1"]; got != "u1" {
		t.Fatalf("E1 有有效行时应可投递, got %q", got)
	}
	assertSet(t, "BlankEmployeeNos", idx.BlankEmployeeNos)
	assertSet(t, "CollidedEmployeeNos", idx.CollidedEmployeeNos, "E2")
}

// TestBuildLarkUserIDIndex_EmptyInput 验证无行时返回三个非 nil 的空集合。
func TestBuildLarkUserIDIndex_EmptyInput(t *testing.T) {
	idx := buildLarkUserIDIndex(nil)
	if idx.ByEmployeeNo == nil || idx.BlankEmployeeNos == nil || idx.CollidedEmployeeNos == nil {
		t.Fatalf("应返回非 nil 的空集合，避免调用方判空遗漏: %+v", idx)
	}
	if len(idx.ByEmployeeNo)+len(idx.BlankEmployeeNos)+len(idx.CollidedEmployeeNos) != 0 {
		t.Fatalf("空输入应得到空索引: %+v", idx)
	}
	// 防御性断言：返回类型与 empcap 契约一致。
	var _ empcap.LarkUserIDIndex = idx
}
