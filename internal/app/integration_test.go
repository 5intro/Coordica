package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

type itFixture struct {
	s           *Service
	w           *Integrator
	g           *gitops.Client
	owner, node Actor
	remote      string
	b           gitops.OID
}

func itDecode[T any](t *testing.T, data []byte, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// This test-only helper seeds a brand-new local bare repository. It never
// authenticates, invokes a shell, or addresses a user's repository.
func itGit(t *testing.T, binary, repo string, args ...string) string {
	t.Helper()
	argv := []string{"--no-pager", "-c", "core.hooksPath=" + os.DevNull, "-c", "protocol.file.allow=always", "-C", repo}
	cmd := exec.CommandContext(context.Background(), binary, append(argv, args...)...)
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if !strings.HasPrefix(key, "GIT_") && key != "GH_TOKEN" && key != "GITHUB_TOKEN" && key != "SSH_AUTH_SOCK" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("temporary Git operation failed: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func newITFixture(t *testing.T) *itFixture {
	t.Helper()
	ctx := context.Background()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "center", "coordica.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &itFixture{s: New(db, filepath.Join(dir, "center")), g: gitops.New(binary), owner: Actor{ID: "owner", Role: "owner"}, remote: filepath.Join(dir, "remote.git")}
	f.w = &Integrator{Service: f.s, Git: f.g}
	seed := filepath.Join(dir, "seed")
	if err = f.g.Init(ctx, seed, false); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(seed, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seed, "src", "base.txt"), []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.b, err = f.g.CommitAll(ctx, seed, "baseline", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.g.Init(ctx, f.remote, true); err != nil {
		t.Fatal(err)
	}
	itGit(t, binary, seed, "push", "--", f.remote, string(f.b)+":refs/heads/main")
	_, err = f.s.CreateProject(ctx, f.owner, "project", domain.Project{Name: "Integration", RepositoryURL: f.remote, TargetBranch: "main", TotalBudgetSeconds: 36000, Checks: []domain.Check{{Name: "clean_git", Command: binary, Args: []string{"status", "--porcelain"}, TimeoutSeconds: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	seedReadyBaseline(t, f.s)
	data, err := f.s.CreateTask(ctx, f.owner, "task", domain.Task{Title: "Implement feature", Description: "Add source file", Acceptance: "The fixed candidate adds the source file", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 14400, MaxRunSeconds: 3600, MaxRepairs: 2, MaxRetries: 2})
	task := itDecode[domain.Task](t, data, err)
	if _, err = f.s.TaskAction(ctx, f.owner, "approve", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version}); err != nil {
		t.Fatal(err)
	}
	data, err = f.s.CreatePairing(ctx, f.owner, "pair", "Local runner")
	pairing := itDecode[map[string]string](t, data, err)
	registration, err := f.s.Register(ctx, domain.RegisterRequest{Code: pairing["code"], Name: "Local runner", Platform: "test", Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	f.node, err = f.s.Authenticate(ctx, registration.Token)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *itFixture) snapshot(t *testing.T) domain.Snapshot {
	t.Helper()
	snapshot, err := f.s.Snapshot(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f *itFixture) candidate(t *testing.T) domain.Candidate {
	t.Helper()
	snapshot := f.snapshot(t)
	if len(snapshot.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %+v", snapshot.Candidates)
	}
	return snapshot.Candidates[0]
}

func (f *itFixture) claim(t *testing.T, kind string) domain.Assignment {
	t.Helper()
	base, err := f.w.Base(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.s.Claim(context.Background(), f.node, ID("claim_"), domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}, {Tool: "claude", Available: true, Structured: true, Cancel: true, Review: true}}}, base)
	assignment := itDecode[*domain.Assignment](t, data, err)
	if assignment == nil || assignment.Run.Kind != kind {
		t.Fatalf("expected %s assignment: %+v", kind, assignment)
	}
	return *assignment
}

func (f *itFixture) implement(t *testing.T, path string) domain.Assignment {
	t.Helper()
	ctx := context.Background()
	assignment := f.claim(t, "implement")
	bundle, err := f.w.InputBundle(ctx, assignment.Run)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "implementation")
	if err = f.g.CloneSnapshot(ctx, bundle, gitops.OID(assignment.Run.Base), workspace); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(filepath.Join(workspace, path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, path), []byte("implemented\n"), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := f.g.CommitAll(ctx, workspace, "implement feature", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	materialPath := filepath.Join(t.TempDir(), "implementation.bundle")
	if _, err = f.g.CreateBundle(ctx, workspace, gitops.OID(assignment.Run.Base), head, materialPath); err != nil {
		t.Fatal(err)
	}
	material, err := os.Open(materialPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := f.s.UploadArtifact(ctx, f.node, assignment.Run.ID, material)
	material.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Complete(ctx, f.node, ID("complete_"), assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "succeeded", Head: string(head), ArtifactID: artifact.ID, SessionID: "implementation-session", Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if candidate := f.candidate(t); candidate.State != "accepted" || candidate.Base != assignment.Run.Base || candidate.Head != string(head) {
		t.Fatalf("handoff did not bind B/H: %+v", candidate)
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.candidate(t).State == "prechecking" {
		if err = f.w.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return assignment
}

func (f *itFixture) reviewAndValidate(t *testing.T) domain.Assignment {
	t.Helper()
	ctx := context.Background()
	candidate := f.candidate(t)
	if candidate.State != "reviewing" || candidate.Commit == "" {
		t.Fatalf("candidate not fixed for review: %+v", candidate)
	}
	assignment := f.claim(t, "review")
	if assignment.Run.Base != candidate.Commit || assignment.Candidate == nil || assignment.Run.ID == candidate.SourceRunID || assignment.Run.Tool != "claude" {
		t.Fatalf("review assignment is not independently bound to C: %+v", assignment)
	}
	bundle, err := f.w.InputBundle(ctx, assignment.Run)
	if err != nil {
		t.Fatal(err)
	}
	reviewWorkspace := filepath.Join(t.TempDir(), "review")
	if err = f.g.CloneSnapshot(ctx, bundle, gitops.OID(candidate.Commit), reviewWorkspace); err != nil {
		t.Fatal("review must receive actual fixed candidate", err)
	}
	patch, err := f.g.DiffPatch(ctx, reviewWorkspace, gitops.OID(candidate.Main), gitops.OID(candidate.Commit))
	if err != nil || !strings.Contains(string(patch), "src/feature.txt") {
		t.Fatalf("review cannot see complete M..C patch: %s %v", patch, err)
	}
	_, err = f.s.Complete(ctx, f.node, ID("review_"), assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "succeeded", SessionID: ID("review-session_"), Quiescent: true, Review: &domain.Review{Candidate: candidate.Commit, Verdict: "approve", Summary: "Fixed candidate accepted", Findings: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	validated := f.candidate(t)
	if validated.State != "ready_to_land" || validated.Commit != candidate.Commit || validated.Review == nil || validated.Review.RunID != assignment.Run.ID || len(validated.Checks) != 1 || !validated.Checks[0].Passed {
		t.Fatalf("fixed C review/check gate failed: %+v", validated)
	}
	return assignment
}

func TestIntegrationRealGitHandoffReviewCheckAndExactLanding(t *testing.T) {
	f := newITFixture(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.TotalBudgetSeconds = 1
		p.UsedSeconds = 1000000
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	implementation := f.implement(t, "src/feature.txt")
	review := f.reviewAndValidate(t)
	candidate := f.candidate(t)
	if err := f.w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
	if err != nil || string(remote) != candidate.Commit {
		t.Fatalf("remote did not receive original C: %s %v", remote, err)
	}
	snapshot := f.snapshot(t)
	if snapshot.Candidates[0].State != "landed" || snapshot.Tasks[0].State != "integrated" || snapshot.Project.ReservedSeconds != 0 || snapshot.Project.UsedSeconds <= 1000000 {
		t.Fatalf("integration not reconciled: %+v", snapshot)
	}
	parents := strings.Fields(itGit(t, f.g.GitPath, f.w.repo(), "rev-list", "--parents", "-n", "1", candidate.Commit))
	if len(parents) != 3 || parents[1] != candidate.Main || parents[2] != candidate.Head {
		t.Fatalf("candidate history changed: %v", parents)
	}
	if implementation.Run.ID == review.Run.ID || review.Run.Base != candidate.Commit {
		t.Fatal("review evidence reused implementation run")
	}
}

func TestIntegrationMainMoveInvalidatesEvidenceAndRebuildsCandidate(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	original := f.candidate(t)
	ctx := context.Background()
	other := filepath.Join(t.TempDir(), "concurrent")
	if err := f.g.Clone(ctx, f.remote, other); err != nil {
		t.Fatal(err)
	}
	if err := f.g.Checkout(ctx, other, f.b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "src", "concurrent.txt"), []byte("another task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := f.g.CommitAll(ctx, other, "concurrent main", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.g.Publish(ctx, other, f.remote, "refs/heads/main", f.b, d); err != nil {
		t.Fatal(err)
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	invalidated := f.candidate(t)
	if invalidated.State != "accepted" || invalidated.Commit != "" || invalidated.Main != "" || invalidated.Review != nil || len(invalidated.Checks) != 0 || invalidated.ApprovedCommit != "" {
		t.Fatalf("old evidence survived M move: %+v", invalidated)
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	rebuilt := f.candidate(t)
	if rebuilt.State != "prechecking" || rebuilt.Main != string(d) || rebuilt.Commit == original.Commit || rebuilt.Review != nil || len(rebuilt.Checks) != 0 || rebuilt.CheckRunID != "" {
		t.Fatalf("new candidate incorrectly reused evidence: %+v", rebuilt)
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	f.reviewAndValidate(t)
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	remote, err := f.g.Query(ctx, f.remote, "refs/heads/main")
	if err != nil || string(remote) != rebuilt.Commit {
		t.Fatalf("rebuilt fixed C not landed: %s %v", remote, err)
	}
	if ancestor, err := f.g.IsAncestor(ctx, f.w.repo(), d, remote); err != nil || !ancestor {
		t.Fatal("concurrent main history was discarded", err)
	}
}

func TestIntegrationOutOfScopeMaterialIsRejectedBeforeReview(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "unauthorized.txt")
	candidate := f.candidate(t)
	if candidate.State != "rejected" || candidate.Commit != "" || !strings.Contains(candidate.Detail, "scope") {
		t.Fatalf("out of scope result accepted: %+v", candidate)
	}
	snapshot := f.snapshot(t)
	if snapshot.Tasks[0].HoldReason != "human_decision" || snapshot.Tasks[0].Repairs != 0 {
		t.Fatalf("scope violation silently repaired: %+v", snapshot.Tasks[0])
	}
	remote, err := f.g.Query(context.Background(), f.remote, "refs/heads/main")
	if err != nil || remote != f.b {
		t.Fatal("out of scope material changed main", err)
	}
}

func TestIntegrationLostAcknowledgementReconcilesExactCandidateInDescendant(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	candidate := f.candidate(t)
	ctx := context.Background()
	intent := domain.Intent{ID: "lost_ack", CandidateID: candidate.ID, Expected: candidate.Main, Desired: candidate.Commit, State: "dispatching", CreatedAt: f.s.Now()}
	// Model the durable intent/publication boundary followed by a lost ACK.
	// The ensuing Tick may only observe; it must not regenerate/push another C.
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		candidate.State = "landing"
		candidate.Version++
		if err := tx.Put("candidate", candidate.ID, candidate); err != nil {
			return err
		}
		return tx.Put("intent", intent.ID, intent)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.g.Publish(ctx, f.w.repo(), f.remote, "refs/heads/main", gitops.OID(candidate.Main), gitops.OID(candidate.Commit)); err != nil {
		t.Fatal(err)
	}
	b, e := f.s.CreateRequirement(ctx, f.owner, "reconcile-goal-req", domain.Requirement{Title: "New architecture", Body: "Use TypeScript"})
	req := itDecode[domain.Requirement](t, b, e)
	if _, e = f.s.SavePlanning(ctx, f.owner, "reconcile-goal-edit", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "New same-root goal", NonGoals: "No other source", Acceptance: "TypeScript checks", RequirementIDs: []string{req.ID}, Architecture: "TypeScript", DirectoryOutline: "src/: source", AllowedRoots: []string{"src"}, BudgetSeconds: 10000}}); e != nil {
		t.Fatal(e)
	}
	other := filepath.Join(t.TempDir(), "after-candidate")
	if err := f.g.Clone(ctx, f.remote, other); err != nil {
		t.Fatal(err)
	}
	if err := f.g.Checkout(ctx, other, gitops.OID(candidate.Commit)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "src", "later.txt"), []byte("later main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	later, err := f.g.CommitAll(ctx, other, "advance after original C", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.g.Publish(ctx, other, f.remote, "refs/heads/main", gitops.OID(candidate.Commit), later); err != nil {
		t.Fatal(err)
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	if snapshot.Candidates[0].State != "landed" || snapshot.Candidates[0].Commit != candidate.Commit || snapshot.Tasks[0].State != "integrated" {
		t.Fatalf("lost ACK failed exact C ancestry reconciliation: %+v", snapshot)
	}
	if remote, err := f.g.Query(ctx, f.remote, "refs/heads/main"); err != nil || remote != later {
		t.Fatal("reconciliation modified newer main", err)
	}
	if err = f.s.DB.View(ctx, func(tx *store.Tx) error {
		var recorded domain.Intent
		if err := tx.Get("intent", intent.ID, &recorded); err != nil {
			return err
		}
		if recorded.State != "completed" || recorded.Desired != candidate.Commit {
			t.Errorf("intent did not reconcile fixed C: %+v", recorded)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPhaseBFinalRealGitScopeRejectsAcceptedCandidateAfterGoalEdit(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	fixed := f.candidate(t)
	b, e := f.s.CreateRequirement(context.Background(), f.owner, "goal-requirement", domain.Requirement{Title: "Restricted", Body: "Only change approved directory"})
	req := itDecode[domain.Requirement](t, b, e)
	_, e = f.s.SavePlanning(context.Background(), f.owner, "goal-final-scope", domain.PlanningSaveRequest{Document: domain.PlanningDocument{Objective: "Restricted goal", NonGoals: "No other source", Acceptance: "Check", RequirementIDs: []string{req.ID}, Architecture: "Go", DirectoryOutline: "src/allowed/: source", AllowedRoots: []string{"src/allowed"}, BudgetSeconds: 10000}})
	if e != nil {
		t.Fatal(e)
	}
	if got := f.candidate(t); got.Commit != fixed.Commit || got.State != fixed.State {
		t.Fatal("goal edit destroyed accepted candidate")
	}
	if e = f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	actual, e := f.g.Query(context.Background(), f.remote, "refs/heads/main")
	if e != nil {
		t.Fatal(e)
	}
	if actual != f.b || f.candidate(t).State != fixed.State {
		t.Fatal("candidate outside current goal scope was published")
	}
}

func TestNewPublicationIntentRejectsOldAdmittedGoalWithIdenticalRoots(t *testing.T) {
	ctx := context.Background()
	f := newITFixture(t)
	b, e := f.s.CreateRequirement(ctx, f.owner, "intent-goal-requirement", domain.Requirement{Title: "Source", Body: "Build Go source"})
	req := itDecode[domain.Requirement](t, b, e)
	d := domain.PlanningDocument{Objective: "Build source", NonGoals: "No other source", Acceptance: "Go checks pass", RequirementIDs: []string{req.ID}, Architecture: "Go", DirectoryOutline: "src/: source", AllowedRoots: []string{"src"}, BudgetSeconds: 20000}
	admit := func(key string) {
		t.Helper()
		b, e := f.s.AuthorizePlanning(ctx, f.owner, key+"-auth", domain.VersionRequest{ExpectedVersion: d.Version})
		d = itDecode[domain.PlanningDocument](t, b, e)
		if _, e = f.s.SchedulePlanning(ctx, f.owner, key+"-evaluate", domain.PlanningMessageRequest{ExpectedVersion: d.Version}); e != nil {
			t.Fatal(e)
		}
		result := domain.LeaderResult{Reply: "Ready", Ready: true, MissingItems: []string{}, Clarifications: []string{}, Tasks: []domain.TaskProposal{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}}
		worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
		if e = worker.Tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	b, e = f.s.SavePlanning(ctx, f.owner, "intent-initial-goal", domain.PlanningSaveRequest{Document: d})
	d = itDecode[domain.PlanningDocument](t, b, e)
	admit("intent-initial")
	task := f.snapshot(t).Tasks[0]
	if e = f.s.DB.Update(ctx, func(tx *store.Tx) error { task.RequirementIDs = []string{req.ID}; return tx.Put("task", task.ID, task) }); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.TaskAction(ctx, f.owner, "intent-initial-resume", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); e != nil {
		t.Fatal(e)
	}
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	fixed := f.candidate(t)
	d.Architecture = "TypeScript"
	d.Acceptance = "TypeScript checks pass"
	b, e = f.s.SavePlanning(ctx, f.owner, "intent-same-root-edit", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
	d = itDecode[domain.PlanningDocument](t, b, e)
	admit("intent-new")
	if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Task
		if e := tx.Get("task", task.ID, &current); e != nil {
			return e
		}
		current.HoldReason = ""
		return tx.Put("task", current.ID, current)
	}); e != nil {
		t.Fatal(e)
	}
	if e = f.w.land(ctx, fixed); e == nil || !strings.Contains(e.Error(), "older goal") {
		t.Fatal("new publication intent accepted old goal authority", e)
	}
	if e = f.s.DB.View(ctx, func(tx *store.Tx) error {
		intents, e := store.List[domain.Intent](tx, "intent")
		if len(intents) != 0 {
			t.Error("old goal created external intent")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	actual, e := f.g.Query(ctx, f.remote, "refs/heads/main")
	if e != nil || actual != f.b || f.candidate(t).State != fixed.State {
		t.Fatal("old goal changed main or accepted material", e)
	}
}
