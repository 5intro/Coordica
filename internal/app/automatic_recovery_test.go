package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func recoveryCapabilities(tool string) []domain.Capability {
	return []domain.Capability{{Tool: tool, Available: true, Structured: true, Cancel: true, Review: true}}
}

func (f *serviceFixture) recoveryNode(t *testing.T, name string, capacity int) Actor {
	t.Helper()
	b, err := f.s.CreatePairing(context.Background(), f.owner, "pair-"+name, name)
	pair := decodeTest[map[string]any](t, b, err)
	registered, err := f.s.Register(context.Background(), domain.RegisterRequest{Code: pair["code"].(string), Name: name, Platform: "test", Capacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	node, err := f.s.Authenticate(context.Background(), registered.Token)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func (f *serviceFixture) recoveryClaim(t *testing.T, node Actor, key string, caps []domain.Capability) *domain.Assignment {
	t.Helper()
	b, err := f.s.Claim(context.Background(), node, key, domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40))
	return decodeTest[*domain.Assignment](t, b, err)
}

func recoveryRun(t *testing.T, snap domain.Snapshot, id string) domain.Run {
	t.Helper()
	for _, run := range snap.Runs {
		if run.ID == id {
			return run
		}
	}
	t.Fatalf("missing Run %s", id)
	return domain.Run{}
}

func (f *serviceFixture) expireRecoveryRun(t *testing.T, run domain.Run) {
	t.Helper()
	f.now = run.LeaseUntil.Add(time.Second)
	if err := f.s.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *serviceFixture) confirmRecoveryStop(t *testing.T, id, key string) {
	t.Helper()
	run := recoveryRun(t, f.snapshot(t), id)
	if _, err := f.s.ConfirmStopped(context.Background(), f.owner, key, id, domain.StopConfirmation{ExpectedVersion: run.Version, Evidence: "native process tree inspection confirmed every execution process has exited"}); err != nil {
		t.Fatal(err)
	}
}

func TestLostExecutionRequiresStopBeforeAutomaticCrossNodeReassignment(t *testing.T) {
	for _, confirmation := range []string{"runner", "owner"} {
		t.Run(confirmation, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			other := f.recoveryNode(t, "other", 1)
			third := f.recoveryNode(t, "third", 1)
			old := f.claim(t, "initial")
			f.expireRecoveryRun(t, old.Run)
			if next := f.recoveryClaim(t, other, "unknown-old-process", recoveryCapabilities("codex")); next != nil {
				t.Fatal("an expired lease alone dispatched a second execution")
			}
			if _, err := f.s.Complete(ctx, f.node, "not-stopped", old.Run.ID, domain.CompleteRequest{Generation: old.Run.Generation, Status: "stopped", Quiescent: false}); err != nil {
				t.Fatal(err)
			}
			if next := f.recoveryClaim(t, other, "still-not-stopped", recoveryCapabilities("codex")); next != nil {
				t.Fatal("unconfirmed process exit dispatched a second execution")
			}
			if len(f.snapshot(t).Runs) != 1 {
				t.Fatal("unknown execution created another Run")
			}
			var stopped []byte
			if confirmation == "runner" {
				req := domain.CompleteRequest{Generation: old.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), Quiescent: true}
				b, err := f.s.Complete(ctx, f.node, "confirmed-stopped", old.Run.ID, req)
				stopped = b
				if err != nil {
					t.Fatal(err)
				}
				// A lost completion acknowledgement must replay the same stop response.
				b, err = f.s.Complete(ctx, f.node, "confirmed-stopped", old.Run.ID, req)
				if err != nil || string(b) != string(stopped) {
					t.Fatal("stop acknowledgement replay changed", err)
				}
			} else {
				run := recoveryRun(t, f.snapshot(t), old.Run.ID)
				req := domain.StopConfirmation{ExpectedVersion: run.Version, Evidence: "process tree inspection: all execution processes absent"}
				b, err := f.s.ConfirmStopped(ctx, f.owner, "confirmed-stopped", old.Run.ID, req)
				stopped = b
				if err != nil {
					t.Fatal(err)
				}
				b, err = f.s.ConfirmStopped(ctx, f.owner, "confirmed-stopped", old.Run.ID, req)
				if err != nil || string(b) != string(stopped) {
					t.Fatal("owner stop acknowledgement replay changed", err)
				}
			}
			snap := f.snapshot(t)
			task := snap.Tasks[0]
			if task.State != "ready" || task.HoldReason != "" || task.CurrentRunID != "" || task.Retries != 1 || task.UsedSeconds != old.Run.GrantSeconds || task.ReservedSeconds != 0 || len(snap.Candidates) != 0 {
				t.Fatalf("stop did not atomically recover without accepting expired code: %+v", task)
			}
			if next := f.recoveryClaim(t, other, "wrong-tool", recoveryCapabilities("unsupported")); next != nil {
				t.Fatal("recovered task ignored tool compatibility")
			}
			if next := f.recoveryClaim(t, other, "no-cancel", []domain.Capability{{Tool: "codex", Available: true, Structured: true}}); next != nil {
				t.Fatal("recovered task ignored required cancellation capability")
			}
			// Independent online nodes contend for the recovered task using actual
			// SQLite transactions. Only one may acquire its next generation.
			var wg sync.WaitGroup
			results := make(chan *domain.Assignment, 12)
			for i := 0; i < cap(results); i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					node := []Actor{other, third}[i%2]
					b, err := f.s.Claim(ctx, node, fmt.Sprintf("race-%d", i), domain.ClaimRequest{Capabilities: recoveryCapabilities("codex")}, strings.Repeat("a", 40))
					if err != nil {
						t.Error(err)
						return
					}
					var assignment *domain.Assignment
					if err = json.Unmarshal(b, &assignment); err != nil {
						t.Error(err)
						return
					}
					if assignment != nil {
						results <- assignment
					}
				}(i)
			}
			wg.Wait()
			close(results)
			if len(results) != 1 {
				t.Fatalf("%d cross-node claims won", len(results))
			}
			next := <-results
			if next.Run.NodeID == old.Run.NodeID || next.Run.Generation != old.Run.Generation+1 || next.Run.ID == old.Run.ID {
				t.Fatalf("reassignment did not fence the old node and generation: %+v", next.Run)
			}
			snap = f.snapshot(t)
			if len(snap.Runs) != 2 || snap.Tasks[0].CurrentRunID != next.Run.ID || snap.Tasks[0].Retries != 1 || snap.Project.UsedSeconds != old.Run.GrantSeconds || snap.Project.ReservedSeconds != next.Run.GrantSeconds {
				t.Fatalf("reassignment or accounting was not atomic: %+v %+v", snap.Tasks[0], snap.Project)
			}
		})
	}
}

