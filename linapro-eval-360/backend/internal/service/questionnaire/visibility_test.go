// visibility_test.go 覆盖二维题目可见性 AND 工具函数（见 tasks 11.1、FB-10）。
// IsQuestionVisible：visibleRoleIDs（空=全员）∩ 受限评价者排除名单（评价者本人在名单内=不可见）。
package questionnaire

import "testing"

func TestIsQuestionVisible_AllRolesVisibleWhenEmpty(t *testing.T) {
	// visibleRoleIDs 为空 = 全员可见；无排除名单。
	if !IsQuestionVisible(999, 1001, nil, nil) {
		t.Fatalf("空 visibleRoleIDs 应全员可见")
	}
	if !IsQuestionVisible(999, 1001, []int64{}, []int64{}) {
		t.Fatalf("空切片等价于空，应全员可见")
	}
}

func TestIsQuestionVisible_RoleInList(t *testing.T) {
	// 评价者角色在 visibleRoleIDs 内且不在排除名单 → 可见。
	if !IsQuestionVisible(5, 1001, []int64{5, 6}, nil) {
		t.Fatalf("角色在名单内应可见")
	}
}

func TestIsQuestionVisible_RoleNotInList(t *testing.T) {
	// 评价者角色不在 visibleRoleIDs 内 → 不可见。
	if IsQuestionVisible(7, 1001, []int64{5, 6}, nil) {
		t.Fatalf("角色不在名单内应不可见")
	}
}

func TestIsQuestionVisible_EvaluatorExcluded(t *testing.T) {
	// spec 场景「排除指定评价者（按本人、全局）」：题目 visible_roles=[上级]，受限名单含李四；
	// 张三的上级有李四、王五 → 李四看不到，王五能看到。
	const (
		roleSuperior int64 = 5
		empLisi      int64 = 1001
		empWangwu    int64 = 1002
	)
	excluded := []int64{empLisi}
	if IsQuestionVisible(roleSuperior, empLisi, []int64{roleSuperior}, excluded) {
		t.Fatalf("评价者本人在受限名单内应不可见（即使角色命中）")
	}
	if !IsQuestionVisible(roleSuperior, empWangwu, []int64{roleSuperior}, excluded) {
		t.Fatalf("同角色但不在受限名单内的评价者应可见")
	}
}

func TestIsQuestionVisible_ExclusionIgnoresRole(t *testing.T) {
	// 全局生效：被排除的评价者无论以何种角色作答（评谁都一样）都看不到该题。
	excluded := []int64{1001}
	for _, role := range []int64{5, 6, 7} {
		if IsQuestionVisible(role, 1001, nil, excluded) {
			t.Fatalf("受限评价者以角色 %d 作答时仍应不可见", role)
		}
	}
}

func TestIsQuestionVisible_AndIntersection(t *testing.T) {
	// 二维 AND：角色不在名单内 且 评价者被排除 → 两个条件都不满足，仍不可见。
	if IsQuestionVisible(7, 1001, []int64{5}, []int64{1001}) {
		t.Fatalf("两个条件都不满足应不可见")
	}
	// 角色在名单内 + 评价者不在排除名单 → 可见（取交集）。
	if !IsQuestionVisible(5, 1002, []int64{5}, []int64{1001}) {
		t.Fatalf("角色命中且未排除应可见")
	}
	// 角色不在名单内 + 评价者未被排除 → 不可见（维1 单独拦截）。
	if IsQuestionVisible(7, 1002, []int64{5}, []int64{1001}) {
		t.Fatalf("角色不命中应不可见")
	}
}

func TestContainsInt64(t *testing.T) {
	if !containsInt64([]int64{1, 2, 3}, 2) {
		t.Fatalf("应命中 2")
	}
	if containsInt64([]int64{1, 2, 3}, 9) {
		t.Fatalf("不应命中 9")
	}
	if containsInt64(nil, 1) {
		t.Fatalf("空切片不应命中")
	}
}

func TestParseCSVInt64(t *testing.T) {
	got := parseCSVInt64("1, 2 ,,3,2,0,abc")
	if len(got) != 3 {
		t.Fatalf("去空/去零/去重后应剩 3 项，got %v", got)
	}
	// 顺序保持 1,2,3。
	for i, want := range []int64{1, 2, 3} {
		if got[i] != want {
			t.Fatalf("第 %d 项应为 %d，got %d", i, want, got[i])
		}
	}
	if got := parseCSVInt64(""); len(got) != 0 {
		t.Fatalf("空串应返回空切片，got %v", got)
	}
}

func TestJoinCSVInt64(t *testing.T) {
	if got := joinCSVInt64([]int64{1, 2, 3}); got != "1,2,3" {
		t.Fatalf("拼接应为 1,2,3，got %q", got)
	}
	if got := joinCSVInt64([]int64{1, 0, 1, 2}); got != "1,2" {
		t.Fatalf("去零去重应为 1,2，got %q", got)
	}
	if got := joinCSVInt64(nil); got != "" {
		t.Fatalf("空切片应空串，got %q", got)
	}
}
