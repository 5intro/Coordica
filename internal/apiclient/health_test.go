package apiclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type healthTransportFunc func(*http.Request) (*http.Response, error)

func (f healthTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPublicHealthHasOneFixedPathAndNeverSendsCredentials(t *testing.T) {
	c, e := New("https://fixed.example", "own_private_owner", "")
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	c.http.Transport = healthTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "https://fixed.example/healthz" || r.Header.Get("Authorization") != "" {
			t.Fatal("health broadened or leaked credentials", r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"ok","api_version":"v1","center_instance_id":"fixed","config_digest":"digest"}`)), Header: make(http.Header)}, nil
	})
	health, e := c.PublicHealth(context.Background())
	if e != nil || health.CenterInstanceID != "fixed" || calls != 1 {
		t.Fatal(health, e, calls)
	}
	if _, e = c.Do(context.Background(), "GET", "/healthz", "", nil); e == nil || calls != 1 {
		t.Fatal("generic API path validation weakened")
	}
}
