// employee_v1_test.go 验证后台员工控制器的边界行为：
//
//   - 绑定飞书身份前先按应用凭据校验通讯录存在性；应用未配置、用户查不到都
//     在写库之前拒绝（此时员工服务未被触碰）。
//   - 租户名按当前页租户集合一次批量补齐；租户能力不可用或缺租户时降级为空名。
//   - 宿主用户能力不可用时，后台账号候选返回空列表而不是放开查询。
package employee

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gogf/gf/v2/container/gvar"

	"lina-core/pkg/plugin/capability/capmodel"
	"lina-core/pkg/plugin/capability/hostconfigcap"
	"lina-core/pkg/plugin/capability/tenantcap"
	v1 "lina-plugin-linapro-employee-core/backend/api/employee/v1"
	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
	"linapro-lark-sdk/larkcontact"
)

// fakeHostConfig 是 hostconfigcap.Service 的最小假实现，只回答 lark_apps。
type fakeHostConfig struct {
	apps any
}

func (f *fakeHostConfig) Get(_ context.Context, _ string, defaultValue any) (*gvar.Var, error) {
	if f.apps == nil {
		return nil, nil
	}
	return gvar.New(f.apps), nil
}

func (f *fakeHostConfig) Exists(context.Context, string) (bool, error) { return true, nil }
func (f *fakeHostConfig) String(_ context.Context, _ string, d string) (string, error) {
	return d, nil
}
func (f *fakeHostConfig) Bool(_ context.Context, _ string, d bool) (bool, error) { return d, nil }
func (f *fakeHostConfig) Int(_ context.Context, _ string, d int) (int, error)    { return d, nil }
func (f *fakeHostConfig) Duration(_ context.Context, _ string, d time.Duration) (time.Duration, error) {
	return d, nil
}

// SysConfig 在被调用即失败：本控制器的测试路径都经 Get 读配置，
// 走到 SysConfig 说明依赖用错了地方。
func (f *fakeHostConfig) SysConfig() hostconfigcap.SysConfigService {
	panic("fakeHostConfig: SysConfig 不应被调用")
}

// fakeTenantDir 记录 BatchGet 的入参与调用次数，并回放预设的租户名。
type fakeTenantDir struct {
	tenantcap.DirectoryService
	names []string
	calls int
	got   []tenantcap.TenantID
	err   error
}

func (f *fakeTenantDir) BatchGet(_ context.Context, ids []tenantcap.TenantID) (*capmodel.BatchResult[*tenantcap.TenantInfo, tenantcap.TenantID], error) {
	f.calls++
	f.got = ids
	if f.err != nil {
		return nil, f.err
	}
	out := &capmodel.BatchResult[*tenantcap.TenantInfo, tenantcap.TenantID]{Items: map[tenantcap.TenantID]*tenantcap.TenantInfo{}}
	for i, id := range ids {
		name := ""
		if i < len(f.names) {
			name = f.names[i]
		}
		out.Items[id] = &tenantcap.TenantInfo{ID: id, Name: name}
	}
	return out, nil
}

// fakeTenantService 只提供 Directory()。
type fakeTenantService struct {
	tenantcap.Service
	dir *fakeTenantDir
}

func (f *fakeTenantService) Directory() tenantcap.DirectoryService { return f.dir }

// fakeContact 模拟飞书通讯录客户端的单用户查询。
type fakeContact struct {
	user larkcontact.User
	err  error
	// called 记录被查询的 open_id。
	called []string
}

func (f *fakeContact) GetUser(_ context.Context, openID string) (larkcontact.User, error) {
	f.called = append(f.called, openID)
	if f.err != nil {
		return larkcontact.User{}, f.err
	}
	return f.user, nil
}

// larkAppsConfig 构造一份含指定应用的 lark_apps 配置（与本插件运行期配置同构）。
func larkAppsConfig(appID, secret, name string) []map[string]any {
	return []map[string]any{{"app_id": appID, "secret_key": secret, "name": name}}
}

// TestBindLarkIdentityRejectsUnconfiguredApp 验证：应用不在 lark_apps 中时直接按
// 凭据缺失拒绝，且不发起任何飞书调用、不写库（员工服务为 nil 也不会被触碰）。
func TestBindLarkIdentityRejectsUnconfiguredApp(t *testing.T) {
	var contactCalls int
	ctrl := NewV1(nil, &fakeHostConfig{apps: larkAppsConfig("cli_known", "s", "")}, nil, nil,
		func(string, string) ContactClient {
			contactCalls++
			return &fakeContact{}
		})

	_, err := ctrl.BindLarkIdentity(context.Background(), &v1.BindLarkIdentityReq{
		EmpId: 1, LarkAppId: "cli_unknown", OpenId: "ou_x",
	})
	if !errors.Is(err, lark.ErrCredentialMissing) {
		t.Fatalf("未配置的应用应返回 ErrCredentialMissing, got %v", err)
	}
	if contactCalls != 0 {
		t.Fatalf("未配置的应用不应构造飞书客户端, calls=%d", contactCalls)
	}
}