func TestRunnerStoppedAcknowledgementCanRecoverBeforeLeaseExpiration(t *testing.T) {
	f := newServiceFixture(t)
	old := f.claim(t, "initial")
	f.now = f.now.Add(time.Second)
	if _, err := f.s.Complete(context.Background(), f.node, "runner-restart-stop", old.Run.ID, domain.CompleteRequest{Generation: old.Run.Generation, ElapsedSeconds: 1, Status: "stopped", Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	if got := snap.Tasks[0]; got.State != "ready" || got.HoldReason != "" || got.Retries != 1 || got.UsedSeconds != 1 || got.ReservedSeconds != 0 {
		t.Fatalf("confirmed stop with a valid lease did not recover: %+v", got)
	}
	if snap.Project.UsedSeconds != 1 || snap.Project.ReservedSeconds != 0 {
		t.Fatal("confirmed stop changed already observed budget usage")
	}
}

func TestOldRunMessagesCannotChangeReplacementOrAcceptedCandidate(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := f.recoveryNode(t, "replacement", 1)
	old := f.claim(t, "initial")
	f.expireRecoveryRun(t, old.Run)
	f.confirmRecoveryStop(t, old.Run.ID, "stop")
	next := f.recoveryClaim(t, other, "replacement", recoveryCapabilities("codex"))
	if next == nil {
		t.Fatal("no replacement was dispatched")
	}
	assertProtected := func(before, after domain.Snapshot) {
		t.Helper()
		if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Project, after.Project) || !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(recoveryRun(t, before, next.Run.ID), recoveryRun(t, after, next.Run.ID)) {
			t.Fatalf("old Run changed the replacement, candidate, or accounting:\nbefore=%+v\nafter=%+v", before.Tasks, after.Tasks)
		}
	}
	lateMessages := func(stage string) {
		t.Helper()
		before := f.snapshot(t)
		for _, status := range []string{"succeeded", "failed", "transient_failure", "stopped"} {
			for _, quiet := range []bool{false, true} {
				if _, err := f.s.Complete(ctx, f.node, fmt.Sprintf("late-%s-%s-%t", stage, status, quiet), old.Run.ID, domain.CompleteRequest{Generation: old.Run.Generation, Status: status, Quiescent: quiet, ElapsedSeconds: 999999, Head: strings.Repeat("d", 40), SessionID: "old-session"}); err != nil {
					t.Fatal(err)
				}
				assertProtected(before, f.snapshot(t))
			}
		}
		response, err := f.s.Heartbeat(ctx, f.node, old.Run.ID, domain.HeartbeatRequest{Generation: old.Run.Generation, ElapsedSeconds: 999999, Detail: "late old heartbeat"})
		if err != nil || !response.Cancel || response.Reason != "stale_run" {
			t.Fatal("old heartbeat was not fenced", response, err)
		}
		assertProtected(before, f.snapshot(t))
		if _, err = f.s.Block(ctx, f.node, "late-block-"+stage, old.Run.ID, old.Run.Generation, "late decision"); err == nil {
			t.Fatal("old Run blocked replacement")
		}
		if _, err = f.s.UploadArtifact(ctx, f.node, old.Run.ID, strings.NewReader("late bundle")); err == nil {
			t.Fatal("old Run uploaded replacement code")
		}
		assertProtected(before, f.snapshot(t))
		if !recoveryRun(t, f.snapshot(t), old.Run.ID).Quiescent {
			t.Fatal("late message made a confirmed stopped process unknown again")
		}
	}
	lateMessages("executing")
	artifact, err := f.s.UploadArtifact(ctx, other, next.Run.ID, strings.NewReader("replacement immutable bundle"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	if _, err = f.s.Complete(ctx, other, "new-success", next.Run.ID, domain.CompleteRequest{Generation: next.Run.Generation, ElapsedSeconds: 1, Status: "succeeded", Head: strings.Repeat("c", 40), ArtifactID: artifact.ID, Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	lateMessages("candidate-accepted")
	snap := f.snapshot(t)
	if len(snap.Candidates) != 1 || snap.Candidates[0].SourceRunID != next.Run.ID || snap.Candidates[0].State != "accepted" || snap.Tasks[0].Retries != 1 {
		t.Fatal("late messages corrupted the accepted replacement candidate")
	}
}

func TestAutomaticRecoveryEnforcesCumulativeBudgetAndRetryLimits(t *testing.T) {
	for _, limitCase := range []string{"task-budget", "legacy-project-statistics", "no-retries", "last-retry"} {
		t.Run(limitCase, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				var task domain.Task
				if err := tx.Get("task", f.task.ID, &task); err != nil {
					return err
				}
				task.MaxRunSeconds = 10
				if limitCase == "task-budget" {
					task.BudgetSeconds = 10
				}
				if limitCase == "no-retries" {
					task.MaxRetries = 0
				}
				if limitCase == "last-retry" {
					task.MaxRetries = 1
				}
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				if limitCase == "legacy-project-statistics" {
					project, err := getProject(tx)
					if err != nil {
						return err
					}
					project.TotalBudgetSeconds = 10
					return tx.Put("project", project.ID, project)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			old := f.claim(t, "initial")
			f.expireRecoveryRun(t, old.Run)
			f.confirmRecoveryStop(t, old.Run.ID, "stop")
			spent := old.Run.GrantSeconds
			expectedRetries := 0
			if limitCase == "last-retry" {
				next := f.claim(t, "last-retry")
				if next == nil || next.Task.Retries != 1 {
					t.Fatal("the final authorized retry was not dispatched")
				}
				f.expireRecoveryRun(t, next.Run)
				f.confirmRecoveryStop(t, next.Run.ID, "last-stop")
				spent += next.Run.GrantSeconds
				expectedRetries = 1
			}
			snap := f.snapshot(t)
			wantHold := "human_decision"
			if limitCase == "task-budget" {
				wantHold = "budget"
			}
			if limitCase == "legacy-project-statistics" {
				if snap.Tasks[0].HoldReason != "" || snap.Tasks[0].UsedSeconds != spent || snap.Project.UsedSeconds != spent {
					t.Fatal("project spending incorrectly gated recovery")
				}
				next := f.claim(t, "beyond-legacy-cap")
				if next == nil || next.Task.Retries != 1 {
					t.Fatal("legacy project cap blocked retry")
				}
				return
			}

			if got := snap.Tasks[0]; got.HoldReason != wantHold || got.CurrentRunID != "" || got.Retries != expectedRetries || got.UsedSeconds != spent || got.ReservedSeconds != 0 {
				t.Fatalf("recovery crossed a finite limit: %+v", got)
			}
			if snap.Project.UsedSeconds != spent || snap.Project.ReservedSeconds != 0 {
				t.Fatalf("uncertain execution was refunded or debited twice: %+v", snap.Project)
			}
			if next := f.claim(t, "limit-blocked"); next != nil {
				t.Fatal("exhausted task was dispatched")
			}
			if err := f.s.Expire(ctx); err != nil {
				t.Fatal(err)
			}
			if got := f.snapshot(t).Tasks[0]; got.Retries != expectedRetries || got.UsedSeconds != spent {
				t.Fatal("repeated recovery scan changed retry or budget usage")
			}
		})
	}
}

func TestAutomaticRecoveryUsesOnlyRemainingFiniteGrant(t *testing.T) {
	f := newServiceFixture(t)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", f.task.ID, &task); err != nil {
			return err
		}
		task.BudgetSeconds = task.MaxRunSeconds + 5
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	old := f.claim(t, "initial")
	f.expireRecoveryRun(t, old.Run)
	f.confirmRecoveryStop(t, old.Run.ID, "stop")
	next := f.claim(t, "remaining-five-seconds")
	if next == nil || next.Run.GrantSeconds != 5 || !next.Run.LeaseUntil.Equal(next.Run.Deadline) || next.Task.UsedSeconds != old.Run.GrantSeconds || next.Task.ReservedSeconds != 5 {
		t.Fatalf("recovery exceeded remaining cumulative budget: %+v", next)
	}
}

func TestStoppedImplementationRetainsAlreadyAcceptedCandidateWithoutRedevelopment(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	old := f.claim(t, "initial")
	f.expireRecoveryRun(t, old.Run)
	candidate := domain.Candidate{ID: "already-accepted", Version: 1, TaskID: old.Run.TaskID, SourceRunID: old.Run.ID, State: "accepted", AuthorizationVersion: old.Run.AuthorizationVersion, PolicyVersion: old.Run.PolicyVersion, Base: old.Run.Base, Head: strings.Repeat("b", 40), CreatedAt: f.now}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", old.Run.TaskID, &task); err != nil {
			return err
		}
		task.State = "awaiting_integration"
		task.CurrentCandidateID = candidate.ID
		task.MaxRetries = 0
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		return tx.Put("candidate", candidate.ID, candidate)
	}); err != nil {
		t.Fatal(err)
	}
	f.confirmRecoveryStop(t, old.Run.ID, "stop-after-acceptance")
	snap := f.snapshot(t)
	if task := snap.Tasks[0]; task.State != "awaiting_integration" || task.CurrentCandidateID != candidate.ID || task.CurrentRunID != "" || task.HoldReason != "" || task.Retries != 0 || !reflect.DeepEqual(snap.Candidates[0], candidate) {
		t.Fatalf("stop acknowledgement restarted accepted work: %+v", task)
	}
	if next := f.claim(t, "do-not-redevelop"); next != nil {
		t.Fatal("already accepted candidate was dispatched for development again")
	}
}

func TestAutomaticRecoveryCannotUndoPauseCancelDecisionOrAuthorizationChange(t *testing.T) {
	for _, fence := range []string{"task-pause", "cancel", "human-decision", "task-authorization", "project-policy"} {
		t.Run(fence, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			other := f.recoveryNode(t, "replacement", 1)
			old := f.claim(t, "initial")
			current := f.snapshot(t).Tasks[0]
			switch fence {
			case "task-pause", "cancel":
				action := "pause"
				if fence == "cancel" {
					action = "cancel"
				}
				if _, err := f.s.TaskAction(ctx, f.owner, "user-action", current.ID, action, domain.VersionRequest{ExpectedVersion: current.Version}); err != nil {
					t.Fatal(err)
				}
			case "human-decision":
				if _, err := f.s.Block(ctx, f.node, "decision", old.Run.ID, old.Run.Generation, "architecture approval required"); err != nil {
					t.Fatal(err)
				}
			default:
				if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					if fence == "task-authorization" {
						current.AuthorizationVersion++
						return tx.Put("task", current.ID, current)
					}
					project, err := getProject(tx)
					if err != nil {
						return err
					}
					project.PolicyVersion++
					return tx.Put("project", project.ID, project)
				}); err != nil {
					t.Fatal(err)
				}
			}
			f.expireRecoveryRun(t, old.Run)
			f.confirmRecoveryStop(t, old.Run.ID, "confirmed-stop")
			if err := f.s.Expire(ctx); err != nil {
				t.Fatal(err)
			}
			got := f.snapshot(t).Tasks[0]
			if next := f.recoveryClaim(t, other, "must-remain-blocked", recoveryCapabilities("codex")); next != nil {
				t.Fatalf("automatic recovery crossed %s: %+v", fence, got)
			}
			if got.Retries != 0 || fence == "task-pause" && got.HoldReason != "paused" || fence == "human-decision" && got.HoldReason != "human_decision" || fence == "cancel" && got.State != "cancelled" {
				t.Fatalf("stop confirmation undid %s: %+v", fence, got)
			}
			if got.UsedSeconds != old.Run.GrantSeconds {
				t.Fatal("blocking recovery refunded spent budget")
			}
		})
	}
}

