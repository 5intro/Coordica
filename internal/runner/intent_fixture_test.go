package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/httpapi"
	"coordica/internal/store"
)

func newIntentFixtureWithRemote(t *testing.T, bare bool) *intentFixture {
	t.Helper()
	return newIntentFixtureWithPolicy(t, bare, false)
}

func newIntentFixtureWithPolicy(t *testing.T, bare, progressManaged bool) *intentFixture {
	t.Helper()
	dir := t.TempDir()
	source, bundle, base := intentRepository(t, dir, bare)
	db, err := store.Open(filepath.Join(dir, "center.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &intentFixture{
		s: app.New(db, filepath.Join(dir, "center")), owner: app.Actor{ID: "owner", Role: "owner"}, base: base,
	}
	requirement, doc := prepareIntentGoal(t, f, source)
	admitIntentTask(t, f, requirement, doc)
	if progressManaged {
		settings := f.snapshot(t).Automation
		settings.EnforceTimeLimits = false
		if _, err := f.s.ConfigureAutomation(context.Background(), f.owner, "progress-mode", domain.AutomationRequest{
			ExpectedVersion: settings.Version, Settings: settings,
		}); err != nil {
			t.Fatal(err)
		}
	}
	connectIntentRunner(t, f, dir, bundle)
	return f
}

func decodeIntent[T any](t *testing.T, raw []byte, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func intentRepository(t *testing.T, dir string, bare bool) (string, string, gitops.OID) {
	t.Helper()
	ctx := context.Background()
	g := gitops.New("git")
	source := filepath.Join(dir, "source")
	if err := g.Init(ctx, source, false); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/greeting.mjs":       "export const greeting = () => 'Hello';\n",
		"test/greeting.test.mjs": "// baseline\n", ".gitignore": "cache/\n",
	}
	for path, body := range files {
		file := filepath.Join(source, path)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base, err := g.CommitAll(ctx, source, "base", gitops.Identity{Name: "Test", Email: "test@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "input.bundle")
	if _, err := g.CreateSnapshotBundle(ctx, source, base, bundle); err != nil {
		t.Fatal(err)
	}
	if bare {
		remote := filepath.Join(dir, "remote.git")
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "clone", "--bare", "--no-hardlinks", "--", source, remote)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bare fixture: %v %s", err, output)
		}
		source = remote
	}
	return source, bundle, base
}

func prepareIntentGoal(t *testing.T, f *intentFixture, source string) (domain.Requirement, domain.PlanningDocument) {
	t.Helper()
	ctx := context.Background()
	project := domain.Project{
		Name: "Native scope", RepositoryURL: source, TargetBranch: "main", TotalBudgetSeconds: 10000,
		Checks: []domain.Check{{Name: "test", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}},
	}
	if _, err := f.s.CreateProject(ctx, f.owner, "project", project); err != nil {
		t.Fatal(err)
	}
	seedRunnerBaseline(t, f.s)
	raw, err := f.s.CreateRequirement(ctx, f.owner, "req", domain.Requirement{
		Title: "Greeting", Body: "Normalize names and add regression tests",
	})
	requirement := decodeIntent[domain.Requirement](t, raw, err)
	raw, err = f.s.SavePlanning(ctx, f.owner, "goal", domain.PlanningSaveRequest{Document: domain.PlanningDocument{
		Objective: "Normalize greeting", NonGoals: "No other products", Acceptance: "Tests pass",
		RequirementIDs: []string{requirement.ID}, Architecture: "ES modules", DirectoryOutline: "src and test",
		AllowedRoots: []string{"src", "test"}, BudgetSeconds: 2000,
	}})
	doc := decodeIntent[domain.PlanningDocument](t, raw, err)
	if _, err := f.s.AuthorizePlanning(ctx, f.owner, "auth", domain.VersionRequest{ExpectedVersion: doc.Version}); err != nil {
		t.Fatal(err)
	}
	return requirement, doc
}

func admitIntentTask(t *testing.T, f *intentFixture, requirement domain.Requirement, doc domain.PlanningDocument) {
	t.Helper()
	ctx := context.Background()
	settings := domain.DefaultAutomationSettings()
	settings.Enabled = true
	settings.EnforceTimeLimits = true
	settings.PlanningBudgetSeconds = 900
	if _, err := f.s.ConfigureAutomation(ctx, f.owner, "auto", domain.AutomationRequest{
		ExpectedVersion: 1, Settings: settings,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SchedulePlanning(ctx, f.owner, "plan", domain.PlanningMessageRequest{ExpectedVersion: doc.Version}); err != nil {
		t.Fatal(err)
	}
	f.w = &app.PlanningWorker{Service: f.s, Executor: intentLeader(func() domain.LeaderResult {
		return domain.LeaderResult{
			Reply: "Ready", Ready: true, MissingItems: []string{}, Clarifications: []string{},
			Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{},
			Tasks: []domain.TaskProposal{{
				Key: "greeting", Title: "Greeting", Objective: requirement.Body, Acceptance: "Tests pass",
				Paths: []string{"test/**"}, RequirementIDs: []string{requirement.ID}, Dependencies: []string{}, HotResources: []string{},
				BudgetSeconds: 300, MaxRunSeconds: 60, MaxRetries: 2, MaxRepairs: 2,
			}},
		}
	})}
	if err := f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
}

func connectIntentRunner(t *testing.T, f *intentFixture, dir, bundle string) {
	t.Helper()
	ctx := context.Background()
	raw, err := f.s.CreatePairing(ctx, f.owner, "pair", "Runner")
	pair := decodeIntent[map[string]string](t, raw, err)
	registered, err := f.s.Register(ctx, domain.RegisterRequest{Code: pair["code"], Name: "Runner", Platform: "test", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.node, err = f.s.Authenticate(ctx, registered.Token)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = f.s.Claim(ctx, f.node, "claim", domain.ClaimRequest{ProgressVersion: domain.ProgressVersion,
		AssignmentContextVersion: domain.AssignmentContextVersion,
		Capabilities:             []domain.Capability{{Tool: "codex", Available: true, Structured: true, Review: true, Cancel: true}},
	}, f.base.String())
	assignment := decodeIntent[domain.Assignment](t, raw, err)
	if assignment.Run.ID == "" || assignment.GoalContext == nil {
		t.Fatal("Phase B assignment absent")
	}
	f.r, err = New(Config{
		CenterURL: "http://127.0.0.1:8080", DataDir: filepath.Join(dir, "runner"),
		NodeID: registered.NodeID, Token: registered.Token,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.r.OnEvent = func(Event) {}
	f.record = spool{
		Assignment: assignment, Key: "native-complete", StartedAt: time.Now().Add(-3 * time.Second),
		WorkDir: filepath.Join(f.r.config.DataDir, "runs", "fixture", "workspace"),
	}
	if err := writePrivateJSON(f.r.spoolPath(assignment.Run.ID), f.record); err != nil {
		t.Fatal(err)
	}
	f.r.http.Transport = intentHTTPTransport(t, f, bundle, assignment.Run.ID)
}

func intentHTTPTransport(t *testing.T, f *intentFixture, bundle, runID string) http.RoundTripper {
	t.Helper()
	handler := (&httpapi.Server{Service: f.s}).Handler()
	return intentTransport(func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if req.Header.Get("Authorization") != "Bearer "+f.r.config.Token {
			t.Fatal("missing authenticated node boundary")
		}
		w := httptest.NewRecorder()
		w.Header().Set("Date", f.s.Now().UTC().Format(http.TimeFormat))
		switch {
		case strings.HasSuffix(req.URL.Path, "input.bundle"):
			body, err := os.ReadFile(bundle)
			if err != nil {
				t.Fatal(err)
			}
			w.Write(body)
		case strings.HasSuffix(req.URL.Path, "/artifacts"):
			f.artifactCalls++
			req.RemoteAddr = "127.0.0.1:12345"
			handler.ServeHTTP(w, req)
		default:
			raw, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			req.Body = io.NopCloser(bytes.NewReader(raw))
			req.RemoteAddr = "127.0.0.1:12345"
			if strings.HasSuffix(req.URL.Path, "/request_scope") {
				f.scopeCalls++
				f.scopeBodies = append(f.scopeBodies, string(raw))
				f.scopeKeys = append(f.scopeKeys, req.Header.Get("Idempotency-Key"))
				var persisted spool
				err := readJSON(f.r.spoolPath(runID), &persisted)
				if err != nil || persisted.Request != nil || persisted.Intent == nil || !persisted.Intent.Completion.Quiescent {
					t.Fatal("Complete emitted before scope receipt or missing stopped observation", err)
				}
				if f.rejectScope {
					w.WriteHeader(http.StatusConflict)
					w.Write([]byte(`{"code":"stale_run","message":"stale fixture"}`))
					return w.Result(), nil
				}
			}
			if strings.HasSuffix(req.URL.Path, "/block") {
				f.blockCalls++
			}
			if strings.HasSuffix(req.URL.Path, "/complete") {
				f.completeCalls++
			}
			handler.ServeHTTP(w, req)
			if strings.HasSuffix(req.URL.Path, "/request_scope") {
				f.scopeReplies = append(f.scopeReplies, w.Body.String())
				if f.lostScopeACK {
					return nil, io.ErrUnexpectedEOF
				}
			}
		}
		return w.Result(), nil
	})
}
