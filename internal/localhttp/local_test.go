package localhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackAddress(t *testing.T) {
	for _, input := range []string{"127.0.0.1:8080", "[::1]:8081", "127.0.0.2:0", "localhost:8080"} {
		got, err := Address(input)
		if err != nil || got == "" {
			t.Fatalf("local address %q: %q %v", input, got, err)
		}
		if input == "localhost:8080" && got != "127.0.0.1:8080" {
			t.Fatalf("localhost was not normalized: %q", got)
		}
	}
	for _, input := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.1:8080", "local.example:8080", "127.0.0.1", "127.0.0.1:"} {
		if _, err := Address(input); err == nil {
			t.Fatalf("accepted nonlocal or incomplete console address %q", input)
		}
	}
}

func TestLocalGuardRejectsRemotePeerDNSRebindingAndForeignOrigin(t *testing.T) {
	handler := Guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		name, peer, host, origin string
		allowed                  bool
	}{
		{"local IPv4", "127.0.0.1:1234", "127.0.0.1:8080", "", true},
		{"local IPv6", "[::1]:1234", "[::1]:8080", "", true},
		{"localhost same origin", "127.0.0.1:1234", "localhost:8080", "http://localhost:8080", true},
		{"uppercase localhost", "127.0.0.1:1234", "LOCALHOST:8080", "", true},
		{"malformed brackets", "127.0.0.1:1234", "[[::1]]", "", false},
		{"remote peer", "192.0.2.1:1234", "localhost:8080", "", false},
		{"DNS rebound hostname", "127.0.0.1:1234", "attacker.example:8080", "", false},
		{"foreign origin", "127.0.0.1:1234", "127.0.0.1:8080", "https://attacker.example", false},
		{"null origin", "127.0.0.1:1234", "127.0.0.1:8080", "null", false},
		{"invalid peer", "localhost:1234", "localhost:8080", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://localhost:8080/", nil)
			r.RemoteAddr, r.Host = tc.peer, tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("Forwarded", "for=127.0.0.1;host=localhost:8080")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusNoContent
			}
			if w.Code != want {
				t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
			}
		})
	}
}