func TestProjectPauseDefersAutomaticRecoveryUntilResume(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := f.recoveryNode(t, "replacement", 1)
	old := f.claim(t, "initial")
	project := f.snapshot(t).Project
	if _, err := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: project.Version}); err != nil {
		t.Fatal(err)
	}
	f.expireRecoveryRun(t, old.Run)
	f.confirmRecoveryStop(t, old.Run.ID, "stop")
	if next := f.recoveryClaim(t, other, "paused-project", recoveryCapabilities("codex")); next != nil {
		t.Fatal("project pause was bypassed")
	}
	if got := f.snapshot(t).Tasks[0]; got.Retries != 0 {
		t.Fatal("paused pending recovery consumed retry before resuming")
	}
	project = f.snapshot(t).Project
	if _, err := f.s.ProjectAction(ctx, f.owner, "resume-project", "resume", domain.VersionRequest{ExpectedVersion: project.Version}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	next := f.recoveryClaim(t, other, "after-project-resume", recoveryCapabilities("codex"))
	if next == nil || next.Task.Retries != 1 || next.Run.Generation <= old.Run.Generation {
		t.Fatal("project resume did not automatically reconsider confirmed stopped work")
	}
}

func TestAutomaticRecoveryRespectsNodeCapacityAndRevocation(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	busyNode := f.recoveryNode(t, "busy", 1)
	freeNode := f.recoveryNode(t, "free", 1)
	old := f.claim(t, "initial")
	b, err := f.s.CreateTask(ctx, f.owner, "other-task", domain.Task{Title: "other", Description: "occupy a node", Acceptance: "independent task", AllowedPaths: []string{"other/**"}, Tool: "other-tool", BudgetSeconds: 100, MaxRunSeconds: 100})
	busyTask := decodeTest[domain.Task](t, b, err)
	if _, err = f.s.TaskAction(ctx, f.owner, "approve-other", busyTask.ID, "approve", domain.VersionRequest{ExpectedVersion: busyTask.Version}); err != nil {
		t.Fatal(err)
	}
	busy := f.recoveryClaim(t, busyNode, "occupy", recoveryCapabilities("other-tool"))
	if busy == nil {
		t.Fatal("could not occupy the test node")
	}
	// Keep the busy node's real lease live while the original execution expires.
	f.now = old.Run.LeaseUntil.Add(-time.Second)
	if _, err = f.s.Heartbeat(ctx, busyNode, busy.Run.ID, domain.HeartbeatRequest{Generation: busy.Run.Generation}); err != nil {
		t.Fatal(err)
	}
	f.expireRecoveryRun(t, old.Run)
	if _, err = f.s.RevokeNode(ctx, f.owner, "revoke-old", f.node.NodeID); err != nil {
		t.Fatal(err)
	}
	f.confirmRecoveryStop(t, old.Run.ID, "stop-old")
	if next := f.recoveryClaim(t, busyNode, "busy-cannot-claim", recoveryCapabilities("codex")); next != nil {
		t.Fatal("reassignment ignored node capacity")
	}
	if _, err = f.s.Claim(ctx, f.node, "revoked-cannot-claim", domain.ClaimRequest{Capabilities: recoveryCapabilities("codex")}, strings.Repeat("a", 40)); err == nil {
		t.Fatal("revoked node received recovered work")
	}
	next := f.recoveryClaim(t, freeNode, "free-can-claim", recoveryCapabilities("codex"))
	if next == nil || next.Run.NodeID != freeNode.NodeID || next.Run.TaskID != old.Run.TaskID {
		t.Fatal("revoking a failed node prevented reassignment to a valid free node")
	}
}

