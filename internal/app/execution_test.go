package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type serviceFixture struct {
	s           *Service
	owner, node Actor
	task        domain.Task
	now         time.Time
}

func decodeTest[T any](t *testing.T, b []byte, e error) T {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
	var v T
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	dir := t.TempDir()
	db, e := store.Open(filepath.Join(dir, "coordica.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	f := &serviceFixture{s: New(db, dir), owner: Actor{ID: "owner", Role: "owner"}, now: time.Now().UTC()}
	f.s.Now = func() time.Time { return f.now }
	ctx := context.Background()
	_, e = f.s.CreateProject(ctx, f.owner, "project", domain.Project{Name: "Test", RepositoryURL: dir, TargetBranch: "main", TotalBudgetSeconds: 36000, Checks: []domain.Check{{Name: "git", Command: "git", Args: []string{"status", "--porcelain"}, TimeoutSeconds: 10}}})
	if e != nil {
		t.Fatal(e)
	}
	seedReadyBaseline(t, f.s)
	b, e := f.s.CreateTask(ctx, f.owner, "task", domain.Task{Title: "Task", Description: "Change source", Acceptance: "Test", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 14400, MaxRunSeconds: 3600, MaxRepairs: 2, MaxRetries: 2})
	f.task = decodeTest[domain.Task](t, b, e)
	b, e = f.s.TaskAction(ctx, f.owner, "approve", f.task.ID, "approve", domain.VersionRequest{ExpectedVersion: f.task.Version})
	f.task = decodeTest[domain.Task](t, b, e)
	b, e = f.s.CreatePairing(ctx, f.owner, "pair", "Node")
	pair := decodeTest[map[string]any](t, b, e)
	registered, e := f.s.Register(ctx, domain.RegisterRequest{Code: pair["code"].(string), Name: "Node", Platform: "test", Capacity: 2})
	if e != nil {
		t.Fatal(e)
	}
	f.node, e = f.s.Authenticate(ctx, registered.Token)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *serviceFixture) claim(t *testing.T, key string) *domain.Assignment {
	t.Helper()
	b, e := f.s.Claim(context.Background(), f.node, key, domain.ClaimRequest{AssignmentContextVersion: domain.AssignmentContextVersion, Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}, strings.Repeat("a", 40))
	return decodeTest[*domain.Assignment](t, b, e)
}
func (f *serviceFixture) snapshot(t *testing.T) domain.Snapshot {
	t.Helper()
	v, e := f.s.Snapshot(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestAtomicClaimAndReplay(t *testing.T) {
	f := newServiceFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := f.s.Claim(context.Background(), f.node, ID("key"), domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}, strings.Repeat("a", 40))
			if e != nil {
				t.Error(e)
				return
			}
			if string(b) != "null" {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatalf("%d executions won", count)
	}
	snap := f.snapshot(t)
	if len(snap.Runs) != 1 || snap.Project.ReservedSeconds != 3600 {
		t.Fatalf("wrong reservation: %+v", snap.Project)
	}
}
func TestCompletionIdempotencyAndFinalAllowedRetry(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		a := f.claim(t, ID("claim"))
		f.now = f.now.Add(time.Second)
		_, e := f.s.Complete(ctx, f.node, ID("complete"), a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, ElapsedSeconds: 1, Status: "transient_failure", Quiescent: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	a := f.claim(t, "last-retry")
	if a == nil {
		t.Fatal("second retry must be allowed")
	}
	artifact, e := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("immutable bundle fixture, git validation happens later"))
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Second)
	req := domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), ArtifactID: artifact.ID, Quiescent: true}
	b, e := f.s.Complete(ctx, f.node, "done", a.Run.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	b2, e := f.s.Complete(ctx, f.node, "done", a.Run.ID, req)
	if e != nil || string(b) != string(b2) {
		t.Fatal("replay changed", e)
	}
	snap := f.snapshot(t)
	if len(snap.Candidates) != 1 || snap.Tasks[0].Retries != 2 || snap.Tasks[0].HoldReason != "" || snap.Project.UsedSeconds != 3 || snap.Project.ReservedSeconds != 0 {
		t.Fatalf("bad terminal accounting: %+v %+v", snap.Tasks[0], snap.Project)
	}
	req.Detail = "altered"
	if _, e = f.s.Complete(ctx, f.node, "done", a.Run.ID, req); e == nil {
		t.Fatal("same key different payload accepted")
	}
	f.now = f.now.Add(2 * time.Hour)
	if e = f.s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).Candidates[0].State != "accepted" {
		t.Fatal("accepted candidate depends on old lease")
	}
}
func TestStaleRunCannotSubmitOrResumeUnknownProcess(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a := f.claim(t, "start")
	f.now = f.now.Add(time.Minute)
	if e := f.s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	task := snap.Tasks[0]
	if _, e := f.s.TaskAction(ctx, f.owner, "resume", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); e == nil {
		t.Fatal("unknown old process resumed")
	}
	if _, e := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("late")); e == nil {
		t.Fatal("stale upload accepted")
	}
	_, e := f.s.Complete(ctx, f.node, "late", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), Quiescent: true})
	if e != nil {
		t.Fatal(e)
	}
	snap = f.snapshot(t)
	if len(snap.Candidates) != 0 || snap.Project.UsedSeconds != 3600 || snap.Project.ReservedSeconds != 0 {
		t.Fatal("stale result changed authority or accounting")
	}
}
func TestNodeCannotApprove(t *testing.T) {
	f := newServiceFixture(t)
	if _, e := f.s.TaskAction(context.Background(), f.node, "evil", f.task.ID, "resume", domain.VersionRequest{ExpectedVersion: f.task.Version}); e == nil {
		t.Fatal("node crossed owner boundary")
	}
}

