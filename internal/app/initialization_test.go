package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
	"coordica/internal/store"
)

type analysisSourceFake struct {
	SHA                  string
	RevisionErr, ReadErr error
	Revision             func(context.Context) (string, error)
}

func (f *analysisSourceFake) AnalysisRevision(ctx context.Context, _, _ string) (string, error) {
	if f.Revision != nil {
		return f.Revision(ctx)
	}
	return f.SHA, f.RevisionErr
}
func (f *analysisSourceFake) ReadAnalysis(context.Context, string, string) (gitops.AnalysisSnapshot, error) {
	return gitops.AnalysisSnapshot{SHA: f.SHA, Files: []gitops.SourceFile{{Path: "README.md", Text: "public readme"}, {Path: "src/main.go", Text: "package main"}}, Omissions: []string{"ignored private inputs excluded"}}, f.ReadErr
}
func initFixture(t *testing.T) (*Service, Actor, *analysisSourceFake) {
	t.Helper()
	return initFixtureChecks(t, []domain.Check{{Name: "check", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 300}})
}
func initFixtureChecks(t *testing.T, checks []domain.Check) (*Service, Actor, *analysisSourceFake) {
	t.Helper()
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, dir)
	owner := Actor{ID: "owner", Role: "owner"}
	if _, e = s.CreateDraftProject(context.Background(), owner, "project", domain.Project{Name: "Init", RepositoryURL: filepath.Join(dir, "source"), TargetBranch: "main", Checks: checks}); e != nil {
		t.Fatal(e)
	}
	source := &analysisSourceFake{SHA: strings.Repeat("a", 40)}
	s.Source = source
	return s, owner, source
}
func initResult(t *testing.T, s *Service, ready bool) domain.InitializationResult {
	t.Helper()
	v, _ := s.Initialization(context.Background(), Actor{ID: "owner", Role: "owner"})
	p, e := s.Project(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return domain.InitializationResult{Generation: v.OperationGeneration, InitializationVersion: v.Version, OperationID: v.OperationID, SourceSHA: v.SourceSHA, Ready: ready, InspectedPaths: []string{"README.md", "src/main.go"}, Facts: []domain.AnalysisEvidence{{Path: "src/main.go", Fact: "A source module exists"}}, Suggestions: []string{"Architecture suggestions require Owner review"}, Questions: []domain.InitializationQuestion{}, Baseline: domain.Baseline{Requirements: "Requirements", Acceptance: "Acceptance", Architecture: "Architecture", Modules: "src owns application", Conventions: "Small consumer boundaries", Testing: "Actual failure tests", Functionality: "Existing source", Risks: "Incomplete items require review", AllowedRoots: []string{"src"}, Checks: p.Checks, ProtectedPaths: p.ProtectedPaths}}
}
func initExecutor(result domain.InitializationResult) PlanningExecutor {
	return planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		raw, _ := json.Marshal(result)
		return PlanningOutcome{Output: string(raw), TreeStopped: true, ElapsedSeconds: 2}, nil
	})
}
func requestInit(t *testing.T, s *Service, a Actor, kind string) domain.Initialization {
	t.Helper()
	v, e := s.Initialization(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.RequestInitialization(context.Background(), a, ID("request"), domain.InitializationRequest{ExpectedVersion: v.Version, Kind: kind})
	return decodeTest[domain.Initialization](t, raw, e)
}
func analyzeInit(t *testing.T, s *Service, a Actor, result domain.InitializationResult) domain.Initialization {
	t.Helper()
	bound, _ := s.Initialization(context.Background(), a)
	result.Generation = bound.OperationGeneration
	result.InitializationVersion = bound.Version
	result.OperationID = bound.OperationID
	result.SourceSHA = bound.SourceSHA
	w := InitializationWorker{Service: s, Executor: initExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, e := s.Initialization(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestInitializationExistingSourceExactOwnerAndInputRaces(t *testing.T) {
	s, a, source := initFixture(t)
	v := requestInit(t, s, a, "existing")
	r := initResult(t, s, true)
	v = analyzeInit(t, s, a, r)
	if v.Status != "awaiting_owner" || len(v.CoveredPaths) != 2 || v.SourceSHA != source.SHA {
		t.Fatalf("analysis not ready: %+v", v)
	}
	for _, req := range []domain.InitializationConfirm{{ExpectedVersion: v.Version + 1, Digest: v.Digest}, {ExpectedVersion: v.Version, Digest: "wrong"}} {
		if _, e := s.ConfirmInitialization(context.Background(), a, ID("bad"), req); e == nil {
			t.Fatal("stale confirmation accepted")
		}
	}
	confirm := domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}
	if _, e := s.ConfirmInitialization(context.Background(), a, "confirm", confirm); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConfirmInitialization(context.Background(), a, "confirm", confirm); e != nil {
		t.Fatal("duplicate exact confirmation failed", e)
	}
	source.SHA = strings.Repeat("b", 40)
	if _, e := s.ConfirmInitialization(context.Background(), a, "confirm", confirm); e == nil {
		t.Fatal("changed main admitted through replay")
	}
	p, _ := s.Project(context.Background())
	if !p.Paused {
		t.Fatal("baseline confirmation unpaused project")
	}
	snap, _ := s.Snapshot(context.Background(), a)
	if len(snap.Tasks) != 0 || snap.PlanningDocument != nil || snap.Automation.Enabled {
		t.Fatal("analysis granted development authority")
	}
}
func TestInitializationQuietStopEvidenceAndLandedHistory(t *testing.T) {
	for _, state := range []string{"unknown_live", "running", "pending_result", "cancel_requested"} {
		t.Run(state, func(t *testing.T) {
			s, a, _ := initFixture(t)
			if e := s.DB.Update(context.Background(), func(tx *store.Tx) error {
				return tx.Put("planning_run", "old", domain.PlanningRun{ID: "old", State: state, Settled: state == "unknown_live", TreeStopped: false})
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.RequestInitialization(context.Background(), a, "init", domain.InitializationRequest{Kind: "existing"}); e == nil {
				t.Fatal("potentially live Leader admitted")
			}
		})
	}
	s, a, _ := initFixture(t)
	s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return tx.Put("candidate", "history", domain.Candidate{ID: "history", State: "landed"})
	})
	requestInit(t, s, a, "existing")
	var c domain.Candidate
	s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("candidate", "history", &c) })
	if c.State != "landed" {
		t.Fatal("landed history rewritten")
	}
}
func TestInitializationRejectsForgedNewAndCoveragePolicyDuplicates(t *testing.T) {
	s, a, _ := initFixture(t)
	if _, e := s.RequestInitialization(context.Background(), a, "forged", domain.InitializationRequest{Kind: "new"}); e == nil {
		t.Fatal("existing source bypassed")
	}
	r := initResult(t, s, true)
	v := domain.Initialization{Kind: "existing", AvailablePaths: []string{"README.md", "src/main.go"}}
	r.InspectedPaths = []string{"README.md"}
	raw, _ := json.Marshal(r)
	if _, e := decodeInitialization(string(raw), v); e == nil {
		t.Fatal("citation to uninspected source accepted")
	}
	r.InspectedPaths = []string{"README.md", "src/main.go"}
	r.Baseline.ProtectedPaths[1] = r.Baseline.ProtectedPaths[0]
	p, _ := s.Project(context.Background())
	if e := baselinePolicyAgreement(r.Baseline, p); e == nil {
		t.Fatal("duplicate protected path hides omission")
	}
	for _, text := range []string{`{}`, `{"ready":true}`, strings.Repeat("x", (128<<10)+1)} {
		if _, e := decodeInitialization(text, v); e == nil {
			t.Fatal("invalid structured result admitted")
		}
	}
}
func TestInitializationGitUnknownStopRetainsReservationAndOwnerProof(t *testing.T) {
	s, a, source := initFixture(t)
	source.ReadErr = process.ErrTreeNotStopped
	requestInit(t, s, a, "existing")
	w := InitializationWorker{Service: s, Executor: initExecutor(initResult(t, s, true))}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, _ := s.Initialization(context.Background(), a)
	p, _ := s.Project(context.Background())
	if v.OperationState != "unknown_live" || p.ReservedSeconds != 300 || p.UsedSeconds != 0 {
		t.Fatal("Git uncertainty released accounting", v, p)
	}
	if _, e := s.RequestInitialization(context.Background(), a, "retry", domain.InitializationRequest{ExpectedVersion: v.Version, Kind: "existing"}); e == nil {
		t.Fatal("unknown process reinitialized")
	}
	if _, e := s.ConfirmInitializationStopped(context.Background(), a, "bad-proof", domain.StopConfirmation{ExpectedVersion: v.OperationVersion}); e == nil {
		t.Fatal("missing stop proof accepted")
	}
	if _, e := s.ConfirmInitializationStopped(context.Background(), a, "proof", domain.StopConfirmation{ExpectedVersion: v.OperationVersion, Evidence: "Owner inspected exact host process tree and verified absent"}); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(context.Background())
	if p.ReservedSeconds != 0 || p.UsedSeconds != 300 {
		t.Fatal("conservative stop proof accounting lost")
	}
}
func TestInitializationNewInterviewFrontierOriginalAnswers(t *testing.T) {
	s, a, source := initFixture(t)
	source.RevisionErr = gitops.ErrUnborn
	source.SHA = ""
	requestInit(t, s, a, "new")
	r := initResult(t, s, false)
	r.InspectedPaths = []string{}
	r.Facts = []domain.AnalysisEvidence{}
	r.Questions = []domain.InitializationQuestion{{ID: "purpose", Text: "What should the product do?", Prerequisites: []string{}}}
	v := analyzeInit(t, s, a, r)
	if v.Status != "interview" {
		t.Fatal(v.Detail)
	}
	input := domain.InitializationAnswer{ExpectedVersion: v.Version, QuestionID: "purpose", OriginalText: "Exact original answer.", Source: domain.ChatSource{SessionID: "session", MessageID: "answer"}}
	raw, e := s.AnswerInitialization(context.Background(), a, "answer", input)
	v = decodeTest[domain.Initialization](t, raw, e)
	if v.Feedback[0].OriginalText != input.OriginalText || v.Feedback[0].Source.ClientID != a.ID {
		t.Fatal("answer provenance lost")
	}
	r.Questions = []domain.InitializationQuestion{{ID: "platform", Text: "Choose a platform", Prerequisites: []string{"purpose"}}}
	v = analyzeInit(t, s, a, r)
	if v.Status != "interview" || v.Version != 2 {
		t.Fatal("settled prerequisite frontier failed", v.Detail)
	}
	r.Generation = v.OperationGeneration
	r.InitializationVersion = v.Version
	r.OperationID = v.OperationID
	r.SourceSHA = v.SourceSHA
	r.Questions[0].Prerequisites = []string{"unanswered"}
	text, _ := json.Marshal(r)
	if _, e := decodeInitialization(string(text), v); e == nil {
		t.Fatal("unsettled frontier admitted")
	}
	r.Questions = []domain.InitializationQuestion{{ID: "purpose", Text: "Repeat", Prerequisites: []string{}}}
	text, _ = json.Marshal(r)
	if _, e := decodeInitialization(string(text), v); e == nil {
		t.Fatal("settled answer repeated")
	}
}
func TestInitializationIssuerRevocationReplayAndWorker(t *testing.T) {
	s, a, _ := initFixture(t)
	raw, e := s.CreateChatCredential(context.Background(), a, "chat", domain.ChatCredentialRequest{Name: "Center"})
	grant := decodeTest[domain.ChatCredentialGrant](t, raw, e)
	chat, e := s.Authenticate(context.Background(), grant.Token)
	if e != nil {
		t.Fatal(e)
	}
	request := domain.InitializationRequest{Kind: "existing"}
	if _, e = s.RequestInitialization(context.Background(), chat, "request", request); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RevokeChatCredential(context.Background(), a, "revoke", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestInitialization(context.Background(), chat, "request", request); e == nil {
		t.Fatal("revoked issuer replayed")
	}
	calls := 0
	w := InitializationWorker{Service: s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) { calls++; return PlanningOutcome{}, nil })}
	if e = w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal("revoked issuer launched Leader")
	}
	for _, actor := range []Actor{{ID: "node", Role: "node"}, {ID: "run", Role: "run"}, {ID: chat.ID, Role: "chat", ChatScope: "center"}} {
		if _, e = s.Initialization(context.Background(), actor); e == nil {
			t.Fatal("forged role reached initialization")
		}
		if _, e = s.ConfirmInitialization(context.Background(), actor, "confirm", domain.InitializationConfirm{}); e == nil {
			t.Fatal("normal role reached approval")
		}
	}
}
func TestInitializationRestartUnknownLiveAndReceiptFences(t *testing.T) {
	s, a, _ := initFixture(t)
	v := requestInit(t, s, a, "existing")
	s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var op domain.InitializationOperation
		if e := tx.Get("initialization_operation", v.OperationID, &op); e != nil {
			return e
		}
		op.State = "running"
		op.Attempt = 1
		op.Deadline = s.Now().Add(time.Minute)
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.ReservedSeconds = 300
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		return tx.Put("initialization_operation", op.ID, op)
	})
	w := InitializationWorker{Service: s}
	if e := w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	v, _ = s.Initialization(context.Background(), a)
	if v.OperationState != "unknown_live" {
		t.Fatal("restart inferred a stopped process")
	}
	old := initializationReceipt{OperationID: v.OperationID, Version: v.Version, Generation: 99, Outcome: PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2}}
	if e := w.apply(context.Background(), old); e != nil {
		t.Fatal(e)
	}
	p, _ := s.Project(context.Background())
	if p.ReservedSeconds != 300 {
		t.Fatal("old result generation released reservation")
	}
}
func TestInitializationAnalysisUsesScratchManifestAndFiniteReconciliation(t *testing.T) {
	s, a, source := initFixture(t)
	requestInit(t, s, a, "existing")
	r := initResult(t, s, true)
	calls := 0
	source.Revision = func(ctx context.Context) (string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("reconciliation lacks finite deadline")
		}
		return source.SHA, nil
	}
	w := InitializationWorker{Service: s, Executor: planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
		if strings.Contains(prompt, "public readme") || strings.Contains(prompt, "package main") {
			t.Error("raw source flooded prompt")
		}
		if raw, e := os.ReadFile(filepath.Join(dir, "source", "src", "main.go")); e != nil || string(raw) != "package main" {
			t.Error("fixed source unavailable")
		}
		raw, _ := json.Marshal(r)
		return PlanningOutcome{TreeStopped: true, Output: string(raw), ElapsedSeconds: 1}, nil
	})}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("missing bounded input reconciliation")
	}
}
func TestMandatoryBaselineEveryAdmissionAndLateResult(t *testing.T) {
	f := newServiceFixture(t)
	a := f.claim(t, "run")
	ctx := context.Background()
	f.s.DB.Update(ctx, func(tx *store.Tx) error {
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		v.Status = "interview"
		return tx.Put("initialization", "current", v)
	})
	if _, e := f.s.Claim(ctx, f.node, "run", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}, a.Run.Base); e == nil {
		t.Fatal("cached claim bypassed initialization")
	}
	if _, e := f.s.TaskAction(ctx, f.owner, "approve", f.task.ID, "approve", domain.VersionRequest{ExpectedVersion: a.Task.Version}); e == nil {
		t.Fatal("task approval bypassed baseline")
	}
	if _, e := f.s.RunContext(ctx, f.node, a.Run.ID); e == nil {
		t.Fatal("context exposed stale baseline")
	}
	if _, e := f.s.InputRun(ctx, f.node, a.Run.ID); e == nil {
		t.Fatal("input admitted stale baseline")
	}
	hb, e := f.s.Heartbeat(ctx, f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation, ElapsedSeconds: 1})
	if e != nil || !hb.Cancel {
		t.Fatal("heartbeat did not stop stale baseline", e)
	}
	if _, e = f.s.Complete(ctx, f.node, "complete", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, ElapsedSeconds: 2, Quiescent: true, Status: "completed", Head: strings.Repeat("b", 40)}); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.Candidates) != 0 || snap.Project.ReservedSeconds != 0 {
		t.Fatal("stale completion admitted or lost settlement")
	}
}

