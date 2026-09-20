package syncer

import "testing"

// TestPersonSetEqual_OrderAndDuplicateInsensitive 验证集合判等去重、顺序无关：
// 双租户同人多个 open_id、成员顺序不同都不应影响判等。
func TestPersonSetEqual_OrderAndDuplicateInsensitive(t *testing.T) {
	cases := []struct {
		name  string
		a, b  personSet
		equal bool
	}{
		{"完全相同", personSet{"ou_a"}, personSet{"ou_a"}, true},
		{"顺序不同", personSet{"ou_a", "ou_b"}, personSet{"ou_b", "ou_a"}, true},
		{"含重复", personSet{"ou_a", "ou_a", "ou_b"}, personSet{"ou_b", "ou_a"}, true},
		{"空串成员被忽略", personSet{"ou_a", ""}, personSet{"ou_a"}, true},
		{"双方空集合", personSet{}, nil, true},
		{"成员不同", personSet{"ou_a"}, personSet{"ou_b"}, false},
		{"数量不同", personSet{"ou_a"}, personSet{"ou_a", "ou_b"}, false},
		{"一方空另一方非空", nil, personSet{"ou_a"}, false},
	}
	for _, c := range cases {
		if got := c.a.equal(c.b); got != c.equal {
			t.Errorf("%s: equal(%v,%v)=%v，期望 %v", c.name, c.a, c.b, got, c.equal)
		}
	}
}
