package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/httpapi"
	"coordica/internal/store"
)

type intentTransport func(*http.Request) (*http.Response, error)

func (f intentTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type intentLeader func() domain.LeaderResult

func (f intentLeader) Execute(context.Context, string, string, func(int) error) (app.PlanningOutcome, error) {
	b, err := json.Marshal(f())
	return app.PlanningOutcome{Output: string(b), TreeStopped: true, ElapsedSeconds: 1}, err
}

type intentFixture struct {
	r                                                    *Runner
	s                                                    *app.Service
	owner, node                                          app.Actor
	w                                                    *app.PlanningWorker
	record                                               spool
	base                                                 gitops.OID
	scopeCalls, blockCalls, completeCalls, artifactCalls int
	scopeBodies, scopeKeys, scopeReplies                 []string
	lostScopeACK, rejectScope                            bool
}

// Runs the real Runner and authenticated HTTP application boundary without a
// listening socket. Native execution is a protocol fixture, not a live-model
// claim. Real private Git inputs still exercise the snapshot/scope checks.
func newIntentFixture(t *testing.T) *intentFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	g := gitops.New("git")
	source := filepath.Join(dir, "source")
	if err := g.Init(ctx, source, false); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"src/greeting.mjs": "export const greeting = () => 'Hello';\n", "test/greeting.test.mjs": "// baseline\n", ".gitignore": "cache/\n"} {
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
	if _, err = g.CreateSnapshotBundle(ctx, source, base, bundle); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "center.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &intentFixture{s: app.New(db, filepath.Join(dir, "center")), owner: app.Actor{ID: "owner", Role: "owner"}, base: base}
	decode := func(b json.RawMessage, err error, out any) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.s.CreateProject(ctx, f.owner, "project", domain.Project{Name: "Native scope", RepositoryURL: source, TargetBranch: "main", TotalBudgetSeconds: 10000, Checks: []domain.Check{{Name: "test", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}}}); err != nil {
		t.Fatal(err)
	}
	seedRunnerBaseline(t, f.s)
	b, err := f.s.CreateRequirement(ctx, f.owner, "req", domain.Requirement{Title: "Greeting", Body: "Normalize names and add regression tests"})
	var requirement domain.Requirement
	decode(b, err, &requirement)
	b, err = f.s.SavePlanning(ctx, f.owner, "goal", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "Normalize greeting", NonGoals: "No other products", Acceptance: "Tests pass", RequirementIDs: []string{requirement.ID}, Architecture: "ES modules", DirectoryOutline: "src and test", AllowedRoots: []string{"src", "test"}, BudgetSeconds: 2000}})
	var doc domain.PlanningDocument
	decode(b, err, &doc)
	if _, err = f.s.AuthorizePlanning(ctx, f.owner, "auth", domain.VersionRequest{ExpectedVersion: doc.Version}); err != nil {
		t.Fatal(err)
	}
	settings := domain.DefaultAutomationSettings()
	settings.Enabled = true
	if _, err = f.s.ConfigureAutomation(ctx, f.owner, "auto", domain.AutomationRequest{ExpectedVersion: 1, Settings: settings}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SchedulePlanning(ctx, f.owner, "plan", domain.PlanningMessageRequest{ExpectedVersion: doc.Version}); err != nil {
		t.Fatal(err)
	}
	f.w = &app.PlanningWorker{Service: f.s, Executor: intentLeader(func() domain.LeaderResult {
		return domain.LeaderResult{Reply: "Ready", Ready: true, MissingItems: []string{}, Clarifications: []string{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}, Tasks: []domain.TaskProposal{{Key: "greeting", Title: "Greeting", Objective: requirement.Body, Acceptance: "Tests pass", Paths: []string{"test/**"}, RequirementIDs: []string{requirement.ID}, Dependencies: []string{}, HotResources: []string{}, BudgetSeconds: 300, MaxRunSeconds: 60, MaxRetries: 2, MaxRepairs: 2}}}
	})}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	b, err = f.s.CreatePairing(ctx, f.owner, "pair", "Runner")
	var pair map[string]string
	decode(b, err, &pair)
	registered, err := f.s.Register(ctx, domain.RegisterRequest{Code: pair["code"], Name: "Runner", Platform: "test", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.node, err = f.s.Authenticate(ctx, registered.Token)
	if err != nil {
		t.Fatal(err)
	}
	b, err = f.s.Claim(ctx, f.node, "claim", domain.ClaimRequest{AssignmentContextVersion: domain.AssignmentContextVersion, Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Review: true, Cancel: true}}}, base.String())
	var assignment domain.Assignment
	decode(b, err, &assignment)
	if assignment.Run.ID == "" || assignment.GoalContext == nil {
		t.Fatal("Phase B assignment absent")
	}
	f.r, err = New(Config{CenterURL: "http://127.0.0.1:8080", DataDir: filepath.Join(dir, "runner"), NodeID: registered.NodeID, Token: registered.Token})
	if err != nil {
		t.Fatal(err)
	}
	f.r.OnEvent = func(Event) {}
	f.record = spool{Assignment: assignment, Key: "native-complete", StartedAt: time.Now().Add(-3 * time.Second), WorkDir: filepath.Join(f.r.config.DataDir, "runs", "fixture", "workspace")}
	if err = writePrivateJSON(f.r.spoolPath(assignment.Run.ID), f.record); err != nil {
		t.Fatal(err)
	}
	handler := (&httpapi.Server{Service: f.s}).Handler()
	f.r.http.Transport = intentTransport(func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if req.Header.Get("Authorization") != "Bearer "+registered.Token {
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
				if err := readJSON(f.r.spoolPath(assignment.Run.ID), &persisted); err != nil || persisted.Request != nil || persisted.Intent == nil || !persisted.Intent.Completion.Quiescent {
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
	return f
}

func (f *intentFixture) snapshot(t *testing.T) domain.Snapshot {
	t.Helper()
	snap, err := f.s.Snapshot(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func scopeIntentText() string {
	return `{"version":1,"status":"scope_requested","requested_paths":["src/greeting.mjs"],"reason":"Existing task needs the greeting implementation","summary":"Stopped before any edits"}`
}

func TestAssignedExecutionOptionsReachAdapterWithoutReadingMutableNodeSettings(t *testing.T) {
	f := newIntentFixture(t)
	f.record.Assignment.Run.Model = "gpt-6-astra"
	f.record.Assignment.Run.ReasoningEffort = "ultra"
	called := false
	f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
		called = true
		if req.Model != "gpt-6-astra" || req.ReasoningEffort != "ultra" {
			t.Fatalf("assigned settings lost: %+v", req)
		}
		return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: scopeIntentText(), SessionID: "native-fixture"}, nil
	}
	f.perform(context.Background())
	if !called {
		t.Fatal("execution adapter not reached")
	}
}

func (f *intentFixture) perform(ctx context.Context) {
	now := time.Now()
	f.r.perform(ctx, f.record, responseClock{Server: now, Sent: now, Received: now})
}

func TestNativeScopeIntentStopsAndAutomaticallyExtendsOriginalTask(t *testing.T) {
	f := newIntentFixture(t)
	f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
		b, err := os.ReadFile(req.OutputSchemaPath)
		if err != nil || !bytes.Equal(b, executionOutputSchema) || !strings.Contains(req.Prompt, nativeIntentPrompt) || strings.Contains(req.Prompt, f.r.config.Token) {
			t.Fatal("native schema/prompt boundary lost", err)
		}
		return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: scopeIntentText(), SessionID: "native-fixture"}, nil
	}
	f.perform(context.Background())
	snap := f.snapshot(t)
	if f.scopeCalls != 1 || f.completeCalls != 1 || f.artifactCalls != 0 || len(snap.ScopeRequests) != 1 || len(snap.Candidates) != 0 {
		t.Fatal("scope did not reach exact stopped handoff", snap, f.scopeCalls)
	}
	run, task, scope := snap.Runs[0], snap.Tasks[0], snap.ScopeRequests[0]
	if run.State != "stopped" || !run.Quiescent || task.HoldReason != "scope_requested" || task.Repairs != 0 || task.Retries != 0 || task.UsedSeconds < 3 || !reflect.DeepEqual(task.AllowedPaths, []string{"test/**"}) {
		t.Fatal("intent conferred authority/refunded spend/consumed repair", run, task)
	}
	var req domain.RunScopeRequest
	if err := json.Unmarshal([]byte(f.scopeBodies[0]), &req); err != nil || req.Generation != f.record.Assignment.Run.Generation || !reflect.DeepEqual(req.Paths, []string{"src/greeting.mjs"}) || f.scopeKeys[0] != intentKey(f.record.Assignment.Run) {
		t.Fatal("request generation/path/key mismatch", req, err)
	}
	head, err := f.r.git.Head(context.Background(), f.record.WorkDir)
	if err != nil || head != f.base {
		t.Fatal("scope intent committed private changes", err)
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(f.record.WorkDir), "result.bundle")); !os.IsNotExist(err) {
		t.Fatal("scope intent created material", err)
	}
	f.w.Executor = intentLeader(func() domain.LeaderResult {
		return domain.LeaderResult{Reply: "Extend original task", Ready: true, MissingItems: []string{}, Clarifications: []string{}, MajorDecisions: []domain.MajorDecisionProposal{}, Tasks: []domain.TaskProposal{}, Scopes: []domain.ScopeProposal{{TaskID: task.ID, ExpectedVersion: task.Version, Paths: append(append([]string{}, task.AllowedPaths...), scope.Paths...), Reason: scope.Reason}}}
	})
	if err = f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if len(after.Tasks) != 1 || after.Tasks[0].ID != task.ID || after.Tasks[0].State != "ready" || after.Tasks[0].UsedSeconds != task.UsedSeconds || after.Tasks[0].Repairs != task.Repairs || after.Tasks[0].Retries != task.Retries || after.ScopeRequests[0].State != "consumed" || after.Tasks[0].AuthorizationVersion != task.AuthorizationVersion+1 {
		t.Fatal("center did not assess original scope cumulatively", after)
	}
}