func TestAuthorityPolicyMigrationPreservesHistoryAndDrainsOldGrant(t *testing.T) {
	f := newServiceFixture(t)
	run := f.claim(t, "legacy-running")
	ctx := context.Background()
	before := f.snapshot(t)
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.ProtectedPaths = []string{"AGENTS.md"}
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		return tx.Delete("authority_policy_migration", "current")
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.MigrateAuthorityPolicy(ctx); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if !after.Project.Paused || after.Project.RepositoryURL != before.Project.RepositoryURL || after.Project.Name != before.Project.Name || after.Project.UsedSeconds != before.Project.UsedSeconds || after.Project.ReservedSeconds != before.Project.ReservedSeconds || after.Project.PolicyVersion != before.Project.PolicyVersion+1 {
		t.Fatal("migration changed identity/accounting or omitted version")
	}
	if after.Runs[0].ID != run.Run.ID || after.Runs[0].Quiescent || after.Runs[0].State != "cancel_requested" || after.Tasks[0].ApprovedVersion != 0 || after.Initialization.Status != "draft" {
		t.Fatal("migration grandfathered grant or fabricated stop")
	}
	for _, path := range []string{"internal/mcp/**", "internal/chatprofile/**", "internal/mcpconfig/**", "internal/centerhost/**"} {
		found := false
		for _, protected := range after.Project.ProtectedPaths {
			if path == protected {
				found = true
			}
		}
		if !found {
			t.Fatal("authority path not protected", path)
		}
	}
	version := after.Project.PolicyVersion
	events := len(after.Events)
	if e := f.s.MigrateAuthorityPolicy(ctx); e != nil {
		t.Fatal(e)
	}
	again := f.snapshot(t)
	if again.Project.PolicyVersion != version || len(again.Events) != events {
		t.Fatal("migration not idempotent")
	}
}

