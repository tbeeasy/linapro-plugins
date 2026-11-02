package empcap

import (
	"context"
	"testing"
	"time"
)

// TestEmpTokenRoundTrip 验证 sign→verify 返回相同身份。
func TestEmpTokenRoundTrip(t *testing.T) {
	const secret = "employee-secret"
	want := EmpContext{EmpID: 5, TenantID: 1}

	token, err := signEmpToken(secret, want, TokenTTL)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := verifyEmpToken(secret, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

// TestEmpTokenKeyIsolation 验证用员工密钥签发的 token
// 在另一（管理员）密钥下被拒绝，反之亦然。
func TestEmpTokenKeyIsolation(t *testing.T) {
	token, err := signEmpToken("employee-secret", EmpContext{EmpID: 1, TenantID: 1}, TokenTTL)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := verifyEmpToken("host-admin-secret", token); err == nil {
		t.Fatal("expected verification to fail under a different key")
	}
}

// TestEmpTokenExpired 验证过期 token 被拒绝。
func TestEmpTokenExpired(t *testing.T) {
	token, err := signEmpToken("s", EmpContext{EmpID: 1, TenantID: 1}, -1*time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := verifyEmpToken("s", token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

// TestIssueTokenSecretMissingFailClosed 验证未绑定密钥提供方时
// IssueToken/VerifyToken 按失败关闭处理，绝不使用空密钥。
func TestIssueTokenSecretMissingFailClosed(t *testing.T) {
	defer BindTokenSecretProvider(boundSecretFn) // restore process-global provider

	BindTokenSecretProvider(nil)
	if _, err := IssueToken(context.Background(), EmpContext{EmpID: 1, TenantID: 1}); err == nil {
		t.Fatal("expected IssueToken to fail closed with no secret provider")
	}

	// 绑定返回空密钥的提供方也必须按失败关闭处理。
	BindTokenSecretProvider(func(context.Context) (string, error) { return "", nil })
	if _, err := IssueToken(context.Background(), EmpContext{EmpID: 1, TenantID: 1}); err == nil {
		t.Fatal("expected IssueToken to fail closed with empty secret")
	}
}

// TestIssueVerifyViaBoundSecret 验证公开的 Issue/Verify 路径使用
// 绑定的密钥提供方。
func TestIssueVerifyViaBoundSecret(t *testing.T) {
	defer BindTokenSecretProvider(boundSecretFn) // restore process-global provider

	BindTokenSecretProvider(func(context.Context) (string, error) { return "bound-secret", nil })
	ctx := context.Background()
	want := EmpContext{EmpID: 9002, TenantID: 2}
	token, err := IssueToken(ctx, want)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := VerifyToken(ctx, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != want {
		t.Fatalf("mismatch: got %+v want %+v", got, want)
	}
}
