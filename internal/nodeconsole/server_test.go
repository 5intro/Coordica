package nodeconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/domain"
)

type clientFunc func(context.Context, string, string, string, any) (json.RawMessage, error)

func (f clientFunc) Do(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
	return f(ctx, method, path, key, input)
}

func localRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:8081"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:52000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Coordica-Local", "1")
	r.Header.Set("Idempotency-Key", "browser-key")
	return r
}

func TestRestrictedProxyKeepsCredentialsOutsideBrowser(t *testing.T) {
	var keys []string
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-node-token" {
			t.Errorf("center authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/runner/state":
			_, _ = w.Write([]byte(`{"api_version":"v1","node":{"id":"own-node","name":"this computer","token_hash":"private-hash"},"project_name":"demo","tasks":[{"id":"own-task"}],"runs":[{"id":"own-run","node_id":"own-node"}],"requirements":[],"owner_token":"private-owner-token","token":"private-node-token"}`))
		case "POST /api/v1/runner/requirements":
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			var input domain.RequirementProposal
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Body != "body" {
				t.Errorf("requirement input %+v, %v", input, err)
			}
			if input.Title == "changed" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"idempotency_conflict","message":"private-node-token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"requirement-one","version":1,"title":"request","body":"body","token":"private-node-token"}`))
		default:
			t.Errorf("unrestricted center call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer center.Close()
	client, err := apiclient.New(center.URL, "private-node-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{API: client}).Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/state", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("state = %d: %s", w.Code, w.Body)
	}
	var snapshot domain.NodeSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.Node.ID != "own-node" || len(snapshot.Runs) != 1 {
		t.Fatalf("state not preserved: %+v, %v", snapshot, err)
	}
	if strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("credential leaked: %s", w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unsafe response headers: %v", w.Header())
	}
	for i := 0; i < 2; i++ {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, localRequest("POST", "/api/v1/local/requirements", `{"title":"request","body":"body"}`))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "private-") || !strings.Contains(w.Body.String(), "requirement-one") {
			t.Fatalf("requirement replay = %d: %s", w.Code, w.Body)
		}
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, localRequest("POST", "/api/v1/local/requirements", `{"title":"changed","body":"body"}`))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "idempotency_conflict") || strings.Contains(w.Body.String(), "private-") {
		t.Fatalf("conflicting replay = %d: %s", w.Code, w.Body)
	}
	if len(keys) != 3 || keys[0] != "browser-key" || keys[1] != keys[0] || keys[2] != keys[0] {
		t.Fatalf("browser key was not forwarded unchanged: %v", keys)
	}
}

func TestLocalBoundaryRejectsRemoteAndReboundHosts(t *testing.T) {
	tests := []struct {
		name, peer, host string
	}{
		{"remote-peer", "192.168.0.2:50000", "127.0.0.1:8081"},
		{"invalid-peer", "localhost:50000", "127.0.0.1:8081"},
		{"missing-peer-port", "127.0.0.1", "127.0.0.1:8081"},
		{"rebound-host", "127.0.0.1:50000", "attacker.example:8081"},
		{"LAN-host", "127.0.0.1:50000", "192.168.0.2:8081"},
		{"malformed-host", "127.0.0.1:50000", "::1:8081"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler := (&Server{API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
				called = true
				return []byte(`{}`), nil
			})}).Handler()
			r := localRequest("GET", "/api/v1/local/state", "")
			r.RemoteAddr, r.Host = test.peer, test.host
			r.Header.Set("Forwarded", "for=127.0.0.1;host=localhost")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || called {
				t.Fatalf("boundary bypass: status %d, called %t", w.Code, called)
			}
		})
	}
}

func TestLocalPageAndModeSupportIPv4AndIPv6(t *testing.T) {
	handler := (&Server{UI: fstest.MapFS{"index.html": {Data: []byte("local page")}}}).Handler()
	for _, host := range []string{"127.0.0.1:8081", "localhost:8081", "[::1]:8081"} {
		for _, path := range []string{"/", "/api/v1/local/info"} {
			r := localRequest("GET", path, "")
			r.Host, r.RemoteAddr = host, "[::1]:50000"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s = %d", host, path, w.Code)
			}
			if path == "/api/v1/local/info" && !strings.Contains(w.Body.String(), `"mode":"runner"`) {
				t.Fatalf("missing mode: %s", w.Body)
			}
		}
	}
}

func TestWritesRequireSameOriginOrLocalJSONHeader(t *testing.T) {
	tests := []struct {
		name, origin, header, contentType string
		want                              int
	}{
		{"same-origin", "http://127.0.0.1:8081", "", "application/json", 200},
		{"local-header", "", "1", "application/json; charset=utf-8", 200},
		{"missing-origin-and-header", "", "", "application/json", 403},
		{"wrong-origin", "https://attacker.example", "1", "application/json", 403},
		{"wrong-scheme", "https://127.0.0.1:8081", "1", "application/json", 403},
		{"wrong-port", "http://127.0.0.1:8082", "1", "application/json", 403},
		{"null-origin", "null", "1", "application/json", 403},
		{"origin-with-path", "http://127.0.0.1:8081/path", "1", "application/json", 403},
		{"form-post", "http://127.0.0.1:8081", "1", "text/plain", 415},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			handler := (&Server{API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
				calls++
				return []byte(`{"id":"requirement-one"}`), nil
			})}).Handler()
			r := localRequest("POST", "/api/v1/local/requirements", `{"title":"request","body":"body"}`)
			r.Header.Set("Origin", test.origin)
			r.Header.Set("X-Coordica-Local", test.header)
			r.Header.Set("Content-Type", test.contentType)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want || (calls == 1) != (test.want == 200) {
				t.Fatalf("origin boundary status %d calls %d", w.Code, calls)
			}
		})
	}
}

func TestBrowserCannotCallOwnerOrExecutionAPIs(t *testing.T) {
	calls := 0
	handler := (&Server{API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
		calls++
		return nil, nil
	})}).Handler()
	for _, path := range []string{"/api/v1/state", "/api/v1/local/requirements/admin", "/api/v1/runner/register", "/api/v1/runner/claim", "/api/v1/project", "/api/v1/nodes/own/revoke", "/api/v1/runs/own/complete"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, localRequest("POST", path, `{}`))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s unexpectedly exposed: %d", path, w.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("owner API forwarded %d times", calls)
	}
}

func TestStrictRequirementEnvelopeAndBoundedInput(t *testing.T) {
	calls := 0
	handler := (&Server{API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
		calls++
		return []byte(`{}`), nil
	})}).Handler()
	for _, body := range []string{`null`, `[]`, `{"title":`, `{"title":"request","body":"body","approved":true}`, `{} {}`, strings.Repeat(" ", (1<<20)+1)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, localRequest("POST", "/api/v1/local/requirements", body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid envelope status %d", w.Code)
		}
	}
	for _, key := range []string{"", " ", strings.Repeat("k", 201)} {
		r := localRequest("POST", "/api/v1/local/requirements", `{"title":"request","body":"body"}`)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid key status %d", w.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("malformed input forwarded %d times", calls)
	}
}

func TestUpstreamFailureStatusAndMessagesAreSanitized(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"network", errors.New("dial private-node-token: connection refused"), 502, "center_unavailable"},
		{"revoked", &apiclient.HTTPError{Status: 403, Problem: domain.Problem{Code: "forbidden", Message: "private-node-token"}}, 403, "forbidden"},
		{"expired-credential", &apiclient.HTTPError{Status: 401, Problem: domain.Problem{Code: "unauthorized", Message: "private-node-token"}}, 401, "unauthorized"},
		{"invalid-input", &apiclient.HTTPError{Status: 400, Problem: domain.Problem{Code: "invalid_input", Message: "private-node-token"}}, 400, "invalid_input"},
		{"unknown-rejection", &apiclient.HTTPError{Status: 409, Problem: domain.Problem{Code: "private-node-token", Message: "private-node-token"}}, 409, "request_rejected"},
		{"center-failed", &apiclient.HTTPError{Status: 500, Problem: domain.Problem{Code: "internal_error", Message: "private-node-token"}}, 502, "center_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := (&Server{API: clientFunc(func(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 15*time.Second {
					t.Error("upstream request has no bounded deadline")
				}
				return nil, test.err
			})}).Handler()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/state", ""))
			if w.Code != test.want || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "private-node-token") {
				t.Fatalf("upstream failure = %d: %s", w.Code, w.Body)
			}
		})
	}
}

func TestInvalidOrOversizedCenterResponseRejected(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{}`, `{"node_id":"registration","token":"private-token"}`, `{"node":`, strings.Repeat(" ", (8<<20)+1)} {
		handler := (&Server{API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
			return []byte(data), nil
		})}).Handler()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/state", ""))
		if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "invalid_center_response") {
			t.Errorf("invalid center response = %d: %s", w.Code, w.Body)
		}
	}
}

func TestBrowserCancellationPropagatesToCenterRequest(t *testing.T) {
	handler := (&Server{API: clientFunc(func(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Error("browser cancellation did not reach the center request")
		}
		return nil, ctx.Err()
	})}).Handler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := localRequest("GET", "/api/v1/local/state", "").WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("canceled request status = %d", w.Code)
	}
}
