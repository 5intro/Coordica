package runner

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

type responseClock struct {
	Server         time.Time
	Sent, Received time.Time
}
type apiError struct {
	Status  int
	Problem domain.Problem
}

func (e *apiError) Error() string {
	return fmt.Sprintf("center returned HTTP %d (%s): %s", e.Status, e.Problem.Code, e.Problem.Message)
}

func newHTTPClient(config Config) (*http.Client, *url.URL, error) {
	u, err := url.Parse(config.CenterURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, nil, errors.New("invalid center URL")
	}
	if u.Scheme != "https" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if u.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, nil, errors.New("HTTPS is required except for loopback development")
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if config.TLSCA != "" {
		pem, err := os.ReadFile(config.TLSCA)
		if err != nil {
			return nil, nil, err
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, nil, errors.New("TLS CA file contains no certificates")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client := &http.Client{Transport: transport, Timeout: time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("center redirects are not accepted") }}
	return client, u, nil
}

func (r *Runner) endpoint(path string) (string, error) {
	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	resolved := r.base.ResolveReference(u)
	if resolved.Scheme != r.base.Scheme || resolved.Host != r.base.Host || resolved.User != nil {
		return "", errors.New("input URL must have the authenticated center origin")
	}
	return resolved.String(), nil
}

func (r *Runner) request(ctx context.Context, method, path, key string, body io.Reader, contentType string, out any) (responseClock, error) {
	var clock responseClock
	address, err := r.endpoint(path)
	if err != nil {
		return clock, err
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return clock, err
	}
	if r.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.config.Token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	clock.Sent = time.Now()
	response, err := r.http.Do(req)
	clock.Received = time.Now()
	if err != nil {
		return clock, err
	}
	defer response.Body.Close()
	clock.Server, _ = http.ParseTime(response.Header.Get("Date"))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem domain.Problem
		json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&problem)
		return clock, &apiError{Status: response.StatusCode, Problem: problem}
	}
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return clock, nil
	}
	if writer, ok := out.(io.Writer); ok {
		written, err := io.Copy(writer, io.LimitReader(response.Body, (64<<20)+1))
		if written > 64<<20 {
			return clock, errors.New("input bundle exceeds 64 MiB")
		}
		return clock, err
	}
	limited := &io.LimitedReader{R: response.Body, N: (8 << 20) + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(out); err != nil {
		return clock, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return clock, errors.New("center response contains trailing or invalid JSON")
	}
	if limited.N == 0 {
		return clock, errors.New("center response exceeds 8 MiB")
	}
	return clock, nil
}
func (r *Runner) json(ctx context.Context, method, path, key string, input, output any) (responseClock, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return responseClock{}, err
		}
		body = bytes.NewReader(raw)
	}
	return r.request(ctx, method, path, key, body, "application/json", output)
}

// HTTP Date is center time, rounded down to a second. Deduct one second plus
// the measured round trip, then keep this deadline on Go's monotonic clock.
// Missing center time is rejected instead of assuming synchronized machines.
func localDeadline(until time.Time, clock responseClock) (time.Time, error) {
	if clock.Server.IsZero() {
		return time.Time{}, errors.New("center response lacks a valid Date header")
	}
	remaining := until.Sub(clock.Server) - time.Second - clock.Received.Sub(clock.Sent)
	if remaining <= 0 {
		return time.Time{}, errors.New("center execution permission already expired")
	}
	return clock.Received.Add(remaining), nil
}

func shortDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	return strings.ReplaceAll(detail, "\x00", "")
}
