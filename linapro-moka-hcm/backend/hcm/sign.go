package hcm

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// CanonicalString builds the data-to-sign from query parameters. It excludes
// any existing "sign" key, sorts the remaining keys in ascending order, and
// joins them as k=v&k=v using the RAW, un-encoded values.
func CanonicalString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

// ParsePrivateKey decodes a PEM-encoded RSA private key. It accepts both PKCS#1
// ("RSA PRIVATE KEY") and PKCS#8 ("PRIVATE KEY") encodings.
func ParsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemData)))
	if block == nil {
		return nil, gerror.New("hcm: no PEM block found in RSA private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: parse RSA private key (tried PKCS#1 and PKCS#8)")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, gerror.Newf("hcm: PKCS#8 key is not RSA (%T)", parsed)
	}
	return key, nil
}

// Sign produces the value for the "sign" query parameter: MD5withRSA over the
// canonical string, base64-encoded. The returned value is the RAW base64
// string; callers place it into url.Values so URL-encoding happens exactly once.
func Sign(params map[string]string, priv *rsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", gerror.New("hcm: nil RSA private key")
	}
	digest := md5.Sum([]byte(CanonicalString(params)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.MD5, digest[:])
	if err != nil {
		return "", gerror.Wrap(err, "hcm: RSA sign failed")
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks a base64 signature against the canonical string using a public
// key. It exists for unit tests and operator diagnostics.
func Verify(params map[string]string, signB64 string, pub *rsa.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(signB64)
	if err != nil {
		return gerror.Wrap(err, "hcm: decode signature")
	}
	digest := md5.Sum([]byte(CanonicalString(params)))
	if err := rsa.VerifyPKCS1v15(pub, crypto.MD5, digest[:], sig); err != nil {
		return gerror.Wrap(err, "hcm: signature verify failed")
	}
	return nil
}

// BasicAuthHeader returns the Authorization header value for Moka Basic auth:
// "Basic " + base64(apiKey + ":"). The API key is the username; password is
// empty, which matches `curl -u 'apiKey:'`.
func BasicAuthHeader(apiKey string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey+":"))
}
