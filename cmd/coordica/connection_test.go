package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func connectionCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tls")
	if err := certificate([]string{"--out", dir, "--hosts", "localhost,127.0.0.1,192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	cert, key := filepath.Join(dir, "center.crt"), filepath.Join(dir, "center.key")
	public, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	private, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	// A user-provided certificate file may also contain a key. Loading it is
	// valid, but the copyable setup material must exclude every private block.
	if err := os.WriteFile(cert, append(public, private...), 0600); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	return pair, string(private)
}

func TestConnectionInfoExportsOnlyPublicCertificate(t *testing.T) {
	pair, private := connectionCertificate(t)
	info, err := centerConnectionInfo("192.0.2.1:8443", "127.0.0.1:8080", "", "/private/runtime", pair)
	if err != nil {
		t.Fatal(err)
	}
	if info.RunnerURL != "https://192.0.2.1:8443" || info.ConsoleURL != "http://127.0.0.1:8080" || info.RunnerListen != "192.0.2.1:8443" {
		t.Fatal("unexpected runtime addresses", info)
	}
	if info.Platform != runtime.GOOS {
		t.Fatal("center platform must describe the server runtime, not the browser or supplied path")
	}
	expected := sha256.Sum256(pair.Certificate[0])
	if info.CertificateSHA256 != hex.EncodeToString(expected[:]) {
		t.Fatal("certificate fingerprint does not match DER")
	}
	block, rest := pem.Decode([]byte(info.CertificatePEM))
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 || string(block.Bytes) != string(pair.Certificate[0]) {
		t.Fatal("public certificate was not re-encoded from parsed DER")
	}
	raw, err := json.Marshal(info)
	if err != nil || strings.Contains(string(raw), private) || strings.Contains(info.CertificatePEM, "PRIVATE KEY") {
		t.Fatal("private key entered runtime configuration")
	}
}

func TestConnectionInfoWildcardRequiresExplicitRunnerURL(t *testing.T) {
	pair, _ := connectionCertificate(t)
	for _, address := range []string{"0.0.0.0:8443", "[::]:8443"} {
		info, err := centerConnectionInfo(address, "127.0.0.1:8080", "", "/runtime", pair)
		if err != nil || info.RunnerURL != "" || info.RunnerListen != address {
			t.Fatalf("wildcard became a remote setup URL: %s %v", info.RunnerURL, err)
		}
		info, err = centerConnectionInfo(address, "127.0.0.1:8080", "https://192.0.2.1:8443/", "/runtime", pair)
		if err != nil || info.RunnerURL != "https://192.0.2.1:8443" {
			t.Fatalf("explicit TLS endpoint rejected: %s %v", info.RunnerURL, err)
		}
	}
}

func TestConnectionInfoRejectsUnsafeAdvertisedRunnerURL(t *testing.T) {
	pair, _ := connectionCertificate(t)
	for _, candidate := range []string{"http://192.0.2.1:8443", "https://owner:secret@192.0.2.1:8443", "https://192.0.2.1:8443/setup", "https://192.0.2.1:8443/?token=secret", "https://192.0.2.1:8443/#owner", "https://0.0.0.0:8443", "https://[::]:8443", "https://127.0.0.1:8443", "https://localhost:8443", "https://192.0.2.2:8443", "https://192.0.2.1:0", "https://192.0.2.1:65536", "https://192.0.2.1:"} {
		if _, err := centerConnectionInfo("0.0.0.0:8443", "127.0.0.1:8080", candidate, "/runtime", pair); err == nil {
			t.Error("unsafe endpoint accepted", candidate)
		}
	}
	if _, err := centerConnectionInfo("127.0.0.1:8080", "127.0.0.1:8080", "https://192.0.2.1:8443", "/runtime", tls.Certificate{}); err == nil {
		t.Fatal("TLS advertised without a certificate")
	}
	info, err := centerConnectionInfo("127.0.0.1:8080", "127.0.0.1:8080", "", "/runtime", tls.Certificate{})
	if err != nil || info.RunnerURL != "http://127.0.0.1:8080" || info.CertificatePEM != "" {
		t.Fatal("loopback default should remain usable without TLS", err)
	}
}

func TestConnectionInfoDoesNotAdvertiseCertificateMismatch(t *testing.T) {
	pair, _ := connectionCertificate(t)
	info, err := centerConnectionInfo("192.0.2.2:8443", "127.0.0.1:8080", "", "/runtime", pair)
	if err != nil || info.RunnerURL != "" {
		t.Fatal("certificate mismatch became a copyable endpoint", err)
	}
	info, err = centerConnectionInfo("192.0.2.2:8443", "127.0.0.1:8080", "https://192.0.2.1:8443", "/runtime", pair)
	if err != nil || info.RunnerURL != "https://192.0.2.1:8443" {
		t.Fatal("valid explicit endpoint must be independent of bind address", err)
	}
}

func TestLocalRunnerConsoleLinkValidation(t *testing.T) {
	for input, want := range map[string]string{"off": "", "http://localhost:9081/": "http://127.0.0.1:9081", "http://[::1]:8081": "http://[::1]:8081"} {
		got, err := localRunnerConsoleURL(input)
		if err != nil || got != want {
			t.Fatalf("%s: got %q, want %q: %v", input, got, want, err)
		}
	}
	for _, input := range []string{"http://192.0.2.1:8081", "https://127.0.0.1:8081", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:8081/?token=secret", "http://owner:secret@127.0.0.1:8081", "http://127.0.0.1:8081/task"} {
		if _, err := localRunnerConsoleURL(input); err == nil {
			t.Error("unsafe viewer link accepted", input)
		}
	}
}