func TestNativeScopeLostReceiptReplaysBeforeCompleteAfterRestart(t *testing.T) {
	f := newIntentFixture(t)
	f.lostScopeACK = true
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{TreeStopped: true, OutputText: scopeIntentText()}, nil
	}
	f.perform(context.Background())
	if f.scopeCalls != 2 || f.completeCalls != 0 || len(f.snapshot(t).ScopeRequests) != 1 {
		t.Fatal("uncertain receipt bypassed ordering or duplicated request")
	}
	var pending spool
	if err := readJSON(f.r.spoolPath(f.record.Assignment.Run.ID), &pending); err != nil || pending.Request != nil || pending.Intent == nil || pending.Intent.Attempts != 2 {
		t.Fatal("pending exact receipt not persisted", err)
	}
	f.lostScopeACK = false
	if err := f.r.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.scopeCalls != 3 || f.completeCalls != 1 || f.artifactCalls != 0 {
		t.Fatal("restart did not settle receipt before stop")
	}
	for i := 1; i < len(f.scopeBodies); i++ {
		if f.scopeBodies[i] != f.scopeBodies[0] || f.scopeKeys[i] != f.scopeKeys[0] || f.scopeReplies[i] != f.scopeReplies[0] {
			t.Fatal("uncertain replay changed body/key/receipt")
		}
	}
	if snap := f.snapshot(t); !snap.Runs[0].Quiescent || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 0 {
		t.Fatal("receipt replay changed accounting", snap)
	}
}