func TestInitializationCancelledLiveOperationNeedsStopReceipt(t *testing.T) {
	s, a, _ := initFixture(t)
	v := requestInit(t, s, a, "existing")
	started := make(chan struct{})
	done := make(chan error, 1)
	w := InitializationWorker{Service: s, Executor: planningExecFunc(func(ctx context.Context, _, _ string) (PlanningOutcome, error) {
		close(started)
		<-ctx.Done()
		return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 3}, ctx.Err()
	})}
	go func() { done <- w.Tick(context.Background()) }()
	<-started
	if _, e := s.CancelInitialization(context.Background(), a, "cancel", domain.VersionRequest{ExpectedVersion: v.Version}); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("committed cancellation did not stop bounded operation")
	}
	current, _ := s.Initialization(context.Background(), a)
	p, _ := s.Project(context.Background())
	if current.OperationState == "queued" || p.ReservedSeconds != 0 || p.UsedSeconds < 3 || current.Status != "draft" {
		t.Fatal("cancel retried or erased actual spending")
	}
}

func TestInitializationModelNonceGenerationAndPolicyRaces(t *testing.T) {
	s, a, _ := initFixture(t)
	v := requestInit(t, s, a, "existing")
	result := initResult(t, s, true)
	result.Generation++
	failed := analyzeInit(t, s, a, result) // helper supplies normal operation identity
	if failed.Status != "awaiting_owner" {
		t.Fatal("valid result fixture failed", failed.Detail)
	}
	// Validate the model output independently, with each actual stale binding.
	result = initResult(t, s, true)
	for _, change := range []func(*domain.InitializationResult){func(r *domain.InitializationResult) { r.Generation++ }, func(r *domain.InitializationResult) { r.InitializationVersion++ }, func(r *domain.InitializationResult) { r.OperationID = "old-operation" }, func(r *domain.InitializationResult) { r.SourceSHA = strings.Repeat("b", 40) }} {
		r := result
		change(&r)
		raw, _ := json.Marshal(r)
		if _, e := decodeInitialization(string(raw), failed); e == nil {
			t.Fatal("replayed model binding admitted")
		}
	}
	if e := s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.PolicyVersion++
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ConfirmInitialization(context.Background(), a, "stale-policy", domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: failed.Digest}); e == nil {
		t.Fatal("changed policy confirmed")
	}
}