func TestLostReviewRecoveryKeepsFixedCandidateAndStage(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := f.recoveryNode(t, "other-reviewer", 1)
	implementation := f.claim(t, "implementation")
	artifact, err := f.s.UploadArtifact(ctx, f.node, implementation.Run.ID, strings.NewReader("immutable candidate fixture"))
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	if _, err = f.s.Complete(ctx, f.node, "implementation-complete", implementation.Run.ID, domain.CompleteRequest{Generation: implementation.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), ArtifactID: artifact.ID, Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	// Git material verification is independently exercised by integration tests.
	// Seed its durable fixed-C output, then use normal review dispatch/recovery.
	if err = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", f.task.ID, &task); err != nil {
			return err
		}
		var candidate domain.Candidate
		if err := tx.Get("candidate", task.CurrentCandidateID, &candidate); err != nil {
			return err
		}
		candidate.Commit = strings.Repeat("c", 40)
		candidate.Main = strings.Repeat("a", 40)
		candidate.State = "reviewing"
		return tx.Put("candidate", candidate.ID, candidate)
	}); err != nil {
		t.Fatal(err)
	}
	review := f.recoveryClaim(t, f.node, "first-review", recoveryCapabilities("independent-review"))
	if review == nil || review.Run.Kind != "review" {
		t.Fatal("initial review was not dispatched")
	}
	before := f.snapshot(t).Candidates[0]
	f.expireRecoveryRun(t, review.Run)
	f.confirmRecoveryStop(t, review.Run.ID, "review-stopped")
	if got := f.snapshot(t).Tasks[0]; got.State != "awaiting_integration" || got.CurrentCandidateID != before.ID || got.Repairs != 0 || got.Retries != 1 {
		t.Fatalf("lost review restarted development or replaced C: %+v", got)
	}
	next := f.recoveryClaim(t, other, "replacement-review", recoveryCapabilities("independent-review"))
	if next == nil || next.Run.Kind != "review" || next.Run.CandidateID != before.ID || next.Run.Base != before.Commit || next.Candidate == nil || !reflect.DeepEqual(*next.Candidate, before) {
		t.Fatalf("review retry changed the fixed candidate: %+v", next)
	}
}