func TestNativeIntentCannotDiscoverOrGrantScopeFromUnsafeResults(t *testing.T) {
	for _, test := range []struct {
		name, output, change         string
		quiet, cancel, expire, stale bool
		exit                         int
	}{
		{name: "prose", output: "Need src/greeting.mjs; stopped", quiet: true},
		{name: "unknown field", output: strings.Replace(scopeIntentText(), `"version":1`, `"version":1,"approved":true`, 1), quiet: true},
		{name: "null path", output: strings.Replace(scopeIntentText(), `["src/greeting.mjs"]`, `[null]`, 1), quiet: true},
		{name: "trailing prose", output: scopeIntentText() + " all done", quiet: true},
		{name: "unknown tree", output: scopeIntentText()},
		{name: "unknown tree without identity", output: scopeIntentText()},
		{name: "cancelled caller", output: scopeIntentText(), quiet: true, cancel: true},
		{name: "expired center lease", output: scopeIntentText(), quiet: true, expire: true},
		{name: "stale current generation", output: scopeIntentText(), quiet: true, stale: true},
		{name: "unauthorized edits", output: scopeIntentText(), quiet: true, change: "src/greeting.mjs"},
		{name: "authorized partial edits", output: scopeIntentText(), quiet: true, change: "test/greeting.test.mjs"},
		{name: "ignored edits", output: scopeIntentText(), quiet: true, change: "cache/ignored.mjs"},
		{name: "failed native exit", output: scopeIntentText(), quiet: true, exit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newIntentFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.r.execute = func(_ context.Context, req agent.Request, onEvent func(agent.Event)) (agent.Result, error) {
				if !test.quiet && test.name != "unknown tree without identity" {
					onEvent(agent.Event{Type: "process.started", Data: json.RawMessage(`{"pid":999999}`)})
				}
				if test.change != "" {
					p := filepath.Join(req.WorkDir, test.change)
					if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte("private unauthorized input"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if test.cancel {
					cancel()
				}
				if test.expire {
					f.s.Now = func() time.Time { return f.record.Assignment.Run.LeaseUntil.Add(time.Second) }
				}
				if test.stale {
					if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
						var task domain.Task
						if err := tx.Get("task", f.record.Assignment.Task.ID, &task); err != nil {
							return err
						}
						task.Generation++
						return tx.Put("task", task.ID, task)
					}); err != nil {
						t.Fatal(err)
					}
				}
				return agent.Result{ExitCode: test.exit, TreeStopped: test.quiet, OutputText: test.output}, nil
			}
			f.perform(ctx)
			snap := f.snapshot(t)
			if len(snap.ScopeRequests) != 0 || f.artifactCalls != 0 || len(snap.Candidates) != 0 || !reflect.DeepEqual(snap.Tasks[0].AllowedPaths, []string{"test/**"}) {
				t.Fatal("unsafe result requested/granted scope or material", snap)
			}
			if !test.expire && !test.stale && f.scopeCalls != 0 {
				t.Fatal("unsafe executor intent reached scope endpoint")
			}
			if !test.quiet && snap.Runs[0].Quiescent {
				t.Fatal("unknown tree reported stopped")
			}
		})
	}
}