func TestMandatoryBaselineFencesCandidatePrepareValidateLandAndApproval(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	candidate := domain.Candidate{ID: "candidate", Version: 1, TaskID: f.task.ID, BaselineVersion: f.task.BaselineVersion, BaselineDigest: f.task.BaselineDigest, AuthorizationVersion: f.task.AuthorizationVersion, PolicyVersion: 1, State: "ready_to_land", Commit: strings.Repeat("c", 40), Main: strings.Repeat("a", 40)}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		task := f.task
		task.State = "awaiting_integration"
		task.CurrentCandidateID = candidate.ID
		if e := tx.Put("task", task.ID, task); e != nil {
			return e
		}
		if e := tx.Put("candidate", candidate.ID, candidate); e != nil {
			return e
		}
		return tx.Delete("initialization", "current")
	}); e != nil {
		t.Fatal(e)
	}
	w := Integrator{Service: f.s, Git: nil} // any Git access before rejection would panic
	if ok, e := w.eligible(ctx, candidate); e != nil || ok {
		t.Fatal("legacy candidate eligible", ok, e)
	}
	for _, call := range []func() error{func() error { return w.prepare(ctx, candidate) }, func() error { return w.validate(ctx, candidate) }, func() error { return w.land(ctx, candidate) }} {
		if e := call(); e == nil {
			t.Fatal("candidate path bypassed mandatory initialization")
		}
	}
	if _, e := f.s.ApproveCandidate(ctx, f.owner, "approve", candidate.ID, domain.VersionRequest{ExpectedVersion: 1}); e == nil {
		t.Fatal("protected approval bypassed initialization")
	}
	if len(f.snapshot(t).Runs) != 0 {
		t.Fatal("candidate gate dispatched work")
	}
}

