package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