func TestNativeBlockedIntentUsesExistingBlockAndNeverSnapshots(t *testing.T) {
	f := newIntentFixture(t)
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{TreeStopped: true, OutputText: `{"version":1,"status":"blocked","requested_paths":[],"reason":"Major architecture change requires owner decision","summary":"Stopped before edits"}`}, nil
	}
	f.perform(context.Background())
	snap := f.snapshot(t)
	if f.blockCalls != 1 || f.scopeCalls != 0 || f.artifactCalls != 0 || f.completeCalls != 1 || snap.Runs[0].State != "stopped" || !snap.Runs[0].Quiescent || snap.Tasks[0].HoldReason != "human_decision" || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 0 {
		t.Fatal("blocked intent bypassed existing owner gate", snap)
	}
}

func TestNativeCompletedIntentStillRequiresActualAuthorizedChange(t *testing.T) {
	for _, path := range []string{"", "src/greeting.mjs", "test/greeting.test.mjs"} {
		t.Run("path="+path, func(t *testing.T) {
			f := newIntentFixture(t)
			f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
				if path != "" {
					if err := os.WriteFile(filepath.Join(req.WorkDir, path), []byte("// actual private change\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return agent.Result{TreeStopped: true, OutputText: `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Implementation finished"}`}, nil
			}
			f.perform(context.Background())
			snap := f.snapshot(t)
			if f.scopeCalls != 0 {
				t.Fatal("completed intent discovered scope")
			}
			head, err := f.r.git.Head(context.Background(), f.record.WorkDir)
			if err != nil {
				t.Fatal(err)
			}
			if path == "test/greeting.test.mjs" {
				if head == f.base || f.artifactCalls != 1 || len(snap.Candidates) != 1 || snap.Runs[0].State != "succeeded" {
					t.Fatal("authorized actual diff/material was not verified", snap)
				}
			} else if head != f.base || f.artifactCalls != 0 || len(snap.Candidates) != 0 {
				t.Fatal("completed prose authorized missing/outside changes", snap)
			}
		})
	}
}

func TestNativeIntentDeliveryIsFiniteAndNeverGenericFailure(t *testing.T) {
	f := newIntentFixture(t)
	f.lostScopeACK = true
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{TreeStopped: true, OutputText: scopeIntentText()}, nil
	}
	f.perform(context.Background())
	f.r.flush(context.Background())
	f.r.flush(context.Background())
	if f.scopeCalls != maxIntentAttempts || f.completeCalls != 1 {
		t.Fatal("unbounded receipt attempts", f.scopeCalls, f.completeCalls)
	}
	snap := f.snapshot(t)
	if snap.Runs[0].State != "stopped" || !snap.Runs[0].Quiescent || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 0 || f.artifactCalls != 0 {
		t.Fatal("scope uncertainty became generic retry/material", snap)
	}
	if _, err := os.Stat(f.r.spoolPath(f.record.Assignment.Run.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed handoff not acknowledged", err)
	}
}

func TestNativeJSONLExecutorCarriesSchemaAndScopeIntentThroughRealStoppedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix native-protocol CLI fixture")
	}
	f := newIntentFixture(t)
	cli := filepath.Join(t.TempDir(), "fixture-codex")
	event, err := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": scopeIntentText()}})
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
schema=''
previous=''
model=0
isolated=0
for arg in "$@"; do
  if [ "$previous" = '--output-schema' ]; then schema="$arg"; fi
  if [ "$previous" = '--model' ] && [ "$arg" = 'gpt-6.1-sol' ]; then model=1; fi
  if [ "$arg" = '--ignore-user-config' ]; then isolated=1; fi
  previous="$arg"
done
if [ ! -r "$schema" ] || [ "$model$isolated" != '11' ]; then exit 42; fi
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"native-jsonl-fixture"}'
printf '%s\n' '` + string(event) + `'
printf '%s\n' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.r.config.Executables["codex"] = cli
	f.r.execute = f.r.registry.Execute
	f.perform(context.Background())
	snap := f.snapshot(t)
	if f.scopeCalls != 1 || f.completeCalls != 1 || f.artifactCalls != 0 || snap.Runs[0].SessionID != "native-jsonl-fixture" || !snap.Runs[0].Quiescent || snap.Runs[0].State != "stopped" || len(snap.ScopeRequests) != 1 || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 0 {
		t.Fatal("native process/schema/protocol bridge failed", snap)
	}
}

func TestNativeIntentWithoutReceiptStopsWithoutOfflineRetry(t *testing.T) {
	f := newIntentFixture(t)
	f.rejectScope = true
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{TreeStopped: true, OutputText: scopeIntentText()}, nil
	}
	f.perform(context.Background())
	snap := f.snapshot(t)
	if f.scopeCalls != 1 || f.completeCalls != 1 || f.artifactCalls != 0 || len(snap.ScopeRequests) != 0 || snap.Tasks[0].HoldReason != "human_decision" || snap.Tasks[0].Repairs != 0 || snap.Tasks[0].Retries != 0 || !snap.Runs[0].Quiescent || snap.Runs[0].State != "stopped" {
		t.Fatal("missing scope receipt was mapped to generic retry or grant", snap)
	}
}

func TestPendingNativeIntentCannotSubmitUnknownTreeAfterRestart(t *testing.T) {
	f := newIntentFixture(t)
	intent, err := domain.ParseExecutionIntent(scopeIntentText())
	if err != nil {
		t.Fatal(err)
	}
	f.record.Intent = &intentHandoff{Intent: intent, Key: intentKey(f.record.Assignment.Run), Completion: domain.CompleteRequest{Generation: f.record.Assignment.Run.Generation, Status: "stopped", IntentStatus: "scope_requested", Quiescent: false}}
	if err := writePrivateJSON(f.r.spoolPath(f.record.Assignment.Run.ID), f.record); err != nil {
		t.Fatal(err)
	}
	if err := f.r.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.scopeCalls != 0 || f.completeCalls != 0 {
		t.Fatal("restart fabricated stop evidence for a scope request")
	}
	slots, err := f.r.occupiedSlots()
	if err != nil || slots != 1 {
		t.Fatal("unknown pending intent released local execution slot", slots, err)
	}
}

func TestLegacyPhaseAAndCustomToolsKeepTheirExistingCompletionProtocol(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "Phase A builtin", true: "Phase B custom tool"}[custom], func(t *testing.T) {
			f := newIntentFixture(t)
			if custom {
				def := agent.Definition{ID: "legacy-fixture", Executables: []string{"unused-fixture"}, Protocol: agent.ProtocolACP, CommitMode: "runner", Cancel: true, Review: true}
				if err := f.r.registry.Register(def); err != nil {
					t.Fatal(err)
				}
				f.record.Assignment.Run.Tool = def.ID
			} else {
				f.record.Assignment.GoalContext = nil
			}
			f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
				if req.OutputSchemaPath != "" || strings.Contains(req.Prompt, nativeIntentPrompt) {
					t.Fatal("native schema leaked into legacy/custom protocol")
				}
				if err := os.WriteFile(filepath.Join(req.WorkDir, "test/greeting.test.mjs"), []byte("// actual legacy change\n"), 0600); err != nil {
					t.Fatal(err)
				}
				return agent.Result{TreeStopped: true, OutputText: "Need more scope; this prose must remain legacy and never request it"}, nil
			}
			f.perform(context.Background())
			snap := f.snapshot(t)
			if f.scopeCalls != 0 || f.artifactCalls != 1 || snap.Runs[0].State != "succeeded" || len(snap.Candidates) != 1 {
				t.Fatal("legacy/custom completion changed", snap)
			}
		})
	}
}

func TestPendingScopeReplayRechecksUnchangedPhysicalWorkspace(t *testing.T) {
	f := newIntentFixture(t)
	f.lostScopeACK = true
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{TreeStopped: true, OutputText: scopeIntentText()}, nil
	}
	f.perform(context.Background())
	if f.scopeCalls != 2 || f.completeCalls != 0 {
		t.Fatal("missing pending receipt")
	}
	if err := os.WriteFile(filepath.Join(f.record.WorkDir, "src/greeting.mjs"), []byte("unexpected private edit after stopping"), 0600); err != nil {
		t.Fatal(err)
	}
	f.lostScopeACK = false
	if err := f.r.recoverRecords(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.scopeCalls != 2 || f.completeCalls != 1 || f.artifactCalls != 0 {
		t.Fatal("restart submitted scope or snapshotted unauthorized changed workspace")
	}
	head, err := f.r.git.Head(context.Background(), f.record.WorkDir)
	if err != nil || head != f.base {
		t.Fatal("restart changed fixed base", err)
	}
}
