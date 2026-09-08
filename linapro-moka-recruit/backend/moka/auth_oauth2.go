package moka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

const oauth2TokenPath = "/api-platform/v1/auth/oauth2/getToken"

// OAuth2Credential 保存 Moka OAuth2 流程的客户端凭证。
type OAuth2Credential struct {
	ClientID     string
	ClientSecret string
}

// oauth2TokenResp 是 getToken 的响应信封。
type oauth2TokenResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"` // 单位：秒
		TokenType   string `json:"tokenType"`
	} `json:"data"`
}

// OAuth2Auth 使用 Moka OAuth2 client_credentials 授权模式实现 Author。
// 它会自动续期访问令牌：当前令牌剩余有效期不足 30 分钟时即获取新令牌，
// 与 Moka 文档中旧、新令牌同时有效的窗口期一致。
type OAuth2Auth struct {
	baseURL string
	cred    OAuth2Credential
	http    *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewOAuth2Auth 构建一个 OAuth2Auth。baseURL 为空时回退到 DefaultBaseURL。
func NewOAuth2Auth(baseURL string, cred OAuth2Credential) *OAuth2Auth {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &OAuth2Auth{
		baseURL: baseURL,
		cred:    cred,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// ApplyAuth 确保存在有效的访问令牌，并在请求上设置
// Authorization: Bearer <token> 请求头。
func (a *OAuth2Auth) ApplyAuth(ctx context.Context, req *http.Request) error {
	token, err := a.getToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// getToken 返回有效的访问令牌，必要时自动刷新。
func (a *OAuth2Auth) getToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && time.Until(a.expiresAt) >= 30*time.Minute {
		return a.token, nil
	}
	return a.fetchToken(ctx)
}

// fetchToken 调用 getToken 并缓存结果。调用时必须持有 mu 锁。
func (a *OAuth2Auth) fetchToken(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]string{
		"clientID":     a.cred.ClientID,
		"clientSecret": a.cred.ClientSecret,
		"grantType":    "client_credentials",
	})
	if err != nil {
		return "", gerror.Wrap(err, "moka-recruit: marshal oauth2 token request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+oauth2TokenPath, bytes.NewReader(body))
	if err != nil {
		return "", gerror.Wrap(err, "moka-recruit: build oauth2 token request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", gerror.Wrap(err, "moka-recruit: oauth2 token request failed")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", gerror.Wrap(err, "moka-recruit: read oauth2 token response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", gerror.Newf("moka-recruit: oauth2 token HTTP %d: %s", resp.StatusCode, truncate(string(raw), 256))
	}

	var env oauth2TokenResp
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", gerror.Wrapf(err, "moka-recruit: decode oauth2 token response: %s", truncate(string(raw), 256))
	}
	if env.Code != 0 {
		return "", gerror.Newf("moka-recruit: oauth2 token error code=%d msg=%q", env.Code, env.Msg)
	}
	if env.Data.AccessToken == "" {
		return "", fmt.Errorf("moka-recruit: oauth2 token response missing accessToken")
	}

	a.token = env.Data.AccessToken
	a.expiresAt = time.Now().Add(time.Duration(env.Data.ExpiresIn) * time.Second)
	return a.token, nil
}
