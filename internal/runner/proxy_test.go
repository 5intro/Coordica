package runner

import "testing"

func TestRunnerLocalProxyRejectsSecretsAndNonOrigins(t *testing.T) {
	for _, raw := range []string{"", "http://127.0.0.1:7897", "https://proxy.example/"} {
		if e := ValidateHTTPSProxy(raw); e != nil {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{"http://user:secret@proxy", "socks5://proxy", "http://proxy/path", "http://proxy?secret=x", "http://proxy#x", "http://proxy\n"} {
		if ValidateHTTPSProxy(raw) == nil {
			t.Fatal("invalid proxy accepted", raw)
		}
	}
}