func TestConfirmedPendingRecoverySurvivesCenterRestart(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	other := f.recoveryNode(t, "replacement", 1)
	old := f.claim(t, "initial")
	project := f.snapshot(t).Project
	if _, err := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: project.Version}); err != nil {
		t.Fatal(err)
	}
	f.expireRecoveryRun(t, old.Run)
	f.confirmRecoveryStop(t, old.Run.ID, "confirmed-before-restart")
	// A new service instance has no memory of which recovery was pending.
	f.s = New(f.s.DB, f.s.Dir)
	f.s.Now = func() time.Time { return f.now }
	if err := f.s.RecoverCenter(ctx); err != nil {
		t.Fatal(err)
	}
	project = f.snapshot(t).Project
	if _, err := f.s.ProjectAction(ctx, f.owner, "resume-after-restart", "resume", domain.VersionRequest{ExpectedVersion: project.Version}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	next := f.recoveryClaim(t, other, "reassign-after-restart", recoveryCapabilities("codex"))
	if next == nil || next.Task.Retries != 1 || next.Run.Generation != old.Run.Generation+1 || next.Task.UsedSeconds != old.Run.GrantSeconds {
		t.Fatal("restart lost pending recovery or changed cumulative authority")
	}
}

func seedRecoveryValidation(t *testing.T, f *serviceFixture) (domain.Run, domain.Candidate) {
	t.Helper()
	candidate := domain.Candidate{ID: "fixed-validation-candidate", Version: 1, TaskID: f.task.ID, State: "validating", AuthorizationVersion: 1, PolicyVersion: 1, Base: strings.Repeat("a", 40), Head: strings.Repeat("b", 40), Main: strings.Repeat("a", 40), Commit: strings.Repeat("c", 40), CreatedAt: f.now, Review: &domain.Review{Candidate: strings.Repeat("c", 40), Verdict: "approve", Summary: "independent fixed candidate approved", Findings: []string{}}}
	run := domain.Run{ID: "old-validation", Version: 1, TaskID: f.task.ID, NodeID: "center", Kind: "validation", Tool: "program", State: "running", Generation: 1, AuthorizationVersion: 1, PolicyVersion: 1, LeaseUntil: f.now.Add(time.Hour), Deadline: f.now.Add(time.Hour), GrantSeconds: 3600, Base: candidate.Commit, CandidateID: candidate.ID, StartedAt: f.now}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", f.task.ID, &task); err != nil {
			return err
		}
		task.State = "awaiting_integration"
		task.CurrentCandidateID = candidate.ID
		task.CurrentRunID = run.ID
		task.Generation = run.Generation
		task.ReservedSeconds = run.GrantSeconds
		project, err := getProject(tx)
		if err != nil {
			return err
		}
		project.ReservedSeconds = run.GrantSeconds
		for _, put := range []struct {
			kind, id string
			value    any
		}{{"candidate", candidate.ID, candidate}, {"run", run.ID, run}, {"task", task.ID, task}, {"project", project.ID, project}} {
			if err := tx.Put(put.kind, put.id, put.value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return run, candidate
}

func TestLostValidationRecoveryKeepsFixedCandidateAndIgnoresOldFinalizer(t *testing.T) {
	for _, confirmation := range []string{"owner", "validator"} {
		t.Run(confirmation, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			old, candidate := seedRecoveryValidation(t, f)
			f.expireRecoveryRun(t, old)
			worker := &Integrator{Service: f.s}
			if confirmation == "owner" {
				f.confirmRecoveryStop(t, old.ID, "validator-process-stopped")
			} else if err := worker.finishValidation(ctx, old, candidate, []domain.CheckResult{{Name: "git", Passed: true}}, nil, true, false); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			if got := before.Tasks[0]; got.State != "awaiting_integration" || got.CurrentRunID != "" || got.CurrentCandidateID != candidate.ID || got.HoldReason != "" || got.Retries != 1 || got.Repairs != 0 || got.UsedSeconds != old.GrantSeconds {
				t.Fatalf("validation recovery changed task stage or accounting: %+v", got)
			}
			if !reflect.DeepEqual(before.Candidates[0], candidate) {
				t.Fatal("stale validation accepted checks or changed the fixed candidate")
			}
			// Model the next validation's durable reservation; late finalization is
			// intentionally invoked with the first validator's captured arguments.
			replacement := old
			replacement.ID = "replacement-validation"
			replacement.Generation++
			replacement.StartedAt = f.now
			replacement.LeaseUntil = f.now.Add(time.Hour)
			replacement.Deadline = replacement.LeaseUntil
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				task := before.Tasks[0]
				task.Generation = replacement.Generation
				task.CurrentRunID = replacement.ID
				task.ReservedSeconds = replacement.GrantSeconds
				project := *before.Project
				project.ReservedSeconds = replacement.GrantSeconds
				if err := tx.Put("run", replacement.ID, replacement); err != nil {
					return err
				}
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				return tx.Put("project", project.ID, project)
			}); err != nil {
				t.Fatal(err)
			}
			before = f.snapshot(t)
			for _, failed := range []bool{false, true} {
				for _, quiet := range []bool{false, true} {
					var validationErr error
					if failed {
						validationErr = fmt.Errorf("late failed old check")
					}
					if err := worker.finishValidation(ctx, old, candidate, []domain.CheckResult{{Name: "git", Passed: !failed}}, validationErr, quiet, false); err != nil {
						t.Fatal(err)
					}
					after := f.snapshot(t)
					if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Project, after.Project) || !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(recoveryRun(t, before, replacement.ID), recoveryRun(t, after, replacement.ID)) {
						t.Fatal("late validator changed replacement checks, task, or budget")
					}
					if !recoveryRun(t, after, old.ID).Quiescent {
						t.Fatal("late finalizer made a stopped validator unknown again")
					}
				}
			}
		})
	}
}

