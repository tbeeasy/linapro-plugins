package hcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// HCMCredential holds one tenant's Moka HCM OpenAPI authentication material.
// APIKey (Basic auth username) and APICode (query parameter) are intentionally
// separate values even though a deployment may set them equal.
type HCMCredential struct {
	APIKey     string          // Basic auth username; password is empty
	APICode    string          // query param apiCode (接口编码)
	EntCode    string          // query param entCode (租户唯一 ID)
	PrivateKey *rsa.PrivateKey // RSA key for MD5withRSA signing
}

// Auther applies authentication to an outgoing Moka HCM API request.
type Auther interface {
	ApplyAuth(ctx context.Context, req *http.Request) error
}

// HCMAuth implements Auther for Moka HCM OpenAPI: stacks Basic auth header +
// RSA-signed query parameters (apiCode/entCode/nonce/timestamp/sign) on every
// request. This is the authentication mode for non-privacy HCM endpoints such
// as getReportData.
type HCMAuth struct {
	cred HCMCredential
}

// NewHCMAuth builds an HCMAuth from the given credential.
func NewHCMAuth(cred HCMCredential) *HCMAuth {
	return &HCMAuth{cred: cred}
}

// ApplyAuth sets Authorization: Basic … and appends the signed query
// parameters to the request URL. The request URL must already have a path set;
// any existing query string is preserved and the signing params are merged in.
func (a *HCMAuth) ApplyAuth(_ context.Context, req *http.Request) error {
	n, err := nonce()
	if err != nil {
		return err
	}
	params := map[string]string{
		"entCode":   a.cred.EntCode,
		"apiCode":   a.cred.APICode,
		"nonce":     n,
		"timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10),
	}
	query, err := BuildQuery(params, a.cred.PrivateKey)
	if err != nil {
		return err
	}
	// Merge signed params into existing query string (if any).
	existing := req.URL.RawQuery
	if existing == "" {
		req.URL.RawQuery = query
	} else {
		req.URL.RawQuery = existing + "&" + query
	}
	req.Header.Set("Authorization", BasicAuthHeader(a.cred.APIKey))
	return nil
}

// nonce generates a random alphanumeric string of length 8 (the Moka maximum:
// ≤8 chars, digits and case-insensitive letters).
func nonce() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", gerror.Wrap(err, "hcm: generate nonce")
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

// BuildQuery assembles the signed query string ready to append to a URL. It
// signs params, adds the sign key, and encodes once via url.Values.Encode().
func BuildQuery(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	sign, err := Sign(params, priv)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("sign", sign)
	return values.Encode(), nil
}
