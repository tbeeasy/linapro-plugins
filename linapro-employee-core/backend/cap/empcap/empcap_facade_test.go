package empcap

import (
	"context"
	"testing"
)

// fakeService 是用于门面绑定测试的最小 empcap.Service。
type fakeService struct{}

func (fakeService) UpsertEmployee(context.Context, int, Profile) (int64, error) {
	return 1, nil
}
func (fakeService) MarkDeparted(context.Context, int, int64) error { return nil }
func (fakeService) GetByID(context.Context, int64) (*Employee, error) {
	return &Employee{EmpID: 1, Status: StatusActive}, nil
}
func (fakeService) GetByEmployeeNo(context.Context, int, string) (*Employee, error) {
	return &Employee{EmpID: 1, Status: StatusActive}, nil
}
func (fakeService) GetByEmployeeNoAnyTenant(context.Context, string) (*Employee, error) {
	return &Employee{EmpID: 1, Status: StatusActive}, nil
}
func (fakeService) GetByName(context.Context, int, string) (*Employee, error) {
	return &Employee{EmpID: 1, Status: StatusActive}, nil
}
func (fakeService) ListActiveByTenant(context.Context, int) ([]*Employee, error) {
	return nil, nil
}
func (fakeService) ResolveEmpIDByLarkIdentity(context.Context, string, string) (int64, error) {
	return 1, nil
}
func (fakeService) ResolveEmpIDByLarkUserID(context.Context, string, string) (int64, error) {
	return 1, nil
}
func (fakeService) UpsertLarkIdentity(context.Context, string, string, string, string, int64) error {
	return nil
}
func (fakeService) MapLarkOpenIDsByEmployeeNo(context.Context, string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
func (fakeService) MapOwnLarkOpenIDsByEmployeeNo(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (fakeService) MapOwnLarkUserIDsByEmployeeNo(context.Context, string) (LarkUserIDIndex, error) {
	return LarkUserIDIndex{ByEmployeeNo: map[string]string{}}, nil
}
func (fakeService) MapByEmployeeNos(context.Context, int, []string) (map[string]*Employee, error) {
	return map[string]*Employee{}, nil
}
func (fakeService) MapByEmployeeNosAnyTenant(context.Context, []string) (map[string]*Employee, error) {
	return map[string]*Employee{}, nil
}
func (fakeService) MapByNamesAnyTenant(context.Context, []string) (map[string][]*Employee, error) {
	return map[string][]*Employee{}, nil
}
func (fakeService) MapByEmpIDs(context.Context, []int64) (map[int64]*Employee, error) {
	return map[int64]*Employee{}, nil
}

// 编译期断言：fakeService 满足完整的 Service 契约。
var _ Service = fakeService{}

// mapFakeService 在 fakeService 之上绑定一张固定的「工号 → open_id 集合」映射，
// 用于验证多工号版解析闭包的合并、去重与顺序稳定性。
type mapFakeService struct {
	fakeService
	byEmployeeNo map[string][]string
}

func (s mapFakeService) MapLarkOpenIDsByEmployeeNo(context.Context, string) (map[string][]string, error) {
	return s.byEmployeeNo, nil
}

// TestLarkOpenIDResolverByEmployeeNos 验证多工号解析闭包：按输入顺序合并各工号的 open_id、
// 跨工号去重（同人多身份或重复工号不重复返回）、空工号与未命中工号跳过、全未命中返回空集合。
func TestLarkOpenIDResolverByEmployeeNos(t *testing.T) {
	defer BindEmployeeService(BoundEmployeeService()) // restore process-global binding

	BindEmployeeService(mapFakeService{byEmployeeNo: map[string][]string{
		"GZ000070": {"o70"},
		"GZ000071": {"o71a", "o71b"},
		"GZ000060": {"o71a"}, // 与 GZ000071 命中同一 open_id，应被去重
	}})

	resolve, err := LarkOpenIDResolverByEmployeeNos(context.Background(), "a_app")
	if err != nil || resolve == nil {
		t.Fatalf("解析闭包构造失败: err=%v, 闭包是否为 nil=%v", err, resolve == nil)
	}

	// 多工号合并：顺序按输入工号、去重后保留首次出现的位置。
	got := resolve([]string{"GZ000070", "GZ000071", "GZ000060"})
	want := []string{"o70", "o71a", "o71b"}
	if len(got) != len(want) {
		t.Fatalf("open_id 数量: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("open_id 顺序/去重: got %v want %v", got, want)
		}
	}

	// 空工号段与未命中工号跳过。
	if ids := resolve([]string{"", "GZ000999", "GZ000070"}); len(ids) != 1 || ids[0] != "o70" {
		t.Fatalf("空工号与未命中工号应跳过，got %v", ids)
	}
	// 全未命中返回空集合（消费方据此跳过人员列）。
	if ids := resolve([]string{"", "GZ000999"}); len(ids) != 0 {
		t.Fatalf("全未命中应返回空集合，got %v", ids)
	}
}

// TestLarkOpenIDResolverByEmployeeNosUnbound 验证员工服务未绑定时返回 (nil, nil)，
// 消费方据此降级跳过人员列而不阻断同步。
func TestLarkOpenIDResolverByEmployeeNosUnbound(t *testing.T) {
	defer BindEmployeeService(BoundEmployeeService()) // restore process-global binding

	BindEmployeeService(nil)
	resolve, err := LarkOpenIDResolverByEmployeeNos(context.Background(), "a_app")
	if err != nil || resolve != nil {
		t.Fatalf("未绑定时应返回 (nil, nil)，got err=%v, 闭包是否为 nil=%v", err, resolve == nil)
	}
}

// TestBindEmployeeService 验证绑定后解析返回绑定的服务，清空则解绑。
func TestBindEmployeeService(t *testing.T) {
	defer BindEmployeeService(BoundEmployeeService()) // restore process-global binding

	BindEmployeeService(nil)
	if BoundEmployeeService() != nil {
		t.Fatal("expected nil before bind")
	}

	svc := fakeService{}
	BindEmployeeService(svc)
	if BoundEmployeeService() == nil {
		t.Fatal("expected bound service after bind")
	}

	BindEmployeeService(nil)
	if BoundEmployeeService() != nil {
		t.Fatal("expected nil after clearing bind")
	}
}

// TestSecretProviderRoundTrip 验证绑定的密钥提供方被使用，
// 且错误/空结果按失败关闭处理。
func TestSecretProviderRoundTrip(t *testing.T) {
	defer BindTokenSecretProvider(boundSecretFn) // restore process-global provider

	BindTokenSecretProvider(func(context.Context) (string, error) { return "k", nil })
	if s, err := resolveSecret(context.Background()); err != nil || s != "k" {
		t.Fatalf("expected bound secret k, got %q err %v", s, err)
	}
	BindTokenSecretProvider(nil)
	if _, err := resolveSecret(context.Background()); err == nil {
		t.Fatal("expected fail-closed when unbound")
	}
}

// TestGetByEmployeeNoAndGetByNameFacadeRoundTrip 验证两个新方法
// 能通过门面透传到绑定的服务。
func TestGetByEmployeeNoAndGetByNameFacadeRoundTrip(t *testing.T) {
	svc := fakeService{}
	BindEmployeeService(svc)
	defer BindEmployeeService(nil)

	ctx := context.Background()

	emp, err := svc.GetByEmployeeNo(ctx, 1, "GZ001")
	if err != nil || emp == nil || emp.EmpID != 1 {
		t.Fatalf("GetByEmployeeNo: got emp=%v err=%v", emp, err)
	}

	emp, err = svc.GetByName(ctx, 1, "Alice")
	if err != nil || emp == nil || emp.EmpID != 1 {
		t.Fatalf("GetByName: got emp=%v err=%v", emp, err)
	}
}

// userIDFakeService 在 fakeService 之上绑定一张固定的 user_id 索引，
// 用于验证按身份应用取索引的便捷闭包。
type userIDFakeService struct {
	fakeService
	idx LarkUserIDIndex
	err error
}

func (s userIDFakeService) MapOwnLarkUserIDsByEmployeeNo(context.Context, string) (LarkUserIDIndex, error) {
	return s.idx, s.err
}

// TestLarkUserIDResolverByApp 验证便捷闭包原样返回索引的三个集合，不做跨工号合并。
func TestLarkUserIDResolverByApp(t *testing.T) {
	defer BindEmployeeService(BoundEmployeeService()) // restore process-global binding

	want := LarkUserIDIndex{
		ByEmployeeNo:        map[string]string{"GZ000070": "u70"},
		BlankEmployeeNos:    map[string]struct{}{"GZ000071": {}},
		CollidedEmployeeNos: map[string]struct{}{"GZ000072": {}},
	}
	BindEmployeeService(userIDFakeService{idx: want})

	got, err := LarkUserIDResolverByApp(context.Background(), "a_app")
	if err != nil {
		t.Fatalf("LarkUserIDResolverByApp: %v", err)
	}
	_, blank := got.BlankEmployeeNos["GZ000071"]
	_, collided := got.CollidedEmployeeNos["GZ000072"]
	if got.ByEmployeeNo["GZ000070"] != "u70" || !blank || !collided {
		t.Fatalf("索引应原样返回: %+v", got)
	}
}

// TestLarkUserIDResolverByAppUnbound 验证员工服务未绑定时返回明确错误：
// 静默返回空索引会表现成「所有人都没身份」，掩盖装配缺陷。
func TestLarkUserIDResolverByAppUnbound(t *testing.T) {
	defer BindEmployeeService(BoundEmployeeService()) // restore process-global binding

	BindEmployeeService(nil)
	if _, err := LarkUserIDResolverByApp(context.Background(), "a_app"); err == nil {
		t.Fatal("未绑定时应返回错误而不是空索引")
	}
}
