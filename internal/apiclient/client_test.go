package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPErrorRetainsStatusAndDeclaredProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"idempotency_conflict","message":"request changed","token":"private-token"}`))
	}))
	defer server.Close()
	client, err := New(server.URL, "node-token", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), "POST", "/api/v1/runner/requirements", "key", nil)
	var failure *HTTPError
	if !errors.As(err, &failure) || failure.Status != http.StatusConflict || failure.Problem.Code != "idempotency_conflict" {
		t.Fatalf("HTTP problem not preserved: %v", err)
	}
	if !strings.Contains(err.Error(), "request changed") || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unexpected error formatting: %v", err)
	}
}

func TestHTTPErrorDoesNotIncludeHTMLOrOversizedBody(t *testing.T) {
	for _, body := range []string{"<html>private-token</html>", `{"code":"failure","message":"` + strings.Repeat("s", 64<<10) + `private-token"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(body))
		}))
		client, err := New(server.URL, "node-token", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Do(context.Background(), "GET", "/api/v1/runner/state", "", nil)
		server.Close()
		var failure *HTTPError
		if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("unsafe upstream error: %v", err)
		}
	}
}

func TestResponseLargerThanTransportLimitRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", maxResponseBytes) + `"}`))
	}))
	defer server.Close()
	client, err := New(server.URL, "node-token", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), "GET", "/api/v1/runner/state", "", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds 16 MiB") {
		t.Fatalf("oversized response accepted: %v", err)
	}
}
