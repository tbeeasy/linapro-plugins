package hcm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
)

func TestCanonicalString_SortsAndExcludesSign(t *testing.T) {
	params := map[string]string{
		"timestamp": "1565244098737",
		"entCode":   "1",
		"apiCode":   "0001",
		"nonce":     "999",
		"sign":      "should-be-excluded",
	}
	got := CanonicalString(params)
	want := "apiCode=0001&entCode=1&nonce=999&timestamp=1565244098737"
	if got != want {
		t.Fatalf("canonical string mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestCanonicalString_RawValuesNotEncoded(t *testing.T) {
	params := map[string]string{
		"apiCode":   "0001",
		"entCode":   "1",
		"nonce":     "999",
		"timestamp": "1565244098737",
		"userName":  "xiao@qq.com",
	}
	got := CanonicalString(params)
	if !strings.Contains(got, "userName=xiao@qq.com") {
		t.Fatalf("expected raw un-encoded userName in canonical string, got %q", got)
	}
	if strings.Contains(got, "%40") {
		t.Fatalf("canonical string must not be URL-encoded, got %q", got)
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{
		"apiCode":   "0001",
		"entCode":   "1",
		"nonce":     "abc123",
		"timestamp": "1565244098737",
	}
	sig, err := Sign(params, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if sig == "" {
		t.Fatal("empty signature")
	}
	if err := Verify(params, sig, &priv.PublicKey); err != nil {
		t.Fatalf("verify should pass for untampered params: %v", err)
	}
	params["nonce"] = "tampered"
	if err := Verify(params, sig, &priv.PublicKey); err == nil {
		t.Fatal("verify should fail after tampering")
	}
}

func TestBuildQuery_EncodesSignOnce(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{
		"apiCode":   "0001",
		"entCode":   "1",
		"nonce":     "abc123",
		"timestamp": "1565244098737",
	}
	q, err := BuildQuery(params, priv)
	if err != nil {
		t.Fatalf("build query: %v", err)
	}
	values, err := url.ParseQuery(q)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	sign := values.Get("sign")
	if sign == "" {
		t.Fatal("sign missing from built query")
	}
	if err := Verify(params, sign, &priv.PublicKey); err != nil {
		t.Fatalf("decoded sign must verify: %v", err)
	}
}

func TestBasicAuthHeader_TrailingColon(t *testing.T) {
	got := BasicAuthHeader("mykey")
	const want = "Basic bXlrZXk6"
	if got != want {
		t.Fatalf("basic auth header mismatch: got=%q want=%q", got, want)
	}
}

func TestParsePrivateKey_PKCS8AndPKCS1(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pemEncodePKCS1(t, priv)
	if _, err := ParsePrivateKey(pkcs1); err != nil {
		t.Fatalf("PKCS#1 parse failed: %v", err)
	}
	pkcs8 := pemEncodePKCS8(t, priv)
	if _, err := ParsePrivateKey(pkcs8); err != nil {
		t.Fatalf("PKCS#8 parse failed: %v", err)
	}
	if _, err := ParsePrivateKey("not a pem"); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}

func pemEncodePKCS1(t *testing.T, priv *rsa.PrivateKey) string {
	t.Helper()
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	return string(pem.EncodeToMemory(block))
}

func pemEncodePKCS8(t *testing.T, priv *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}
