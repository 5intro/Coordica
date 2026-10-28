package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestFailureRoutingClassifiesRemediationWithoutGrantingScope(t *testing.T) {
	for _, category := range []string{"test_error", "product_defect", "evidence_missing"} {
		t.Run(category, func(t *testing.T) {
			f, source := failureFixture(t)
			plan := followupPlan(source)
			plan.Category = category
			plan.Tasks[0].Paths = []string{"src/verification/**"}
			got := runRecovery(t, f, plan)
			if got.State != "waiting_followup" {
				t.Fatal("diagnosis did not route to an ordinary repair", got)
			}
			child := f.claim(t, "repair-"+category)
			if child == nil || child.Task.ID != got.FollowupTaskID || child.FailureContext.Category != category ||
				!reflect.DeepEqual(child.Task.AllowedPaths, plan.Tasks[0].Paths) || child.Task.Acceptance != plan.Tasks[0].Acceptance {
				t.Fatal("repair lost its diagnosis, least write scope or validation contract", child)
			}
			if original := requirementTask(t, f, source.SourceTask.ID); !reflect.DeepEqual(original.AllowedPaths, source.SourceTask.AllowedPaths) ||
				original.Acceptance != source.SourceTask.Acceptance || original.HoldReason != "failure_followup" {
				t.Fatal("repair broadened or waived original acceptance", original)
			}
		})
	}
}