func TestDetachedLostExecutionCannotModifyCurrentTask(t *testing.T) {
	for _, kind := range []string{"implement", "validation"} {
		t.Run(kind, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			var old domain.Run
			var candidate domain.Candidate
			if kind == "validation" {
				old, candidate = seedRecoveryValidation(t, f)
			} else {
				old = f.claim(t, "initial").Run
			}
			f.expireRecoveryRun(t, old)
			// A delayed/lifetime-old lost record can remain in a migrated database.
			// The persisted current pointer and generation are authoritative even
			// when that old record still has an unknown process state.
			replacement := old
			replacement.ID = "current-replacement"
			replacement.Generation++
			replacement.StartedAt = f.now
			replacement.LeaseUntil = f.now.Add(time.Hour)
			replacement.Deadline = replacement.LeaseUntil
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				var task domain.Task
				if err := tx.Get("task", old.TaskID, &task); err != nil {
					return err
				}
				task.HoldReason = ""
				task.CurrentRunID = replacement.ID
				task.Generation = replacement.Generation
				task.ReservedSeconds = replacement.GrantSeconds
				project, err := getProject(tx)
				if err != nil {
					return err
				}
				project.ReservedSeconds = replacement.GrantSeconds
				if err = tx.Put("run", replacement.ID, replacement); err != nil {
					return err
				}
				if err = tx.Put("task", task.ID, task); err != nil {
					return err
				}
				return tx.Put("project", project.ID, project)
			}); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			for index, quiet := range []bool{false, true, false} {
				if kind == "validation" {
					if err := (&Integrator{Service: f.s}).finishValidation(ctx, old, candidate, []domain.CheckResult{{Name: "git", Passed: false}}, fmt.Errorf("late old validation error"), quiet, false); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.s.Complete(ctx, f.node, fmt.Sprintf("detached-late-%d", index), old.ID, domain.CompleteRequest{Generation: old.Generation, Status: "succeeded", Head: strings.Repeat("d", 40), Quiescent: quiet, ElapsedSeconds: 999999}); err != nil {
					t.Fatal(err)
				}
				after := f.snapshot(t)
				if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Project, after.Project) || !reflect.DeepEqual(before.Candidates, after.Candidates) || !reflect.DeepEqual(recoveryRun(t, before, replacement.ID), recoveryRun(t, after, replacement.ID)) {
					t.Fatal("detached lost execution changed current task, candidate, or budget")
				}
			}
		})
	}
}

