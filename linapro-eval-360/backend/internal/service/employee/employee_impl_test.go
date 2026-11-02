// employee_impl_test.go 覆盖 employee_impl.go 中不依赖数据库的派生逻辑（待办状态派生与排序权重）。

package employee

import "testing"

// TestDeriveStatus 验证问卷级待办状态派生：全部已提交优先于截止判断。
func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		completed, total int
		closed           bool
		want             DerivedStatus
	}{
		{completed: 2, total: 2, closed: false, want: DerivedStatusFilled},
		{completed: 2, total: 2, closed: true, want: DerivedStatusFilled},
		{completed: 1, total: 2, closed: true, want: DerivedStatusVoided},
		{completed: 1, total: 2, closed: false, want: DerivedStatusPending},
		{completed: 0, total: 0, closed: false, want: DerivedStatusPending},
	}
	for _, tc := range cases {
		if got := deriveStatus(tc.completed, tc.total, tc.closed); got != tc.want {
			t.Fatalf("deriveStatus(%d,%d,%v) 期望 %s，实际 %s", tc.completed, tc.total, tc.closed, tc.want, got)
		}
	}
}

// TestStatusRank 验证待办排序权重：待填写 < 已完成 < 已作废。
func TestStatusRank(t *testing.T) {
	if !(statusRank(DerivedStatusPending) < statusRank(DerivedStatusFilled) &&
		statusRank(DerivedStatusFilled) < statusRank(DerivedStatusVoided)) {
		t.Fatalf("排序权重不符合 待填写 < 已完成 < 已作废")
	}
}
