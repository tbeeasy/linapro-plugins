// Package hcm provides the Moka HCM OpenAPI client and authentication layer.
// It exposes HCMAuth (Basic Auth + MD5withRSA query signing) for HCM endpoints
// such as getReportData. Callers build a Client with NewHCMAuth and call PostJSON.
//
// File layout:
//   - auther.go  — Auther interface, HCMCredential, HCMAuth
//   - sign.go    — Sign, BuildQuery, CanonicalString, ParsePrivateKey, BasicAuthHeader
//   - client.go  — Client, NewClient, PostJSON
//   - report.go  — GetReportData, ReportData, ReportHeader
//
// Additional HCM API domains are added as separate files (e.g. employee.go, org.go),
// each mounting methods on *Client.
package hcm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// DefaultBaseURL is the Moka OpenAPI host.
const DefaultBaseURL = "https://api.mokahr.com"

// Client is a Moka HCM OpenAPI HTTP client. It delegates authentication to the
// injected Auther so the same transport works for any HCM auth variant.
// It is safe for concurrent use.
type Client struct {
	baseURL string
	auth    Auther
	http    *http.Client
}

// NewClient builds a Client. baseURL empty uses DefaultBaseURL.
func NewClient(baseURL string, auth Auther) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PostJSON sends a POST request with a JSON body. path is the full API path
// (e.g. "/api-platform/hcm/oapi/v1/report/getReportData"). Authentication is
// applied by the Auther before the request is sent. Returns raw response bytes;
// callers decode the envelope.
func (c *Client) PostJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: build request")
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.auth.ApplyAuth(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: request failed")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, gerror.Wrap(err, "hcm: read response body")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gerror.Newf("hcm: HTTP %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	return data, nil
}

// truncate bounds a string for safe logging.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