func TestFailureRecheckRetainsFindingsAndCannotMintAnotherFollowup(t *testing.T) {
	f, _, oldReview := failureReviewFixture(t, &domain.RecoveryHint{Category: "product_defect", Reason: "Product fault outside evidence scope"}, false)
	source := failureCases(t, f.s)[0]
	source = followupTimeSource(t, f, source, false, false, true)
	got := runRecovery(t, f, followupPlan(source))
	landed := completeTimeFollowup(t, f, got)
	if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A new process gets current main and the immutable rejection/diagnosis,
	// even though the old repair counter was already exhausted.
	next := f.claim(t, "bounded-recheck")
	if next == nil || next.Task.ID != source.SourceTask.ID || next.Run.Kind != "implement" ||
		next.Task.FailureRecheckID != source.ID || next.Task.Repairs != next.Task.MaxRepairs || next.Candidate != nil ||
		next.FailureContext == nil || next.FailureContext.FollowupCommit != landed || len(next.FailureContext.Findings) == 0 {
		t.Fatal("recheck lost bounded fresh execution or source findings", next)
	}
	before := requirementTask(t, f, source.SourceTask.ID)
	_, err := f.s.Complete(context.Background(), f.node, "late-old-review", oldReview.Run.ID, domain.CompleteRequest{
		Generation: oldReview.Run.Generation, Status: "succeeded", Quiescent: true,
		Review: &domain.Review{Candidate: oldReview.Run.Base, Verdict: "approve", Summary: "late", Findings: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if after := requirementTask(t, f, source.SourceTask.ID); !reflect.DeepEqual(before, after) {
		t.Fatal("old review changed a fresh recheck", after)
	}
	_, err = f.s.Complete(context.Background(), f.node, "recheck-failed", next.Run.ID, domain.CompleteRequest{
		Generation: next.Run.Generation, Status: "stopped", IntentStatus: "blocked", Quiescent: true, Detail: "Still fails original acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	count := len(f.snapshot(t).Tasks)
	if err := (&PlanningWorker{Service: f.s, Executor: recoveryExecutor(t, followupPlan(source))}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.snapshot(t).Tasks) != count {
		t.Fatal("second failure minted an unlimited replacement task")
	}
	if task := requirementTask(t, f, source.SourceTask.ID); task.State == "ready" || task.HoldReason == "" || task.Repairs != before.Repairs {
		t.Fatal("failed recheck was marked accepted or counters refreshed", task)
	}
}

func TestFailureFollowupClosesFailedChildButWaitsForProvenStop(t *testing.T) {
	for _, hold := range []string{"human_decision", "budget", "tests_failed", "failure_assessment", "cancelled"} {
		t.Run(hold, func(t *testing.T) {
			f, source := failureFixture(t)
			got := runRecovery(t, f, followupPlan(source))
			child := f.claim(t, "live-child")
			if child == nil || child.Task.ID != got.FollowupTaskID {
				t.Fatal("child not claimed")
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var task domain.Task
				if err := tx.Get("task", child.Task.ID, &task); err != nil {
					return err
				}
				task.CurrentRunID, task.HoldReason = "", hold
				if hold == "cancelled" {
					task.State = "cancelled"
				}
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				var r domain.Run
				if err := tx.Get("run", child.Run.ID, &r); err != nil {
					return err
				}
				r.State = "lost"
				return tx.Put("run", r.ID, r)
			}); err != nil {
				t.Fatal(err)
			}
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			if failureCases(t, f.s)[0].State != "waiting_followup" {
				t.Fatal("unknown-live child was replaced")
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var r domain.Run
				if err := tx.Get("run", child.Run.ID, &r); err != nil {
					return err
				}
				r.State, r.Quiescent, r.FinishedAt = "stopped", true, &f.now
				return tx.Put("run", r.ID, r)
			}); err != nil {
				t.Fatal(err)
			}
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			if task := requirementTask(t, f, source.SourceTask.ID); task.HoldReason != "human_decision" || task.FailureRecheckID != "" {
				t.Fatal("failed repair remained waiting or granted a recheck", task)
			}
		})
	}
}

func TestFailureEvidenceFollowupCannotWeakenScopeOrAcceptance(t *testing.T) {
	for _, path := range []string{"src/report/**", "outside/**", "src/secrets/**"} {
		t.Run(path, func(t *testing.T) {
			f, source := failureFixture(t)
			if path == "src/secrets/**" {
				if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					p, err := getProject(tx)
					if err != nil {
						return err
					}
					p.ProtectedPaths = []string{path}
					return tx.Put("project", p.ID, p)
				}); err != nil {
					t.Fatal(err)
				}
			}
			plan := followupPlan(source)
			plan.Category, plan.Tasks[0].Paths = "evidence_missing", []string{path}
			got := runRecovery(t, f, plan)
			if (got.State != "needs_owner" && got.State != "stale") || len(f.snapshot(t).Tasks) != 2 {
				t.Fatal("evidence repair expanded authority", got)
			}
		})
	}
	for _, category := range []string{"unknown", "contract_mismatch", "execution_failure"} {
		raw, _ := json.Marshal(domain.FailureRecoveryPlan{Category: category, Reason: "Unproven remediation", Action: "follow_up", Tasks: []domain.TaskProposal{{Paths: []string{"src/**"}}}})
		if _, err := decodeFailurePlan(string(raw)); err == nil {
			t.Fatal("unproven cause granted follow-up", category)
		}
	}
	if !strings.Contains(failureInstruction, "do not weaken assertions") {
		t.Fatal("test-script correction can weaken the contract")
	}
}

func TestFailureRecheckAdmissionIsAtomicAcrossConcurrentTicks(t *testing.T) {
	f, source := failureFixture(t)
	got := runRecovery(t, f, followupPlan(source))
	completeTimeFollowup(t, f, got)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	task := requirementTask(t, f, source.SourceTask.ID)
	if task.AuthorizationVersion != source.SourceTask.AuthorizationVersion+1 || task.FailureRecheckID != source.ID ||
		task.Repairs != source.SourceTask.Repairs || len(f.snapshot(t).Tasks) != 3 {
		t.Fatal("concurrent ticks minted duplicate work or authority", task)
	}
}

func TestFailureRecheckRequiresActualIntegratedCandidate(t *testing.T) {
	for _, corruption := range []string{"no candidate", "wrong task", "wrong authority", "not landed"} {
		t.Run(corruption, func(t *testing.T) {
			f, source := failureFixture(t)
			got := runRecovery(t, f, followupPlan(source))
			completeTimeFollowup(t, f, got)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var child domain.Task
				if err := tx.Get("task", got.FollowupTaskID, &child); err != nil {
					return err
				}
				if corruption == "no candidate" {
					child.CurrentCandidateID = ""
					return tx.Put("task", child.ID, child)
				}
				var c domain.Candidate
				if err := tx.Get("candidate", child.CurrentCandidateID, &c); err != nil {
					return err
				}
				switch corruption {
				case "wrong task":
					c.TaskID = "another task"
				case "wrong authority":
					c.AuthorizationVersion++
				case "not landed":
					c.State = "ready_to_land"
				}
				return tx.Put("candidate", c.ID, c)
			}); err != nil {
				t.Fatal(err)
			}
			before := requirementTask(t, f, source.SourceTask.ID)
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			after := requirementTask(t, f, source.SourceTask.ID)
			if after.FailureRecheckID != "" || after.HoldReason != "human_decision" || after.AuthorizationVersion != before.AuthorizationVersion ||
				after.Repairs != before.Repairs || failureCases(t, f.s)[0].State != "needs_owner" {
				t.Fatal("invalid integration granted authority or remained waiting", after)
			}
		})
	}
}
