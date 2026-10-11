package onboarding

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/filelock"
	"coordica/internal/localfile"
	"coordica/internal/nodeservice"
	"coordica/internal/runner"
)

type installerFunc func(context.Context, nodeservice.Spec) (nodeservice.Status, error)

func (f installerFunc) EnsureStarted(ctx context.Context, spec nodeservice.Spec) (nodeservice.Status, error) {
	return f(ctx, spec)
}
func initializationFixture(t *testing.T) (*httptest.Server, *atomic.Int32, Config) {
	t.Helper()
	count := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/runner/register" {
			t.Error("unexpected initialization HTTP", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		count.Add(1)
		var req domain.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code != "one-time-secret" {
			t.Error("invalid registration")
		}
		json.NewEncoder(w).Encode(domain.RegisterResponse{NodeID: "node_fixture", Token: "fixture-node-secret"})
	}))
	overrides := map[string]string{}
	for _, definition := range agent.BuiltinDefinitions() {
		overrides[definition.ID] = filepath.Join(t.TempDir(), "not-installed", definition.ID)
	}
	return server, count, Config{CenterURL: server.URL, PairingCode: "one-time-secret", DataDir: filepath.Join(t.TempDir(), "节点 空间"), Capacity: 2, Executables: overrides}
}

func TestInitializationPairsAndStartsOnlyCurrentExecutable(t *testing.T) {
	server, count, cfg := initializationFixture(t)
	defer server.Close()
	var captured nodeservice.Spec
	result, err := InitializeWith(context.Background(), cfg, installerFunc(func(ctx context.Context, spec nodeservice.Spec) (nodeservice.Status, error) {
		captured = spec
		return nodeservice.Status{Name: "fixture-service", State: "activation_requested"}, nil
	}))
	if err != nil || !result.Registered || result.NodeID != "node_fixture" || count.Load() != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	executable, _ := os.Executable()
	absolute, _ := filepath.Abs(executable)
	if captured.Executable != absolute || captured.DataDir != cfg.DataDir || captured.RuntimePath != os.Getenv("PATH") {
		t.Fatalf("untrusted service boundary: %+v", captured)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "token") || strings.Contains(string(raw), "online") {
		t.Fatal("credentials or false online status returned")
	}
	persisted, err := runner.Load(cfg.DataDir)
	if err != nil || persisted.Token != "fixture-node-secret" || persisted.RuntimePath != os.Getenv("PATH") {
		t.Fatal("paired identity/runtime path not saved", err)
	}
}

func TestActivationFailureRetainsIdentityAndRetryDoesNotPairAgain(t *testing.T) {
	server, count, cfg := initializationFixture(t)
	defer server.Close()
	result, err := InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{Name: "fixture-service", State: "failed"}, errors.New("failure one-time-secret fixture-node-secret")
	}))
	if err == nil || !result.Registered || result.Service.State != "failed" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("failure result lost identity or leaked credentials: %+v %v", result, err)
	}
	spool := filepath.Join(cfg.DataDir, "spool", "retained.json")
	if err := localfile.WritePrivate(spool, []byte("durable pending handoff")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.PairingCode = "different-unused-code"
	result, err = InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{Name: "fixture-service", State: "started"}, nil
	}))
	if err != nil || !result.Registered || count.Load() != 1 {
		t.Fatalf("retry paired again: %+v %v", result, err)
	}
	after, _ := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	data, _ := os.ReadFile(spool)
	if string(before) != string(after) || string(data) != "durable pending handoff" {
		t.Fatal("retry overwrote identity or spool")
	}
	cfg.CenterURL = "https://different-center.invalid"
	if _, err = InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		t.Fatal("different center installed service")
		return nodeservice.Status{}, nil
	})); err == nil {
		t.Fatal("different center accepted")
	}
}

