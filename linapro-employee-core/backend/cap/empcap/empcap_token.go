// empcap_token.go 实现员工 JWT：HS256 且使用独立签名密钥
// （绝不用宿主机 jwt.secret）、2 小时 TTL、无刷新 token —— 飞书
// 静默重登即刷新（见 ADR-0002）。密钥通过拥有方绑定的 SecretProvider
// 解析，是平台级的（跨租户共用一个签名密钥）；token 携带 tenantId 声明。

package empcap

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL 是员工 JWT 的有效期。它界定了被盗 token 的窗口和
// 静默重登的节奏；它不约束离职，离职由 EmpAuth 针对状态逐请求强制。
const TokenTTL = 2 * time.Hour

// empClaims 是员工 JWT 的载荷，有意独立于宿主机
// auth.Claims，使两种 token 类型永不互通。
type empClaims struct {
	EmpID    int64 `json:"empId"`
	TenantID int   `json:"tenantId"`
	jwt.RegisteredClaims
}

// IssueToken 使用绑定的密钥为给定身份签发 2 小时员工 JWT，
// 密钥不可用时按失败关闭处理。
func IssueToken(ctx context.Context, c EmpContext) (string, error) {
	secret, err := resolveSecret(ctx)
	if err != nil {
		return "", err
	}
	return signEmpToken(secret, c, TokenTTL)
}

// VerifyToken 使用绑定的密钥解析并校验员工 JWT。
func VerifyToken(ctx context.Context, tokenString string) (EmpContext, error) {
	secret, err := resolveSecret(ctx)
	if err != nil {
		return EmpContext{}, err
	}
	return verifyEmpToken(secret, tokenString)
}

func signEmpToken(secret string, c EmpContext, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := empClaims{
		EmpID:    c.EmpID,
		TenantID: c.TenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func verifyEmpToken(secret, tokenString string) (EmpContext, error) {
	claims := &empClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrUnauthorized
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return EmpContext{}, ErrUnauthorized
	}
	return EmpContext{EmpID: claims.EmpID, TenantID: claims.TenantID}, nil
}