func TestUnconfirmedValidationExitSpendsFullGrantBeforeLeaseExpires(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	old, candidate := seedRecoveryValidation(t, f)
	f.now = f.now.Add(time.Second)
	if err := (&Integrator{Service: f.s}).finishValidation(ctx, old, candidate, nil, fmt.Errorf("process tree exit could not be confirmed"), false, false); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	assertSpent := func(snap domain.Snapshot) {
		t.Helper()
		if snap.Tasks[0].UsedSeconds != old.GrantSeconds || snap.Tasks[0].ReservedSeconds != 0 || snap.Project.UsedSeconds != old.GrantSeconds || snap.Project.ReservedSeconds != 0 {
			t.Fatalf("unknown validator silently refunded its finite grant: task=%+v project=%+v", snap.Tasks[0], snap.Project)
		}
	}
	assertSpent(snap)
	if run := recoveryRun(t, snap, old.ID); run.State != "lost" || run.Quiescent {
		t.Fatalf("unconfirmed validator exit was accepted as stopped: %+v", run)
	}
	if got := snap.Tasks[0]; got.Retries != 0 || got.CurrentRunID != old.ID || got.HoldReason != "offline" {
		t.Fatal("unconfirmed validator was retried before its process stopped")
	}
	f.confirmRecoveryStop(t, old.ID, "confirmed-validator-stop")
	snap = f.snapshot(t)
	assertSpent(snap)
	if got := snap.Tasks[0]; got.Retries != 1 || got.CurrentRunID != "" || got.HoldReason != "" || got.CurrentCandidateID != candidate.ID {
		t.Fatal("confirmed validator stop did not recover its fixed candidate once")
	}
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	snap = f.snapshot(t)
	assertSpent(snap)
	if snap.Tasks[0].Retries != 1 {
		t.Fatal("repeated recovery double-consumed retries")
	}
}