func TestLegacyRegistrationRefreshPreservesIdentity(t *testing.T) {
	dir := t.TempDir()
	cfg := runner.Config{CenterURL: "https://center.invalid", DataDir: dir, NodeID: "preserved-node", Token: "preserved-secret", Capacity: 2}
	raw, _ := json.Marshal(cfg)
	if err := localfile.WritePrivate(filepath.Join(dir, "runner.json"), raw); err != nil {
		t.Fatal(err)
	}
	_, err := InitializeWith(context.Background(), Config{CenterURL: cfg.CenterURL, DataDir: dir}, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{Name: "fixture", State: "started"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := runner.Load(dir)
	if err != nil || loaded.NodeID != cfg.NodeID || loaded.Token != cfg.Token || loaded.RuntimePath != os.Getenv("PATH") {
		t.Fatal("legacy identity changed", err)
	}
}

func TestRetryRefreshesLocalRuntimeWithoutChangingIdentityOrSpool(t *testing.T) {
	server, count, cfg := initializationFixture(t)
	defer server.Close()
	_, err := InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{State: "failed"}, errors.New("service fixture could not start")
	}))
	if err == nil {
		t.Fatal("expected first activation to fail")
	}
	before, err := runner.Load(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(cfg.DataDir, "spool", "retained.json")
	if err := localfile.WritePrivate(spool, []byte("pending handoff remains byte-for-byte")); err != nil {
		t.Fatal(err)
	}
	updatedPath := os.Getenv("PATH") + string(os.PathListSeparator) + filepath.Join(t.TempDir(), "修复 路径")
	t.Setenv("PATH", updatedPath)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tlsFixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer tlsFixture.Close()
	caPath := filepath.Join(t.TempDir(), "公共 CA.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsFixture.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.PairingCode = "unused-new-code"
	cfg.Name, cfg.Capacity = "do-not-replace-existing-name", 1
	cfg.Executables["codex"], cfg.TLSCA = executable, caPath
	result, err := InitializeWith(context.Background(), cfg, installerFunc(func(ctx context.Context, spec nodeservice.Spec) (nodeservice.Status, error) {
		if spec.RuntimePath != updatedPath {
			t.Fatal("service received stale runtime PATH")
		}
		// Activation may acquire the directory after refresh releases its lock.
		unlock, err := filelock.Acquire(filepath.Join(cfg.DataDir, "runner.lock"))
		if err != nil {
			t.Fatal("runtime refresh retained the Runner lock", err)
		}
		unlock()
		return nodeservice.Status{Name: "fixture", State: "started"}, nil
	}))
	if err != nil || !result.Registered || count.Load() != 1 {
		t.Fatalf("retry=%+v err=%v registrations=%d", result, err, count.Load())
	}
	after, err := runner.Load(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if after.NodeID != before.NodeID || after.Token != before.Token || after.Name != before.Name || after.Capacity != before.Capacity || after.PollInterval != before.PollInterval {
		t.Fatal("runtime refresh changed identity or scheduling configuration")
	}
	if after.RuntimePath != updatedPath || after.Executables["codex"] != executable || after.TLSCA != caPath {
		t.Fatal("retry did not save corrected local runtime")
	}
	data, err := os.ReadFile(spool)
	if err != nil || string(data) != "pending handoff remains byte-for-byte" {
		t.Fatal("retry changed pending handoff", err)
	}
}

func TestInUseRunnerDoesNotRefreshOrTouchService(t *testing.T) {
	server, count, cfg := initializationFixture(t)
	defer server.Close()
	_, err := InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{Name: "fixture", State: "started"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := filelock.Acquire(filepath.Join(cfg.DataDir, "runner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+"changed-runtime-path")
	cfg.TLSCA = filepath.Join(t.TempDir(), "not-a-ca")
	result, err := InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		t.Fatal("initialization touched the service while Runner held its lock")
		return nodeservice.Status{}, nil
	}))
	if err == nil || !result.Registered || result.Service.State != "directory_in_use" || count.Load() != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	after, _ := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	if string(before) != string(after) || !strings.Contains(strings.Join(result.Steps, " "), "service_unchanged") {
		t.Fatal("locked retry altered configuration or omitted service boundary")
	}
}

func TestInvalidReplacementCARetainsSavedConfiguration(t *testing.T) {
	server, _, cfg := initializationFixture(t)
	defer server.Close()
	installer := installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		return nodeservice.Status{Name: "fixture", State: "started"}, nil
	})
	if _, err := InitializeWith(context.Background(), cfg, installer); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	cfg.TLSCA = filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(cfg.TLSCA, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := InitializeWith(context.Background(), cfg, installerFunc(func(context.Context, nodeservice.Spec) (nodeservice.Status, error) {
		t.Fatal("invalid CA activated service")
		return nodeservice.Status{}, nil
	}))
	if err == nil || !result.Registered {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	after, _ := os.ReadFile(filepath.Join(cfg.DataDir, "runner.json"))
	if string(before) != string(after) {
		t.Fatal("invalid CA changed saved configuration")
	}
}

func TestRuntimeRefreshRetainsCustomDefinitionsAndExplicitRemoval(t *testing.T) {
	custom := agent.Definition{ID: "local-custom", Executables: []string{"custom-cli"}, Protocol: agent.ProtocolCodex, Cancel: true, Review: true}
	dir := t.TempDir()
	current := runner.Config{CenterURL: "https://center.invalid", DataDir: dir, NodeID: "node_custom", Token: "private-token", Capacity: 1, Tools: []agent.Definition{custom}, Executables: map[string]string{custom.ID: "/not-installed/custom-cli"}}
	refreshed, err := refreshRuntime(Config{}, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Tools) != 1 || refreshed.Tools[0].ID != custom.ID || refreshed.Executables[custom.ID] != current.Executables[custom.ID] || refreshed.NodeID != current.NodeID || refreshed.Token != current.Token {
		t.Fatal("default bootstrap refresh lost local custom registration or identity")
	}
	removed, err := refreshRuntime(Config{Tools: []agent.Definition{}}, refreshed)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Tools) != 0 || removed.Executables[custom.ID] != "" {
		t.Fatal("explicit local registry removal retained removed driver")
	}
}
