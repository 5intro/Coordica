package runner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
)

func TestRunnerCustomRegistryDiscoveryAndPersistence(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	custom := agent.Definition{ID: "local-custom", Executables: []string{executable}, VersionArgs: []string{"-test.run=^$"}, Protocol: agent.ProtocolCodex, Cancel: true, Review: true}
	overrides := map[string]string{}
	for _, def := range agent.BuiltinDefinitions() {
		overrides[def.ID] = filepath.Join(t.TempDir(), "not-installed")
	}
	r, err := New(Config{CenterURL: server.URL, DataDir: t.TempDir(), Tools: []agent.Definition{custom}, Executables: overrides})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, capability := range r.capabilities(context.Background()) {
		if capability.Tool == custom.ID {
			found = capability.Available && capability.Structured && capability.Cancel && capability.Review && capability.Protocol == agent.ProtocolCodex
		}
	}
	if !found {
		t.Fatal("custom definition was not discovered")
	}
	if err := writePrivateJSON(filepath.Join(r.config.DataDir, "runner.json"), r.config); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(r.config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := New(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if definition, ok := restored.registry.Resolve(custom.ID); !ok || definition.Protocol != custom.Protocol || !definition.Review {
		t.Fatal("custom driver was lost during restart")
	}
	if _, err := New(Config{CenterURL: server.URL, DataDir: t.TempDir(), Executables: map[string]string{"unregistered": executable}}); err == nil {
		t.Fatal("unregistered executable override accepted")
	}
	if _, err := LocalRegistry([]agent.Definition{agent.BuiltinDefinitions()[0]}); err == nil {
		t.Fatal("local definition replaced a built-in identity")
	}
}

// This child only runs through an exact -test.run filter; unavailable metadata
// intentionally prevents starting any further protocol subprocess.
func TestRunnerVersionProbeFixture(t *testing.T) {
	path := os.Getenv("RUNNER_PROBE_COUNTER")
	if path == "" {
		t.Skip("subprocess fixture only")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("probe\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	t.Fatal("fixture version intentionally unavailable")
}
func TestIdleRunnerDiscoversOncePerPresenceAndNewClaim(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "probes")
	t.Setenv("RUNNER_PROBE_COUNTER", counter)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claims := 0
	var presence []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		if req.URL.Path == "/api/v1/runner/presence" {
			presence = body
		} else if req.URL.Path == "/api/v1/runner/claim" {
			if !bytes.Equal(body, presence) {
				t.Error("new claim did not reuse presence snapshot")
			}
			claims++
			if claims == 3 {
				cancel()
			}
		} else {
			t.Error("unexpected request", req.URL.Path)
		}
		respond(w, nil)
	}))
	defer server.Close()
	r := testRunner(t, server)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.registry.Register(agent.Definition{ID: "counted-probe", Executables: []string{exe}, VersionArgs: []string{"-test.run=^TestRunnerVersionProbeFixture$"}, Protocol: agent.ProtocolCodex, Cancel: true}); err != nil {
		t.Fatal(err)
	}
	r.config.PollInterval = time.Millisecond
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(counter)
	if err != nil || claims != 3 || strings.Count(string(data), "probe\n") != 3 {
		t.Fatal("idle loop duplicated version subprocesses", claims, string(data), err)
	}
}