func TestBlankNewDraftInterviewRequiresOwnerActiveChecks(t *testing.T) {
	s, a, source := initFixtureChecks(t, []domain.Check{})
	source.RevisionErr = gitops.ErrUnborn
	source.SHA = ""
	p, _ := s.Project(context.Background())
	if !p.Paused || len(p.Checks) != 0 {
		t.Fatal("blank draft acquired fabricated policy", p)
	}
	requestInit(t, s, a, "new")
	r := initResult(t, s, false)
	r.InspectedPaths = []string{}
	r.Facts = []domain.AnalysisEvidence{}
	r.Questions = []domain.InitializationQuestion{{ID: "purpose", Text: "What should the project do?", Prerequisites: []string{}}}
	v := analyzeInit(t, s, a, r)
	if v.Status != "interview" {
		t.Fatal("blank new draft cannot interview", v)
	}
	if _, e := s.ConfirmInitialization(context.Background(), a, "blank-confirm", domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}); e == nil {
		t.Fatal("empty active checks approved")
	}
	if _, e := s.ProjectAction(context.Background(), a, "blank-resume", "resume", domain.VersionRequest{ExpectedVersion: p.Version}); e == nil {
		t.Fatal("empty draft resumed")
	}
	raw, e := s.AnswerInitialization(context.Background(), a, "answer-purpose", domain.InitializationAnswer{ExpectedVersion: v.Version, QuestionID: "purpose", OriginalText: "A small Go tool", Source: domain.ChatSource{SessionID: "owner", MessageID: "purpose"}})
	if e != nil {
		t.Fatal(e)
	}
	_ = raw
	r = initResult(t, s, false)
	r.InspectedPaths = []string{}
	r.Facts = []domain.AnalysisEvidence{}
	r.Questions = []domain.InitializationQuestion{}
	v = analyzeInit(t, s, a, r)
	p, _ = s.Project(context.Background())
	checks := []domain.Check{{Name: "actual Go tests", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 600}}
	if _, e = s.UpdateProjectPolicy(context.Background(), a, "owner-checks", domain.ProjectPolicyRequest{ExpectedVersion: p.Version, Checks: checks, ProtectedPaths: p.ProtectedPaths}); e != nil {
		t.Fatal(e)
	}
	requestInit(t, s, a, "new")
	r = initResult(t, s, true)
	r.InspectedPaths = []string{}
	r.Facts = []domain.AnalysisEvidence{}
	v = analyzeInit(t, s, a, r)
	if v.Status != "awaiting_owner" {
		t.Fatal("actual 600-second policy not admissible", v)
	}
	if _, e = s.ConfirmInitialization(context.Background(), a, "actual-confirm", domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}); e != nil {
		t.Fatal(e)
	}
	p, _ = s.Project(context.Background())
	if !p.Paused {
		t.Fatal("initialization unpaused")
	}
}

