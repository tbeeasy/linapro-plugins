// Package login 编排员工飞书登录：按 lark_app_id 解析每应用凭据 →
// 用 code 换身份 → 通过 lark_identity 映射解析员工 → 签发绑定该员工
// 租户的员工 JWT。身份应用按 (lark_app_id, open_id) 解析；免登应用（配置了
// identity_app_id）按 (identity_app_id, user_id) 解析。它绝不自动开通：未注册的
// 身份会被拒绝，已离职员工同样被拒绝。
// 飞书 provider 和凭据加载器都被注入，使编排逻辑可用 fake 单元测试，
// 真实的 SDK/网络只保留在生产装配中。
package login

import (
	"context"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/lark"
)

// Result 是一次成功登录的结果。除 JWT 外还携带最小身份快照
// （open_id、姓名、工号），均取自本次换票与签发 token 时已读取的数据，
// 调用方无需为展示而再次查询。
type Result struct {
	Token string
	EmpID int64
	// TenantID 是 token 绑定的租户，便于调用方直接使用而不再查库。
	TenantID int
	// OpenID 是本次登录 (larkAppID, code) 换票解析出的飞书 open_id；
	// 它随本次登录的飞书企业而定，不是员工行的固有属性。
	OpenID string
	Name   string
	// EmployeeNo 是 Moka 工号；非 Moka 来源的员工为空。
	EmployeeNo string
}

// Service 使用核心员工服务、飞书 provider 和每应用凭据加载器编排登录。
type Service struct {
	employees      empcap.Service
	provider       lark.Provider
	loadCredential lark.CredentialLoader
	// issueToken 可注入以便测试；默认使用 empcap.IssueToken。
	issueToken func(ctx context.Context, c empcap.EmpContext) (string, error)
}

// New 构建生产登录服务。
func New(employees empcap.Service, provider lark.Provider, loadCredential lark.CredentialLoader) *Service {
	return &Service{
		employees:      employees,
		provider:       provider,
		loadCredential: loadCredential,
		issueToken:     empcap.IssueToken,
	}
}

// Login 针对一个 lark_app_id 和 code 执行完整换票。租户从解析出的
// 员工派生，而不是由外部传入。
func (s *Service) Login(ctx context.Context, larkAppID, code string) (*Result, error) {
	cred, err := s.loadCredential(ctx, larkAppID)
	if err != nil {
		return nil, err
	}

	identity, err := s.provider.ExchangeCode(ctx, cred, code)
	if err != nil {
		return nil, err
	}

	empID, err := s.resolveEmpID(ctx, cred, identity)
	if err != nil {
		return nil, err
	}
	emp, err := s.employees.GetByID(ctx, empID)
	if err != nil {
		return nil, err
	}
	// 离职员工不签发 token，也不回传身份快照；与 EmpAuth 的离职拦截保持同一口径。
	if !emp.Active() {
		return nil, empcap.ErrUnauthorized
	}

	token, err := s.issueToken(ctx, empcap.EmpContext{EmpID: empID, TenantID: emp.TenantID})
	if err != nil {
		return nil, err
	}
	return &Result{
		Token:      token,
		EmpID:      empID,
		TenantID:   emp.TenantID,
		OpenID:     identity.OpenID,
		Name:       emp.Name,
		EmployeeNo: emp.EmployeeNo,
	}, nil
}

// resolveEmpID 按应用类型把换票身份解析为 EmpID：
//   - 身份应用（cred.IdentityAppID 为空）：按 (本应用, open_id) 查 lark_identity，open_id 空即拒；
//   - 免登应用：lark_identity 只按身份应用写入，免登应用的 open_id 查不到，改按
//     (identity_app_id, user_id) 查——user_id 在同一企业内跨应用一致；user_id 空即拒
//     （免登应用未开通「获取用户 user ID」权限时就会如此）。
func (s *Service) resolveEmpID(ctx context.Context, cred lark.Credential, identity lark.LoginIdentity) (int64, error) {
	if cred.IdentityAppID == "" {
		if identity.OpenID == "" {
			return 0, empcap.ErrUnauthorized
		}
		return s.employees.ResolveEmpIDByLarkIdentity(ctx, cred.AppID, identity.OpenID)
	}
	if identity.UserID == "" {
		return 0, empcap.ErrUnauthorized
	}
	return s.employees.ResolveEmpIDByLarkUserID(ctx, cred.IdentityAppID, identity.UserID)
}
