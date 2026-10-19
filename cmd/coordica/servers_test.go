package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/apiclient"
)

func TestCenterListenBoundary(t *testing.T) {
	for _, tc := range []struct {
		address, console, cert, key string
		shared, valid               bool
	}{
		{"127.0.0.1:8080", "127.0.0.1:8080", "", "", true, true},
		{"localhost:8080", "127.0.0.1:8080", "", "", true, true},
		{"192.0.2.1:8443", "127.0.0.1:8080", "cert", "key", false, true},
		{"0.0.0.0:8443", "127.0.0.1:8080", "cert", "key", false, true},
		{"0.0.0.0:8443", "127.0.0.1:8080", "", "", false, false},
		{"127.0.0.1:8080", "0.0.0.0:8081", "", "", false, false},
		{"127.0.0.1:8080", "192.0.2.1:8081", "", "", false, false},
		{"127.0.0.1:8080", "127.0.0.1:8080", "cert", "key", false, false},
		{"127.0.0.1:8443", "127.0.0.1:8080", "cert", "", false, false},
	} {
		_, _, shared, err := centerAddresses(tc.address, tc.console, tc.cert, tc.key)
		if (err == nil) != tc.valid || err == nil && shared != tc.shared {
			t.Errorf("addresses %+v: shared=%t error=%v", tc, shared, err)
		}
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestCenterSeparateTLSRunnerAndLocalConsole(t *testing.T) {
	root := t.TempDir()
	tlsDir := filepath.Join(root, "tls")
	if err := certificate([]string{"--out", tlsDir}); err != nil {
		t.Fatal(err)
	}
	runnerAddress, consoleAddress := freeAddress(t), freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	data := filepath.Join(root, "center")
	go func() {
		done <- serve(ctx, []string{"--data", data, "--listen", runnerAddress, "--console-listen", consoleAddress, "--tls-cert", filepath.Join(tlsDir, "center.crt"), "--tls-key", filepath.Join(tlsDir, "center.key")})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("center did not stop")
		}
	})
	local := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := local.Get("http://" + consoleAddress + "/api/v1/local/info")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("local console did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(data, "owner.token"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := apiclient.New("http://"+consoleAddress, string(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Do(ctx, "GET", "/api/v1/state", "", nil); err != nil {
		t.Fatal("local owner API unavailable", err)
	}
	remote, err := apiclient.New("https://"+runnerAddress, string(raw), filepath.Join(tlsDir, "center.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Do(ctx, "GET", "/api/v1/state", "", nil); err == nil {
		t.Fatal("owner API exposed on Runner listener")
	}
	if _, err := remote.Do(ctx, "GET", "/api/v1/local/info", "", nil); err == nil {
		t.Fatal("console exposed on Runner listener")
	}
}

func TestConsoleBindFailureDoesNotLeavePartialRunnerListener(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	runnerAddress := freeAddress(t)
	err = serve(context.Background(), []string{"--data", t.TempDir(), "--listen", runnerAddress, "--console-listen", occupied.Addr().String()})
	if err == nil || !strings.Contains(err.Error(), "console listen") {
		t.Fatal("expected console bind rejection", err)
	}
	probe, err := net.Listen("tcp", runnerAddress)
	if err != nil {
		t.Fatal("failed startup leaked Runner listener", err)
	}
	probe.Close()
}

func TestServeRejectsGitCredentialsAliasingCenterAssetsBeforeBootstrap(t *testing.T) {
	for _, flag := range []string{"--github-token-file", "--git-auth-file"} {
		for _, kind := range []string{"missing", "existing", "hardlink", "parent_alias", "tls_key"} {
			t.Run(flag+"/"+kind, func(t *testing.T) {
				data := filepath.Join(t.TempDir(), "center")
				token := filepath.Join(data, "owner.token")
				if kind != "missing" {
					if e := os.MkdirAll(data, 0700); e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(token, []byte("synthetic-owner-secret"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				protected := token
				switch kind {
				case "hardlink":
					token = filepath.Join(filepath.Dir(data), "github-token")
					if e := os.Link(protected, token); e != nil {
						t.Skip("hardlinks unavailable", e)
					}
				case "parent_alias":
					alias := filepath.Join(filepath.Dir(data), "alias")
					if e := os.Symlink(data, alias); e != nil {
						t.Skip("symlinks unavailable", e)
					}
					if e := os.Remove(protected); e != nil {
						t.Fatal(e)
					}
					token = filepath.Join(alias, "owner.token")
				case "tls_key":
					token = filepath.Join(filepath.Dir(data), "tls-secret")
				}
				args := []string{"--data", data, "--listen", "127.0.0.1:0", "--console-listen", "127.0.0.1:0", flag, token}
				if flag == "--github-token-file" {
					args = append(args, "--github-repository", "example/repo")
				} else {
					args = append(args, "--git-auth-remote", "https://github.com/example/repo.git")
				}
				if kind == "tls_key" {
					args = append(args, "--tls-cert", token, "--tls-key", token, "--listen", "127.0.0.1:8443", "--console-listen", "127.0.0.1:8080")
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				e := serve(ctx, args)
				if e == nil || !strings.Contains(e.Error(), "aliases a protected center asset") {
					t.Fatal("center credential not rejected before reading", e)
				}
				if _, e := os.Stat(filepath.Join(data, "coordica.db")); !os.IsNotExist(e) {
					t.Fatal("unsafe startup created database", e)
				}
				if kind == "missing" || kind == "parent_alias" {
					if _, e := os.Stat(protected); !os.IsNotExist(e) {
						t.Fatal("unsafe startup minted Owner", e)
					}
				} else {
					raw, e := os.ReadFile(protected)
					if e != nil || string(raw) != "synthetic-owner-secret" {
						t.Fatal("unsafe startup modified credential", e)
					}
				}
			})
		}
	}
}