func TestInitializationCapturesFiniteOwnerTimeoutAndRetriesWithAutomationOff(t *testing.T) {
	s, a, _ := initFixture(t)
	ctx := context.Background()
	settings := domain.DefaultAutomationSettings()
	settings.PlanningTimeoutSeconds = 1800
	settings.PlanningBudgetSeconds = 1800
	settings.PlanningMaxRetries = 0
	if _, e := s.ConfigureAutomation(ctx, a, "finite-init", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
		t.Fatal(e)
	}
	v := requestInit(t, s, a, "existing")
	op, e := s.InitializationOperation(ctx, a)
	if e != nil || op.GrantSeconds != 1800 || op.MaxRetries != 0 {
		t.Fatal(op, e)
	}
	w := InitializationWorker{Service: s, Executor: planningExecFunc(func(ctx context.Context, _, _ string) (PlanningOutcome, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 1700*time.Second || time.Until(deadline) > 1801*time.Second {
			t.Fatal("configured finite deadline absent", deadline)
		}
		return PlanningOutcome{TreeStopped: true, Error: "transient fixture failure", ElapsedSeconds: 2}, nil
	})}
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	op, e = s.InitializationOperation(ctx, a)
	if e != nil || op.Attempt != 1 || op.State == "queued" || !op.Settled {
		t.Fatal("zero retry ignored", op, e)
	}
	current, _ := s.Initialization(ctx, a)
	if current.OperationID != v.OperationID {
		t.Fatal("operation identity changed")
	}
}

