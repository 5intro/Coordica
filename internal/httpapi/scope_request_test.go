package httpapi

import (
	"bytes"
	"context"
	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/mcp"
	"coordica/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type recorderScopeCaller struct {
	handler http.Handler
	token   string
}

func (c recorderScopeCaller) Do(ctx context.Context, method, path, key string, input any) (json.RawMessage, error) {
	b, _ := json.Marshal(input)
	r := httptest.NewRequest(method, path, bytes.NewReader(b)).WithContext(ctx)
	r.Host = "127.0.0.1:8080"
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", w.Code, w.Body.String())
	}
	return json.RawMessage(w.Body.Bytes()), nil
}

type scopeTransportExecutor func() (domain.LeaderResult, error)

func (f scopeTransportExecutor) Execute(context.Context, string, string, func(int) error) (app.PlanningOutcome, error) {
	v, e := f()
	b, _ := json.Marshal(v)
	return app.PlanningOutcome{TreeStopped: true, Output: string(b), ElapsedSeconds: 1}, e
}
func TestMCPHTTPApplicationScopeTriggerAndReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := app.New(db, dir)
	owner := app.Actor{ID: "owner", Role: "owner"}
	if _, e = s.CreateProject(ctx, owner, "transport-project", domain.Project{Name: "Scope", RepositoryURL: dir, TargetBranch: "main", TotalBudgetSeconds: 10000, Checks: []domain.Check{{Name: "check", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}}}); e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateRequirement(ctx, owner, "transport-req", domain.Requirement{Title: "Source", Body: "Reuse existing shared helper"})
	if e != nil {
		t.Fatal(e)
	}
	var req domain.Requirement
	if e = json.Unmarshal(b, &req); e != nil {
		t.Fatal(e)
	}
	b, e = s.SavePlanning(ctx, owner, "transport-goal", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "Build source", NonGoals: "No other products", Acceptance: "Tests pass", RequirementIDs: []string{req.ID}, Architecture: "Go", DirectoryOutline: "src/: source", AllowedRoots: []string{"src"}, BudgetSeconds: 1000}})
	if e != nil {
		t.Fatal(e)
	}
	var d domain.PlanningDocument
	if e = json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizePlanning(ctx, owner, "transport-auth", domain.VersionRequest{ExpectedVersion: d.Version}); e != nil {
		t.Fatal(e)
	}
	settings := domain.DefaultAutomationSettings()
	settings.Enabled = true
	if _, e = s.ConfigureAutomation(ctx, owner, "transport-auto", domain.AutomationRequest{ExpectedVersion: 1, Settings: settings}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SchedulePlanning(ctx, owner, "transport-plan", domain.PlanningMessageRequest{ExpectedVersion: d.Version}); e != nil {
		t.Fatal(e)
	}
	result := domain.LeaderResult{Reply: "Ready", Ready: true, MissingItems: []string{}, Clarifications: []string{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}, Tasks: []domain.TaskProposal{{Key: "source", Title: "Source", Objective: "Use shared helper", Acceptance: "Tests pass", RequirementIDs: []string{req.ID}, Paths: []string{"src/old/**"}, Dependencies: []string{}, HotResources: []string{}, BudgetSeconds: 100, MaxRunSeconds: 30, MaxRetries: 2, MaxRepairs: 1}}}
	worker := &app.PlanningWorker{Service: s, Executor: scopeTransportExecutor(func() (domain.LeaderResult, error) { return result, nil })}
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	b, e = s.CreatePairing(ctx, owner, "transport-pair", "Runner")
	if e != nil {
		t.Fatal(e)
	}
	var pair map[string]string
	if e = json.Unmarshal(b, &pair); e != nil {
		t.Fatal(e)
	}
	registered, e := s.Register(ctx, domain.RegisterRequest{Code: pair["code"], Name: "Runner", Platform: "test", Capacity: 1})
	if e != nil {
		t.Fatal(e)
	}
	node, e := s.Authenticate(ctx, registered.Token)
	if e != nil {
		t.Fatal(e)
	}
	b, e = s.Claim(ctx, node, "transport-claim", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}, strings.Repeat("a", 40))
	if e != nil {
		t.Fatal(e)
	}
	var assignment domain.Assignment
	if e = json.Unmarshal(b, &assignment); e != nil || assignment.Run.ID == "" {
		t.Fatal("missing assignment", e)
	}
	caller := recorderScopeCaller{handler: (&Server{Service: s}).Handler(), token: registered.Token}
	protocol := mcp.Server{API: caller, RunID: assignment.Run.ID, Generation: assignment.Run.Generation}
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"request_scope","arguments":{"reason":"Existing requirement needs shared helper","paths":["src/shared/**"],"idempotency_key":"transport-scope"}}}`
	var out bytes.Buffer
	if e = protocol.Serve(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`+"\n"+call+"\n"+call), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), `"isError":true`) {
		t.Fatal(out.String())
	}
	snap, e := s.Snapshot(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.ScopeRequests) != 1 || snap.Runs[0].State != "cancel_requested" || snap.Tasks[0].HoldReason != "scope_requested" {
		t.Fatal("MCP did not reach atomic application scope request")
	}
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	snap, e = s.Snapshot(ctx, owner)
	if e != nil || len(snap.PlanningRuns) != 1 {
		t.Fatal("assessed before actual stop", e)
	}
	if _, e = s.Complete(ctx, node, "transport-stop", assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "stopped", Quiescent: true, ElapsedSeconds: 1}); e != nil {
		t.Fatal(e)
	}
	worker.Executor = scopeTransportExecutor(func() (domain.LeaderResult, error) {
		snap, e := s.Snapshot(ctx, owner)
		if e != nil {
			return domain.LeaderResult{}, e
		}
		task := snap.Tasks[0]
		result.Tasks = []domain.TaskProposal{}
		result.Scopes = []domain.ScopeProposal{{TaskID: task.ID, ExpectedVersion: task.Version, Paths: []string{"src/old/**", "src/shared/**"}, Reason: "Reuse helper"}}
		return result, nil
	})
	if e = worker.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	snap, e = s.Snapshot(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if snap.ScopeRequests[0].State != "consumed" || snap.Tasks[0].State != "ready" || snap.Tasks[0].HoldReason != "" {
		t.Fatal("MCP scope request never automatically progressed", snap.Decisions)
	}
}