func TestCancelledCandidateCannotStartValidation(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	c := domain.Candidate{ID: "cand_cancel", TaskID: f.task.ID, State: "validating", Commit: strings.Repeat("c", 40), AuthorizationVersion: 1, PolicyVersion: 1, Review: &domain.Review{Candidate: strings.Repeat("c", 40), Verdict: "approve"}}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		f.task.State = "awaiting_integration"
		f.task.CurrentCandidateID = c.ID
		if e := tx.Put("task", f.task.ID, f.task); e != nil {
			return e
		}
		return tx.Put("candidate", c.ID, c)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.TaskAction(ctx, f.owner, "cancel", f.task.ID, "cancel", domain.VersionRequest{ExpectedVersion: f.task.Version}); e != nil {
		t.Fatal(e)
	}
	// A worker that read c before cancel must recheck inside its start transaction.
	if e := (&Integrator{Service: f.s}).validate(ctx, c); e == nil {
		t.Fatal("cancelled task began validation")
	}
	if len(f.snapshot(t).Runs) != 0 {
		t.Fatal("cancelled task created a new Run")
	}
}
func TestStopConfirmationPreservesSpentBudgetAndAutomaticallyRecovers(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a := f.claim(t, "run")
	f.now = f.now.Add(time.Minute)
	if e := f.s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if _, e := f.s.ConfirmStopped(ctx, f.owner, "empty-evidence", a.Run.ID, domain.StopConfirmation{ExpectedVersion: snap.Runs[0].Version}); e == nil {
		t.Fatal("missing evidence accepted")
	}
	if _, e := f.s.ConfirmStopped(ctx, f.owner, "confirm", a.Run.ID, domain.StopConfirmation{ExpectedVersion: snap.Runs[0].Version, Evidence: "checked ordinary process group and no child remains"}); e != nil {
		t.Fatal(e)
	}
	snap = f.snapshot(t)
	if got := snap.Tasks[0]; got.State != "ready" || got.HoldReason != "" || got.CurrentRunID != "" || got.Retries != 1 {
		t.Fatalf("stop confirmation requires another owner action: %+v", got)
	}
	next := f.claim(t, "new-run")
	if next == nil || next.Run.Generation <= a.Run.Generation {
		t.Fatal("generation not fenced")
	}
	if f.snapshot(t).Project.UsedSeconds != 3600 {
		t.Fatal("recovery refunded unknown execution")
	}
}

func TestLatePrepareFailureCannotUndoPauseOrCancel(t *testing.T) {
	for _, action := range []string{"pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			c := domain.Candidate{ID: "candidate_late", TaskID: f.task.ID, State: "accepted", AuthorizationVersion: 1, PolicyVersion: 1}
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				f.task.State = "awaiting_integration"
				f.task.CurrentCandidateID = c.ID
				if e := tx.Put("task", f.task.ID, f.task); e != nil {
					return e
				}
				return tx.Put("candidate", c.ID, c)
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := f.s.TaskAction(ctx, f.owner, "action", f.task.ID, action, domain.VersionRequest{ExpectedVersion: f.task.Version}); e != nil {
				t.Fatal(e)
			}
			if e := (&Integrator{Service: f.s}).reject(ctx, c, domain.Fail("conflict", "late merge conflict"), true); e != nil {
				t.Fatal(e)
			}
			got := f.snapshot(t).Tasks[0]
			if action == "pause" && got.HoldReason != "paused" || action == "cancel" && got.State != "cancelled" || got.Repairs != 0 {
				t.Fatalf("late work undid user action: %+v", got)
			}
		})
	}
}

func TestIdlePollingDoesNotRequireRemoteGit(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		f.task.State = "draft"
		f.task.ApprovedVersion = 0
		return tx.Put("task", f.task.ID, f.task)
	}); e != nil {
		t.Fatal(e)
	}
	// A nil Git client makes any accidental remote fetch fail this test.
	raw, e := (&Integrator{Service: f.s}).Claim(ctx, f.node, "idle", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}})
	if e != nil || string(raw) != "null" {
		t.Fatal(string(raw), e)
	}
}

func TestDependencyBlockedPollingDoesNotResolveGit(t *testing.T) {
	for _, state := range []string{"draft", "executing", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				dependency := f.task
				dependency.ID = "dependency"
				dependency.State = state
				dependency.ApprovedVersion = 0
				if err := tx.Put("task", dependency.ID, dependency); err != nil {
					return err
				}
				f.task.Dependencies = []string{dependency.ID}
				return tx.Put("task", f.task.ID, f.task)
			}); err != nil {
				t.Fatal(err)
			}
			raw, err := (&Integrator{Service: f.s}).Claim(ctx, f.node, "blocked-dependency", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true}}})
			if err != nil || string(raw) != "null" {
				t.Fatal("blocked dependency required Git", string(raw), err)
			}
		})
	}
}