func TestLimitedInitializationProjectionCoversReadsWritesAndAllReplays(t *testing.T) {
	s, owner, _ := initFixtureChecks(t, []domain.Check{{Name: "Go checks", Command: "/Users/alice/private/go", Args: []string{"own_private_owner", "/workspace/private"}, TimeoutSeconds: 600}})
	ctx := context.Background()
	requestInit(t, s, owner, "existing")
	r := initResult(t, s, false)
	r.Baseline.Architecture = "Keep own_private_owner node_private_node at /Users/alice/private/source"
	r.Facts[0].Fact = r.Baseline.Architecture
	r.Suggestions = []string{r.Baseline.Architecture}
	r.Questions = []domain.InitializationQuestion{{ID: "purpose", Text: r.Baseline.Architecture, Prerequisites: []string{}}}
	v := analyzeInit(t, s, owner, r)
	raw, e := s.CreateChatCredential(ctx, owner, "limited-init", domain.ChatCredentialRequest{Name: "limited"})
	grant := decodeTest[domain.ChatCredentialGrant](t, raw, e)
	chat, e := s.Authenticate(ctx, grant.Token)
	if e != nil {
		t.Fatal(e)
	}
	assertSafe := func(value any) {
		t.Helper()
		b, _ := json.Marshal(value)
		for _, bad := range []string{"own_private_owner", "node_private_node", "/Users/alice", "/workspace/private"} {
			if strings.Contains(string(b), bad) {
				t.Fatal("private initialization leaked", bad, string(b))
			}
		}
		if !strings.Contains(string(b), "command redacted") {
			t.Fatal("checks not safely projected", string(b))
		}
	}
	limited, e := s.Initialization(ctx, chat)
	if e != nil {
		t.Fatal(e)
	}
	assertSafe(limited)
	req := domain.InitializationAnswer{ExpectedVersion: v.Version, QuestionID: "purpose", OriginalText: "own_private_owner node_private_node /Users/alice/private/source /workspace/private", Source: domain.ChatSource{SessionID: "normal", MessageID: "original"}}
	first, e := s.AnswerInitialization(ctx, chat, "original", req)
	if e != nil {
		t.Fatal(e)
	}
	assertSafe(first)
	repeat, e := s.AnswerInitialization(ctx, chat, "original", req)
	if e != nil || string(first) != string(repeat) {
		t.Fatal("key replay projection differs", e)
	}
	assertSafe(repeat)
	repeat, e = s.AnswerInitialization(ctx, chat, "source-replay", req)
	if e != nil || string(first) != string(repeat) {
		t.Fatal("source replay projection differs", e)
	}
	assertSafe(repeat)
	private, e := s.Initialization(ctx, owner)
	if e != nil || len(private.Feedback) != 1 || private.Feedback[0].OriginalText != req.OriginalText || private.Result.Baseline.Checks[0].Command != "/Users/alice/private/go" {
		t.Fatal("original evidence was rewritten", private, e)
	}
	// The queued operation can be safely cancelled, then a fresh chat request
	// and its durable replay must also redact retained original feedback.
	if _, e = s.CancelInitialization(ctx, owner, "cancel", domain.VersionRequest{ExpectedVersion: private.Version}); e != nil {
		t.Fatal(e)
	}
	reqOpen := domain.InitializationRequest{ExpectedVersion: private.Version, Kind: "existing"}
	first, e = s.RequestInitialization(ctx, chat, "open", reqOpen)
	if e != nil {
		t.Fatal(e)
	}
	repeat, e = s.RequestInitialization(ctx, chat, "open", reqOpen)
	if e != nil || string(first) != string(repeat) {
		t.Fatal("request replay differs", e)
	}
	for _, b := range []json.RawMessage{first, repeat} {
		if strings.Contains(string(b), "own_private_owner") || strings.Contains(string(b), "/Users/alice") {
			t.Fatal("request retained feedback leaked", string(b))
		}
	}
}

type pinnedAnalysisSource struct {
	analysisSourceFake
	Want  string
	Calls int
}

func (s *pinnedAnalysisSource) AnalysisRevision(ctx context.Context, repo, branch string) (string, error) {
	s.Calls++
	if repo != s.Want {
		return "", domain.Fail("wrong_source", "target is not analysis input")
	}
	return s.analysisSourceFake.AnalysisRevision(ctx, repo, branch)
}
func (s *pinnedAnalysisSource) ReadAnalysis(ctx context.Context, repo, sha string) (gitops.AnalysisSnapshot, error) {
	if repo != s.Want {
		return gitops.AnalysisSnapshot{}, domain.Fail("wrong_source", "worker input differs")
	}
	return s.analysisSourceFake.ReadAnalysis(ctx, repo, sha)
}
func TestRequestWorkerAndConfirmationUseFixedLocalSourceNotHTTPSTarget(t *testing.T) {
	s, a, source := initFixture(t)
	ctx := context.Background()
	local := filepath.Join(s.Dir, "source")
	if e := s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.RepositoryURL = "https://example.invalid/target.git"
		p.SourceRepository = local
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	pinned := &pinnedAnalysisSource{analysisSourceFake: *source, Want: local}
	s.Source = pinned
	requestInit(t, s, a, "existing")
	v := analyzeInit(t, s, a, initResult(t, s, true))
	if v.Status != "awaiting_owner" {
		t.Fatal(v)
	}
	if _, e := s.ConfirmInitialization(ctx, a, "confirm-local", domain.InitializationConfirm{ExpectedVersion: v.Version, Digest: v.Digest}); e != nil {
		t.Fatal(e)
	}
	if pinned.Calls < 3 {
		t.Fatal("source not checked across all entry points", pinned.Calls)
	}
}

