package login

import (
	"context"
	"errors"
	"testing"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
)

// fakeEmployees 是一个 fake empcap.Service，记录 ResolveEmpIDByLarkIdentity
// 的入参并返回预设员工。
type fakeEmployees struct {
	resolveEmpID int64
	resolveErr   error
	gotAppID     string
	gotOpenID    string
	tenantID     int
	name         string
	employeeNo   string
	departed     bool
}

func (f *fakeEmployees) UpsertEmployee(context.Context, int, empcap.Profile) (int64, error) {
	return 0, nil
}
func (f *fakeEmployees) MarkDeparted(context.Context, int, int64) error { return nil }
func (f *fakeEmployees) GetByID(context.Context, int64) (*empcap.Employee, error) {
	status := empcap.StatusActive
	if f.departed {
		status = empcap.StatusDeparted
	}
	return &empcap.Employee{
		EmpID:      f.resolveEmpID,
		TenantID:   f.tenantID,
		Name:       f.name,
		EmployeeNo: f.employeeNo,
		Status:     status,
	}, nil
}
func (f *fakeEmployees) GetByEmployeeNo(context.Context, int, string) (*empcap.Employee, error) {
	return nil, empcap.ErrEmployeeNotFound
}
func (f *fakeEmployees) GetByEmployeeNoAnyTenant(context.Context, string) (*empcap.Employee, error) {
	return nil, empcap.ErrEmployeeNotFound
}
func (f *fakeEmployees) GetByName(context.Context, int, string) (*empcap.Employee, error) {
	return nil, empcap.ErrEmployeeNotFound
}
func (f *fakeEmployees) ListActiveByTenant(context.Context, int) ([]*empcap.Employee, error) {
	return nil, nil
}
func (f *fakeEmployees) ResolveEmpIDByLarkIdentity(_ context.Context, appID, openID string) (int64, error) {
	f.gotAppID = appID
	f.gotOpenID = openID
	return f.resolveEmpID, f.resolveErr
}
func (f *fakeEmployees) ResolveEmpIDByLarkUserID(context.Context, string, string) (int64, error) {
	return f.resolveEmpID, f.resolveErr
}
func (f *fakeEmployees) UpsertLarkIdentity(context.Context, string, string, string, string, int64) error {
	return nil
}
func (f *fakeEmployees) MapLarkOpenIDsByEmployeeNo(context.Context, string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
func (f *fakeEmployees) MapOwnLarkOpenIDsByEmployeeNo(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeEmployees) MapOwnLarkUserIDsByEmployeeNo(context.Context, string) (empcap.LarkUserIDIndex, error) {
	return empcap.LarkUserIDIndex{ByEmployeeNo: map[string]string{}}, nil
}
func (f *fakeEmployees) MapByEmployeeNos(context.Context, int, []string) (map[string]*empcap.Employee, error) {
	return map[string]*empcap.Employee{}, nil
}
func (f *fakeEmployees) MapByEmployeeNosAnyTenant(context.Context, []string) (map[string]*empcap.Employee, error) {
	return map[string]*empcap.Employee{}, nil
}
func (f *fakeEmployees) MapByNamesAnyTenant(context.Context, []string) (map[string][]*empcap.Employee, error) {
	return map[string][]*empcap.Employee{}, nil
}
func (f *fakeEmployees) MapByEmpIDs(context.Context, []int64) (map[int64]*empcap.Employee, error) {
	return map[int64]*empcap.Employee{}, nil
}

// fakeProvider 是一个 fake lark.Provider。
type fakeProvider struct {
	identity lark.LoginIdentity
	err      error
}

func (f *fakeProvider) ExchangeCode(context.Context, lark.Credential, string) (lark.LoginIdentity, error) {
	return f.identity, f.err
}

func newTestService(emp *fakeEmployees, prov *fakeProvider, cred lark.Credential) *Service {
	s := New(emp, prov, func(context.Context, string) (lark.Credential, error) { return cred, nil })
	s.issueToken = func(context.Context, empcap.EmpContext) (string, error) { return "test-token", nil }
	return s
}

// TestLoginResolvesRegisteredIdentity 验证已注册的 (larkAppId, open_id)
// 能解析为员工并返回绑定该员工租户的 token，且结果携带姓名、工号
// 与租户快照，调用方无需二次查询员工。
func TestLoginResolvesRegisteredIdentity(t *testing.T) {
	emp := &fakeEmployees{resolveEmpID: 10, tenantID: 7, name: "张三", employeeNo: "GZ001198"}
	prov := &fakeProvider{identity: lark.LoginIdentity{OpenID: "ou_known", Name: "Known"}}
	s := newTestService(emp, prov, lark.Credential{AppID: "cli_f", AppSecret: "b"})

	res, err := s.Login(context.Background(), "cli_f", "code")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Token != "test-token" || res.EmpID != 10 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Name != "张三" || res.EmployeeNo != "GZ001198" || res.TenantID != 7 {
		t.Fatalf("expected identity snapshot in result, got %+v", res)
	}
	if res.OpenID != "ou_known" {
		t.Fatalf("expected open_id from exchange in result, got %q", res.OpenID)
	}
	if emp.gotAppID != "cli_f" || emp.gotOpenID != "ou_known" {
		t.Fatalf("expected lark identity lookup with app+open_id, got app=%q open=%q", emp.gotAppID, emp.gotOpenID)
	}
}

// TestLoginRejectsUnregisteredIdentity 验证未注册身份被拒绝（不自动开通）。
func TestLoginRejectsUnregisteredIdentity(t *testing.T) {
	emp := &fakeEmployees{resolveErr: empcap.ErrEmployeeNotFound}
	prov := &fakeProvider{identity: lark.LoginIdentity{OpenID: "ou_unknown"}}
	s := newTestService(emp, prov, lark.Credential{AppID: "cli_f", AppSecret: "b"})

	if _, err := s.Login(context.Background(), "cli_f", "code"); !errors.Is(err, empcap.ErrEmployeeNotFound) {
		t.Fatalf("expected ErrEmployeeNotFound, got %v", err)
	}
}

// TestLoginRejectsDepartedEmployee 验证已登记但已离职的员工登录被拒：
// 返回 ErrUnauthorized，且不签发 token、不回传身份快照。
func TestLoginRejectsDepartedEmployee(t *testing.T) {
	emp := &fakeEmployees{resolveEmpID: 11, tenantID: 7, name: "离职者", employeeNo: "GZ009999", departed: true}
	prov := &fakeProvider{identity: lark.LoginIdentity{OpenID: "ou_departed"}}
	s := newTestService(emp, prov, lark.Credential{AppID: "cli_f", AppSecret: "b"})
	issued := false
	s.issueToken = func(context.Context, empcap.EmpContext) (string, error) {
		issued = true
		return "should-not-issue", nil
	}

	res, err := s.Login(context.Background(), "cli_f", "code")
	if !errors.Is(err, empcap.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for departed employee, got res=%+v err=%v", res, err)
	}
	if res != nil {
		t.Fatalf("expected no identity snapshot for departed employee, got %+v", res)
	}
	if issued {
		t.Fatal("token must not be issued for departed employee")
	}
}

// TestLoginInvalidCode 验证 provider 出错会中止登录。
func TestLoginInvalidCode(t *testing.T) {
	emp := &fakeEmployees{}
	prov := &fakeProvider{err: errors.New("invalid code")}
	s := newTestService(emp, prov, lark.Credential{AppID: "cli_f", AppSecret: "b"})

	if _, err := s.Login(context.Background(), "cli_f", "bad"); err == nil {
		t.Fatal("expected login to fail on provider error")
	}
}

// TestLoginEmptyOpenID 验证空 open_id 被以未授权拒绝。
func TestLoginEmptyOpenID(t *testing.T) {
	emp := &fakeEmployees{}
	prov := &fakeProvider{identity: lark.LoginIdentity{OpenID: ""}}
	s := newTestService(emp, prov, lark.Credential{AppID: "cli_f", AppSecret: "b"})

	if _, err := s.Login(context.Background(), "cli_f", "code"); !errors.Is(err, empcap.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestLoginCredentialMissing 验证凭据加载器出错按失败关闭处理。
func TestLoginCredentialMissing(t *testing.T) {
	emp := &fakeEmployees{}
	prov := &fakeProvider{identity: lark.LoginIdentity{OpenID: "ou_x"}}
	s := New(emp, prov, func(context.Context, string) (lark.Credential, error) {
		return lark.Credential{}, lark.ErrCredentialMissing
	})
	s.issueToken = func(context.Context, empcap.EmpContext) (string, error) { return "t", nil }

	if _, err := s.Login(context.Background(), "cli_unknown", "code"); !errors.Is(err, lark.ErrCredentialMissing) {
		t.Fatalf("expected ErrCredentialMissing, got %v", err)
	}
}
