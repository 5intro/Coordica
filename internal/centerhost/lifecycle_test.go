package centerhost

import (
	"context"
	"coordica/internal/localfile"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/nodeservice"
	"coordica/internal/store"
)

type installerFake struct {
	Calls int
	Err   error
	Spec  nodeservice.Spec
}

func (f *installerFake) EnsureStarted(_ context.Context, s nodeservice.Spec) (nodeservice.Status, error) {
	f.Calls++
	f.Spec = s
	return nodeservice.Status{Name: "fixture-center", State: "activation_requested"}, f.Err
}
func hostFixture(t *testing.T) (Config, string, Manager, *installerFake) {
	t.Helper()
	return hostFixtureChecks(t, []domain.Check{{Name: "check", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 300}})
}
func hostFixtureChecks(t *testing.T, checks []domain.Check) (Config, string, Manager, *installerFake) {
	t.Helper()
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	binary := filepath.Join(root, "coordica")
	leader := filepath.Join(root, "codex")
	os.WriteFile(binary, []byte("fixture executable"), 0700)
	os.WriteFile(leader, []byte("fixture native path"), 0700)
	c := Config{Version: 1, Role: "center", Executable: binary, CodexExecutable: leader, DataDir: filepath.Join(root, "center"), ChatProfile: filepath.Join(root, "profile", "interaction.json"), Repository: filepath.Join(root, "repository"), Name: "Fixed Project", Branch: "main", ConsoleListen: "127.0.0.1:18080", RunnerListen: "127.0.0.1:18080", RuntimePath: "/usr/bin:/bin", Checks: checks}
	path := filepath.Join(root, "config", "center.json")
	if e := Save(path, c); e != nil {
		t.Fatal(e)
	}
	installer := &installerFake{}
	m := Manager{ConfigPath: path, Installer: installer, ReservePorts: func(context.Context, []string) (func(), error) { return func() {}, nil }}
	return c, path, m, installer
}
func TestCenterDuplicateActivationPreservesIdentityPauseAndAutomation(t *testing.T) {
	c, _, m, installer := hostFixture(t)
	ctx := context.Background()
	result, e := m.Initialize(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if result.ProjectPaused == nil || !*result.ProjectPaused || result.MatchingCenterHealthy || result.Service.State != "activation_requested" {
		t.Fatal("invented runtime readiness", result)
	}
	first, _ := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
	identity, _ := os.ReadFile(filepath.Join(c.DataDir, "center-identity.json"))
	db, e := store.Open(filepath.Join(c.DataDir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := app.New(db, c.DataDir)
	snap, _ := s.Snapshot(ctx, app.Actor{ID: "owner", Role: "owner"})
	settings := snap.Automation
	settings.Enabled = true
	if _, e = s.ConfigureAutomation(ctx, app.Actor{ID: "owner", Role: "owner"}, "automation", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
		t.Fatal(e)
	}
	db.Close()
	result, e = m.Initialize(ctx)
	if e != nil {
		t.Fatal(e)
	}
	second, _ := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
	id2, _ := os.ReadFile(filepath.Join(c.DataDir, "center-identity.json"))
	if string(first) != string(second) || string(identity) != string(id2) || result.AutomationEnabled == nil || !*result.AutomationEnabled || result.ProjectPaused == nil || !*result.ProjectPaused {
		t.Fatal("restart changed identity or saved policy")
	}
	if installer.Spec.CenterConfig != m.ConfigPath || installer.Spec.Executable != c.Executable {
		t.Fatal("unfixed activation arguments")
	}
	status, e := m.Status(ctx)
	if e != nil || status.Service.State != "activation_requested" || status.ProjectPaused != nil || status.ChatReady != nil {
		t.Fatal("offline status misrepresented state", status, e)
	}
}
func TestCenterPortAndSavedConfigConflictsAreUntouched(t *testing.T) {
	c, path, m, installer := hostFixture(t)
	m.ReservePorts = func(context.Context, []string) (func(), error) { return nil, errors.New("busy fixed port") }
	if _, e := m.Initialize(context.Background()); e == nil {
		t.Fatal("busy port accepted")
	}
	if installer.Calls != 0 {
		t.Fatal("busy service mutated")
	}
	if _, e := os.Stat(filepath.Join(c.DataDir, "owner.token")); !os.IsNotExist(e) {
		t.Fatal("busy port minted Owner")
	}
	changed := c
	changed.DataDir = filepath.Join(filepath.Dir(c.DataDir), "different")
	before, _ := os.ReadFile(path)
	if e := Save(path, changed); e == nil {
		t.Fatal("saved identity replaced")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("conflicting fixed config changed")
	}
}
func TestCenterCannotPromoteRunnerOrCopiedChatProfile(t *testing.T) {
	for _, asset := range []string{"runner.json", "limited-profile"} {
		t.Run(asset, func(t *testing.T) {
			c, _, m, installer := hostFixture(t)
			if asset == "runner.json" {
				os.MkdirAll(c.DataDir, 0700)
				os.WriteFile(filepath.Join(c.DataDir, "runner.json"), []byte(`{"node_id":"node"}`), 0600)
			} else {
				os.MkdirAll(filepath.Dir(c.ChatProfile), 0700)
				os.WriteFile(c.ChatProfile, []byte(`{"token":"copied-node"}`), 0600)
			}
			if _, e := m.Initialize(context.Background()); e == nil {
				t.Fatal("node profile promoted host")
			}
			if installer.Calls != 0 {
				t.Fatal("forged role activated center")
			}
			if _, e := os.Stat(filepath.Join(c.DataDir, "owner.token")); !os.IsNotExist(e) {
				t.Fatal("node minted Owner")
			}
		})
	}
}
func TestCenterServiceUnavailableAndPartialProvisionRecoverable(t *testing.T) {
	c, _, m, installer := hostFixture(t)
	m.Provision = func(context.Context, *app.Service, Config) error {
		return errors.New("conflicting inactive limited profile")
	}
	if _, e := m.Initialize(context.Background()); e == nil {
		t.Fatal("failed profile provision hidden")
	}
	first, _ := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
	if len(first) == 0 || installer.Calls != 0 {
		t.Fatal("profile failure activated service")
	}
	m.Provision = nil
	installer.Err = errors.New("launchd GUI domain unavailable")
	if _, e := m.Initialize(context.Background()); e == nil || !strings.Contains(e.Error(), "configuration retained") {
		t.Fatal("manager diagnostics missing")
	}
	status, _ := m.Status(context.Background())
	if status.Service.State != "activation_failed" {
		t.Fatal("failed activation omitted")
	}
	installer.Err = nil
	if _, e := m.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	second, _ := os.ReadFile(filepath.Join(c.DataDir, "owner.token"))
	if string(first) != string(second) {
		t.Fatal("partial recovery replaced Owner")
	}
}
func TestCenterConfigRejectsAliasesRoleAndUnsafeEndpoints(t *testing.T) {
	c, path, _, _ := hostFixture(t)
	for _, change := range []func(*Config){func(c *Config) { c.Role = "node" }, func(c *Config) { c.ChatProfile = path }, func(c *Config) { c.ChatProfile = filepath.Join(c.DataDir, "owner.token") }, func(c *Config) { c.ConsoleListen = "0.0.0.0:8080" }, func(c *Config) { c.RunnerListen = "127.0.0.1:0" }, func(c *Config) { c.HTTPSProxy = "http://owner:secret@proxy" }} {
		q := c
		change(&q)
		if e := Validate(q, path); e == nil {
			t.Fatal("unsafe configuration accepted", q)
		}
	}
}
func TestCenterMatchingHealthCannotTrustUnrelatedServer(t *testing.T) {
	c, path, m, installer := hostFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()
	c.ConsoleListen = strings.TrimPrefix(server.URL, "http://")
	c.RunnerListen = c.ConsoleListen
	os.Remove(path)
	if e := Save(path, c); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Initialize(context.Background()); e != nil {
		t.Fatal(e)
	}
	status, _ := m.Status(context.Background())
	if status.MatchingCenterHealthy || installer.Calls != 1 {
		t.Fatal("bare health trusted")
	}
}
func TestCenterActualBusyRunnerPortPreventsActivation(t *testing.T) {
	c, path, m, installer := hostFixture(t)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	c.RunnerListen = ln.Addr().String()
	c.RunnerURL = "https://" + c.RunnerListen
	os.Remove(path)
	if e = Save(path, c); e != nil {
		t.Fatal(e)
	}
	m.ReservePorts = nil
	if _, e = m.Initialize(context.Background()); e == nil {
		t.Fatal("busy Runner port ignored")
	}
	if installer.Calls != 0 {
		t.Fatal("busy Runner port activated center")
	}
}

func TestCenterDefaultBlankDraftInitializationKeepsChecksEmpty(t *testing.T) {
	c, _, m, installer := hostFixtureChecks(t, []domain.Check{})
	result, e := m.Initialize(context.Background())
	if e != nil || !result.Initialized || installer.Calls != 1 {
		t.Fatal(result, e)
	}
	db, e := store.Open(filepath.Join(c.DataDir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := app.New(db, c.DataDir)
	p, e := s.Project(context.Background())
	if e != nil || !p.Paused || len(p.Checks) != 0 {
		t.Fatal("default setup fabricated active checks", p, e)
	}
	v, e := s.Initialization(context.Background(), app.Actor{ID: "owner", Role: "owner"})
	if e != nil || v.Status != "draft" || v.ApprovedVersion != 0 {
		t.Fatal("blank project admitted", v, e)
	}
}

func TestCenterTLSRunnerHealthMatchesItsFixedIdentity(t *testing.T) {
	identity := Identity{InstanceID: "fixed-instance", ConfigDigest: "fixed-digest"}
	health := domain.CenterHealth{Status: "ok", APIVersion: domain.APIVersion, CenterInstanceID: identity.InstanceID, ConfigDigest: identity.ConfigDigest}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || r.Header.Get("Authorization") != "" {
			t.Error("wrong public health request", r.URL.Path)
		}
		json.NewEncoder(w).Encode(health)
	}))
	defer server.Close()
	dir := t.TempDir()
	if e := localfile.WritePrivate(filepath.Join(dir, "center.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); e != nil {
		t.Fatal(e)
	}
	c := Config{DataDir: dir, ConsoleListen: "127.0.0.1:1", RunnerListen: strings.TrimPrefix(server.URL, "https://")}
	m := Manager{}
	if e := m.runnerHealth(context.Background(), c, identity); e != nil {
		t.Fatal("matching TLS center rejected", e)
	}
	health.CenterInstanceID = "unrelated"
	if e := m.runnerHealth(context.Background(), c, identity); e == nil {
		t.Fatal("unrelated TLS center trusted")
	}
}