// Mirror the native acceptance fixture's repository entries, which differ from
// their physical scratch locations. Snapshot metadata is not source evidence.
type nativeEvidenceSource struct{ analysisSourceFake }

func (s *nativeEvidenceSource) ReadAnalysis(context.Context, string, string) (gitops.AnalysisSnapshot, error) {
	return gitops.AnalysisSnapshot{SHA: s.SHA, Files: []gitops.SourceFile{
		{Path: "README.md", Text: "Greeting project"},
		{Path: "src/greet.mjs", Text: "export const greet = () => 'hello';"},
		{Path: "test/greet.test.mjs", Text: "// public greeting test"},
	}, Omissions: []string{}}, nil
}

func TestInitializationExecutorDistinguishesPhysicalPathsFromEvidenceIdentifiers(t *testing.T) {
	for _, kind := range []string{"repository_entries", "physical_inspected", "metadata_inspected", "physical_fact", "metadata_fact", "glob_root"} {
		t.Run(kind, func(t *testing.T) {
			s, a, source := initFixture(t)
			s.Source = &nativeEvidenceSource{analysisSourceFake: *source}
			requestInit(t, s, a, "existing")
			result := initResult(t, s, true)
			entries := []string{"README.md", "src/greet.mjs", "test/greet.test.mjs"}
			result.InspectedPaths = append([]string{}, entries...)
			result.Facts = []domain.AnalysisEvidence{{Path: "README.md", Fact: "Public project description"}, {Path: "src/greet.mjs", Fact: "Exports greeting"}, {Path: "test/greet.test.mjs", Fact: "Greeting test module"}}
			switch kind {
			case "physical_inspected":
				result.InspectedPaths = []string{"source/README.md", "source/src/greet.mjs", "source/test/greet.test.mjs"}
			case "metadata_inspected":
				result.InspectedPaths = append(result.InspectedPaths, "source-manifest.json")
			case "physical_fact":
				result.Facts[0].Path = "source/README.md"
			case "metadata_fact":
				result.Facts[0].Path = "source-manifest.json"
			case "glob_root":
				result.Baseline.AllowedRoots = []string{"src/**", "test/**"}
			}
			called := false
			worker := InitializationWorker{Service: s, Executor: planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
				called = true
				if !strings.Contains(prompt, "baseline.allowed_roots contains literal repository-relative") {
					t.Fatal("executor prompt omitted the distinction between root directories and task globs")
				}
				for _, instruction := range []string{"source-manifest.json as snapshot metadata only", "Open its physical scratch location source/<manifest-entry>", "inspected_paths and every facts.path report ONLY the exact repository-relative manifest entry", "Never prepend the scratch source/ prefix", "include the metadata file source-manifest.json in inspected_paths or facts.path", "open source/README.md but report README.md", "open source/src/greet.mjs but report src/greet.mjs", "open source/test/greet.test.mjs but report test/greet.test.mjs"} {
					if !strings.Contains(prompt, instruction) {
						t.Error("executor prompt lacks evidence path protocol", instruction)
					}
				}
				if _, e := os.ReadFile(filepath.Join(dir, "source-manifest.json")); e != nil {
					t.Fatal("metadata missing", e)
				}
				for _, entry := range entries {
					if _, e := os.ReadFile(filepath.Join(dir, "source", filepath.FromSlash(entry))); e != nil {
						t.Fatal("physical source unavailable", entry, e)
					}
				}
				raw, _ := json.Marshal(result)
				return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 1, Output: string(raw)}, nil
			})}
			if e := worker.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			v, e := s.Initialization(context.Background(), a)
			if e != nil || !called {
				t.Fatal(e, called)
			}
			if kind == "repository_entries" {
				if v.Status != "awaiting_owner" || v.Result == nil || len(v.CoveredPaths) != len(entries) {
					t.Fatal("exact entries rejected", v)
				}
				for i, entry := range entries {
					if v.CoveredPaths[i] != entry || v.Result.Facts[i].Path != entry {
						t.Fatal("evidence identifiers changed", v)
					}
				}
			} else if v.Status != "draft" || v.Result != nil || v.Digest != "" || v.ApprovedVersion != 0 {
				t.Fatal("physical or metadata evidence admitted/normalized", kind, v)
			}
		})
	}
}