// TestBindLarkIdentityRejectsUnknownLarkUser 验证：飞书查不到该 open_id 时返回
// ErrLarkUserNotFound，并且不落库（员工服务未被调用）。
func TestBindLarkIdentityRejectsUnknownLarkUser(t *testing.T) {
	contact := &fakeContact{err: larkcontact.ErrUserNotFound}
	ctrl := NewV1(nil, &fakeHostConfig{apps: larkAppsConfig("cli_a", "secret", "")}, nil, nil,
		func(appID, secret string) ContactClient {
			if appID != "cli_a" || secret != "secret" {
				t.Fatalf("应使用配置中的应用凭据, got %s/%s", appID, secret)
			}
			return contact
		})

	_, err := ctrl.BindLarkIdentity(context.Background(), &v1.BindLarkIdentityReq{
		EmpId: 1, LarkAppId: "cli_a", OpenId: "ou_missing",
	})
	if !errors.Is(err, empcap.ErrLarkUserNotFound) {
		t.Fatalf("飞书查不到用户应返回 ErrLarkUserNotFound, got %v", err)
	}
	if len(contact.called) != 1 || contact.called[0] != "ou_missing" {
		t.Fatalf("应按 open_id 查询一次, got %v", contact.called)
	}
}

// TestTenantNamesBatchesOnceAndDedups 验证租户名按去重后的集合一次批量取回。
func TestTenantNamesBatchesOnce(t *testing.T) {
	dir := &fakeTenantDir{names: []string{"租户甲", "租户乙"}}
	ctrl := NewV1(nil, nil, &fakeTenantService{dir: dir}, nil, nil)

	names := ctrl.tenantNames(context.Background(), []int{7, 7, 9})
	if dir.calls != 1 {
		t.Fatalf("应只发起 1 次批量查询, got %d", dir.calls)
	}
	if len(dir.got) != 2 {
		t.Fatalf("应去重为 2 个租户, got %v", dir.got)
	}
	if names[7] != "租户甲" || names[9] != "租户乙" {
		t.Fatalf("租户名映射不符: %+v", names)
	}
}

// TestTenantNamesDegradesWhenUnavailable 验证租户能力缺失或查询失败时名称留空，
// 不把错误抛给列表调用方。
func TestTenantNamesDegradesWhenUnavailable(t *testing.T) {
	// 能力为 nil：直接返回空映射。
	nilCtrl := NewV1(nil, nil, nil, nil, nil)
	if got := nilCtrl.tenantNames(context.Background(), []int{1}); len(got) != 0 {
		t.Fatalf("租户能力缺失时应返回空映射, got %v", got)
	}

	// 查询失败：降级为空映射而不是报错。
	failing := NewV1(nil, nil, &fakeTenantService{dir: &fakeTenantDir{err: errors.New("boom")}}, nil, nil)
	if got := failing.tenantNames(context.Background(), []int{1}); len(got) != 0 {
		t.Fatalf("查询失败时应降级为空映射, got %v", got)
	}
}

// TestListSysUserOptionsWithoutCapability 验证宿主用户能力不可用时返回空候选，
// 而不是放开成无过滤查询。
func TestListSysUserOptionsWithoutCapability(t *testing.T) {
	ctrl := NewV1(nil, nil, nil, nil, nil)
	res, err := ctrl.ListSysUserOptions(context.Background(), &v1.ListSysUserOptionsReq{Keyword: "a"})
	if err != nil {
		t.Fatalf("ListSysUserOptions: %v", err)
	}
	if res == nil || len(res.List) != 0 || res.Total != 0 {
		t.Fatalf("用户能力缺失时应返回空候选, got %+v", res)
	}
}

// TestListLarkAppsHidesSecrets 验证应用下拉只给 app_id 与显示名，绝不带密钥。
func TestListLarkAppsHidesSecrets(t *testing.T) {
	ctrl := NewV1(nil, &fakeHostConfig{apps: larkAppsConfig("cli_a", "top-secret", "霍尔果斯飞书")}, nil, nil, nil)
	res, err := ctrl.ListLarkApps(context.Background(), &v1.ListLarkAppsReq{})
	if err != nil {
		t.Fatalf("ListLarkApps: %v", err)
	}
	if len(res.List) != 1 {
		t.Fatalf("应返回 1 个应用, got %+v", res.List)
	}
	item := res.List[0]
	if item.LarkAppId != "cli_a" || item.Name != "霍尔果斯飞书" {
		t.Fatalf("应用下拉字段不符: %+v", item)
	}
}
