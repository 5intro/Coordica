// Package apiclient is the transport shared by human CLI and restricted MCP.
package apiclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"coordica/internal/domain"
)

const maxResponseBytes = 16 << 20

// HTTPError preserves the center's status for transports that need to
// distinguish rejected requests from network failures. HTML error pages and
// undeclared fields are never included in the error.
type HTTPError struct {
	Status  int
	Problem domain.Problem
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Problem.Message)
}

type Client struct {
	base, token string
	http        *http.Client
}

func New(center, token, ca string) (*Client, error) {
	u, e := url.Parse(center)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("invalid center URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || ip != nil && ip.IsLoopback())) {
		return nil, errors.New("LAN connections require HTTPS")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca != "" {
		b, e := os.ReadFile(ca)
		if e != nil {
			return nil, e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid CA certificate")
		}
		cfg.RootCAs = pool
	}
	h := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	return &Client{strings.TrimRight(center, "/"), strings.TrimSpace(token), h}, nil
}
func (c *Client) Do(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/api/v1/") || strings.Contains(path, "..") || strings.ContainsAny(path, "?#") {
		return nil, errors.New("invalid API path")
	}
	return c.request(ctx, method, path, key, input, true)
}

// PublicHealth queries only the fixed public endpoint and never sends credentials.
func (c *Client) PublicHealth(ctx context.Context) (domain.CenterHealth, error) {
	var health domain.CenterHealth
	raw, err := c.request(ctx, "GET", "/healthz", "", nil, false)
	if err != nil {
		return health, err
	}
	if err = json.Unmarshal(raw, &health); err != nil || health.Status != "ok" || health.APIVersion != domain.APIVersion {
		return health, errors.New("invalid public center health")
	}
	return health, nil
}

func (c *Client) request(ctx context.Context, method, path, key string, input any, authenticated bool) (json.RawMessage, error) {
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if e != nil {
		return nil, e
	}
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, e := c.http.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if e != nil {
		return nil, e
	}
	if res.StatusCode/100 != 2 {
		problem := domain.Problem{Code: "upstream_error", Message: "center rejected the request"}
		if len(b) <= 64<<10 {
			var parsed domain.Problem
			if json.Unmarshal(b, &parsed) == nil && parsed.Code != "" && parsed.Message != "" {
				problem = parsed
			}
		}
		return nil, &HTTPError{Status: res.StatusCode, Problem: problem}
	}
	if len(b) > maxResponseBytes {
		return nil, errors.New("center response exceeds 16 MiB")
	}
	if !json.Valid(b) {
		return nil, errors.New("center returned invalid JSON")
	}
	return b, nil
}

// CloseIdleConnections releases transport resources when a short-lived caller
// reloads its protected startup profile. It does not cancel an active request.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }
