package nodeconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/domain"
)

func TestSameMachineCenterEntryIsMinimalAndUsesFixedAuthenticatedRoute(t *testing.T) {
	calls := 0
	handler := (&Server{CenterURL: "https://127.0.0.1:8443", API: clientFunc(func(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
		calls++
		if method != http.MethodGet || path != "/api/v1/runner/local-entry" || key != "" || input != nil {
			t.Fatalf("unrestricted probe: %s %s %q %+v", method, path, key, input)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("entry discovery has no two-second bound")
		}
		return []byte(`{"console_url":"http://127.0.0.1:8080/","runner_url":"https://127.0.0.1:8443/","owner_token":"private-owner","data_dir":"private-dir","certificate_pem":"private-runtime"}`), nil
	})}).Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/info?center_url=https://attacker.example&path=/api/v1/state", ""))
	var info domain.LocalInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK || info.Mode != "runner" || info.CenterConsoleURL != "http://127.0.0.1:8080" || calls != 1 {
		t.Fatal(w.Code, calls, err, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "owner") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("entry discovery leaked/cached private runtime data", w.Body.String())
	}
}

func TestForeignOrInvalidConfiguredCenterNeverProbesLocalEntry(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	foreign := ""
	for _, candidate := range []string{"192.0.2.101", "198.51.100.101", "203.0.113.101"} {
		if !hasAddress(net.ParseIP(candidate), addresses) {
			foreign = candidate
			break
		}
	}
	if foreign == "" {
		t.Fatal("no unassigned documentation address available")
	}
	for _, center := range []string{"https://" + foreign + ":8443", "https://attacker.example:8443", "http://localhost:8080", "https://127.0.0.1:8443/path", "https://user@127.0.0.1:8443", "https://127.0.0.1:8443?redirect=other", ""} {
		t.Run(center, func(t *testing.T) {
			calls := 0
			handler := (&Server{CenterURL: center, API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
				calls++
				return []byte(`{"console_url":"http://127.0.0.1:8080","runner_url":"https://127.0.0.1:8443"}`), nil
			})}).Handler()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/info", ""))
			if w.Code != http.StatusOK || calls != 0 || strings.Contains(w.Body.String(), "center_console_url") || !strings.Contains(w.Body.String(), `"mode":"runner"`) {
				t.Fatal("foreign center advertised local owner console", w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestOldOfflineOrUnusableEntryKeepsOrdinaryRunnerPageAvailable(t *testing.T) {
	for _, test := range []struct {
		name, data string
		err        error
	}{
		{name: "older-center", err: &apiclient.HTTPError{Status: http.StatusNotFound, Problem: domain.Problem{Code: "not_found", Message: "old center"}}},
		{name: "disconnected", err: errors.New("connection refused with private-token")},
		{name: "revoked-node", err: &apiclient.HTTPError{Status: http.StatusUnauthorized}},
		{name: "empty-entry", data: `{}`},
		{name: "invalid-json", data: `{"console_url":`},
		{name: "registration-receipt", data: `{"node_id":"node","token":"private-token"}`},
		{name: "oversized-entry", data: strings.Repeat(" ", (64<<10)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := (&Server{CenterURL: "https://127.0.0.1:8443", API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) {
				return []byte(test.data), test.err
			})}).Handler()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/info", ""))
			if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "center_console_url") || strings.Contains(w.Body.String(), "private-") || !strings.Contains(w.Body.String(), `"mode":"runner"`) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestCenterShortcutRejectsArbitraryRedirectTargetsAndMismatchedRunner(t *testing.T) {
	for _, console := range []string{
		"https://127.0.0.1:8080", "http://attacker.example:8080", "http://localhost:8080",
		"http://192.0.2.1:8080", "http://0.0.0.0:8080", "//127.0.0.1:8080", "javascript:alert(1)",
		"http://user:password@127.0.0.1:8080", "http://127.0.0.1:8080/path", "http://127.0.0.1:8080//attacker.example",
		"http://127.0.0.1:8080?next=attacker", "http://127.0.0.1:8080?", "http://127.0.0.1:8080#fragment",
		"http://127.0.0.1:8080#", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:bad",
	} {
		t.Run(console, func(t *testing.T) {
			testShortcutAbsent(t, "https://127.0.0.1:8443", domain.CenterEntry{ConsoleURL: console, RunnerURL: "https://127.0.0.1:8443"})
		})
	}
	for _, runner := range []string{
		"https://127.0.0.2:8443", "https://127.0.0.1:9443", "http://127.0.0.1:8443", "https://attacker.example:8443",
		"https://127.0.0.1:8443/path", "https://127.0.0.1:8443?next=other", "https://user@127.0.0.1:8443",
	} {
		t.Run(runner, func(t *testing.T) {
			testShortcutAbsent(t, "https://127.0.0.1:8443", domain.CenterEntry{ConsoleURL: "http://127.0.0.1:8080", RunnerURL: runner})
		})
	}
}

func testShortcutAbsent(t *testing.T, center string, entry domain.CenterEntry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{CenterURL: center, API: clientFunc(func(context.Context, string, string, string, any) (json.RawMessage, error) { return data, nil })}).Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/info", ""))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "center_console_url") {
		t.Fatal("unsafe shortcut advertised", w.Code, w.Body.String())
	}
}

func TestLocalEntryProbeCancellationIsBoundedAndDoesNotHideRunnerMode(t *testing.T) {
	handler := (&Server{CenterURL: "https://127.0.0.1:8443", API: clientFunc(func(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("discovery deadline exceeds two seconds")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})}).Handler()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, localRequest("GET", "/api/v1/local/info", "").WithContext(ctx))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "center_console_url") || !strings.Contains(w.Body.String(), `"mode":"runner"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestConnectionOriginsAndMachineInterfaceAddresses(t *testing.T) {
	for _, pair := range [][2]string{{"https://127.0.0.1", "https://127.0.0.1:443/"}, {"http://[::1]:8080", "http://[0:0:0:0:0:0:0:1]:8080"}} {
		a, aOK := connectionURL(pair[0])
		b, bOK := connectionURL(pair[1])
		if !aOK || !bOK || !sameConnection(a, b) {
			t.Fatal("equivalent literal origins rejected", pair)
		}
	}
	addresses := []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.42"), Mask: net.CIDRMask(24, 32)}, &net.IPAddr{IP: net.ParseIP("fd00::42")}}
	if !hasAddress(net.ParseIP("192.168.1.42"), addresses) || !hasAddress(net.ParseIP("fd00::42"), addresses) || hasAddress(net.ParseIP("192.168.1.43"), addresses) {
		t.Fatal("interface check used network membership instead of exact machine address")
	}
	for _, raw := range []string{"http://127.0.0.1:8080", "http://[::1]:8080"} {
		center, valid := connectionURL(raw)
		if !valid || !centerOnThisMachine(center) {
			t.Fatal("literal loopback not recognized as same computer", raw)
		}
	}
}
