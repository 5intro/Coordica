package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Use the real admission, claim, settlement and stop transitions, not model
// prose, to create a recoverable failure in an arbitrary project.
func failureFixture(t *testing.T) (*serviceFixture, domain.FailureAssessment) {
	t.Helper()
	f, d, result := planningFixture(t, true)
	result.Tasks[0].Paths = []string{"src/report/**"}
	scheduleTest(t, f, d, "failure-admit")
	if err := (&PlanningWorker{Service: f.s, Executor: resultExecutor(result)}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := f.claim(t, "failure-source")
	if a == nil {
		t.Fatal("source not claimed")
	}
	_, err := f.s.Complete(context.Background(), f.node, "failure-blocked", a.Run.ID,
		domain.CompleteRequest{Generation: a.Run.Generation, Status: "stopped", IntentStatus: "blocked", Quiescent: true, ElapsedSeconds: 3,
			Detail: "Requested report requires a missing product capability outside this task scope"})
	if err != nil {
		t.Fatal(err)
	}
	cases := failureCases(t, f.s)
	if len(cases) != 1 {
		t.Fatalf("expected one stopped failure, got %+v", cases)
	}
	return f, cases[0]
}

func failureCases(t *testing.T, s *Service) []domain.FailureAssessment {
	t.Helper()
	var cases []domain.FailureAssessment
	if err := s.DB.View(context.Background(), func(tx *store.Tx) (err error) {
		cases, err = store.List[domain.FailureAssessment](tx, "failure_assessment")
		return
	}); err != nil {
		t.Fatal(err)
	}
	return cases
}

func recoveryExecutor(t *testing.T, plan domain.FailureRecoveryPlan) PlanningExecutor {
	t.Helper()
	return planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		if !strings.HasPrefix(prompt, "COORDICA_FAILURE_RECOVERY_V1\n") || !strings.Contains(prompt, "source_run") {
			t.Fatal("failure diagnosis omitted source evidence", prompt)
		}
		raw, _ := json.Marshal(plan)
		return PlanningOutcome{Output: string(raw), TreeStopped: true, ElapsedSeconds: 2}, nil
	})
}

func runRecovery(t *testing.T, f *serviceFixture, plan domain.FailureRecoveryPlan) domain.FailureAssessment {
	t.Helper()
	if err := (&PlanningWorker{Service: f.s, Executor: recoveryExecutor(t, plan)}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	return failureCases(t, f.s)[0]
}

func TestFailureRecoveryRepairsOriginalWithoutResettingAccounting(t *testing.T) {
	f, before := failureFixture(t)
	got := runRecovery(t, f, domain.FailureRecoveryPlan{Category: "execution_failure", Reason: "Retry the stopped task within its unchanged scope", Action: "repair_current", Tasks: []domain.TaskProposal{}})
	var task domain.Task
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("task", before.SourceTask.ID, &task) }); err != nil {
		t.Fatal(err)
	}
	want := before.SourceTask
	want.Version++
	want.Repairs++
	want.State = "ready"
	want.HoldReason = ""
	if got.State != "applied" || !reflect.DeepEqual(task, want) {
		t.Fatal("repair changed authority, counters or contract", got, task, want)
	}
	if len(f.snapshot(t).Tasks) != 2 || task.UsedSeconds != 3 {
		t.Fatal("repair minted replacement work or cleared spend")
	}
	if err := (&PlanningWorker{Service: f.s, Executor: recoveryExecutor(t, *got.Result)}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(failureCases(t, f.s)) != 1 {
		t.Fatal("duplicate recovery")
	}
}

func followupPlan(source domain.FailureAssessment) domain.FailureRecoveryPlan {
	return domain.FailureRecoveryPlan{Category: "product_defect", Reason: "Implement the missing capability within the admitted goal", Action: "follow_up", Tasks: []domain.TaskProposal{{
		Key: "untrusted-key", Title: "Repair capability", Objective: "Repair the missing capability", Acceptance: "Demonstrate the capability on the fixed development base",
		RequirementIDs: source.SourceTask.RequirementIDs, Paths: []string{"src/runtime/**"}, Dependencies: []string{}, HotResources: []string{}, BudgetSeconds: 60, MaxRunSeconds: 20, MaxRepairs: 1, MaxRetries: 1}}}
}

func TestFailureRecoveryFollowupIsOrdinaryBoundedWorkAndResumesFreshSource(t *testing.T) {
	f, source := failureFixture(t)
	recovery := runRecovery(t, f, followupPlan(source))
	if recovery.State != "waiting_followup" || recovery.FollowupTaskID == "" {
		t.Fatalf("follow-up not created: %+v", recovery)
	}
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", recovery.FollowupTaskID, &task); err != nil {
			return err
		}
		if task.State != "ready" || task.FailureSourceTaskID != source.SourceTask.ID || task.FailureAssessmentID != source.ID {
			t.Fatal("missing ordinary task provenance", task)
		}
		if err := goalGate(tx, &task, false); err != nil {
			return err
		}
		context, err := assignmentFailureContext(tx, task)
		if err != nil {
			return err
		}
		if context.SourceRunID != source.SourceRunID || context.SourceAcceptance != source.SourceTask.Acceptance {
			t.Fatal("follow-up lost source contract", context)
		}
		// Integration completion is injected here; the integration suite separately
		// exercises code, fixed candidate, review and landing gates.
		task.State = "integrated"
		task.Version++
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("task", source.SourceTask.ID, &task) }); err != nil {
		t.Fatal(err)
	}
	if task.State != "ready" || task.HoldReason != "" || task.Repairs != source.SourceTask.Repairs+1 || task.UsedSeconds != source.SourceTask.UsedSeconds ||
		task.AuthorizationVersion != source.SourceTask.AuthorizationVersion+1 || task.CurrentCandidateID != "" {
		t.Fatal("source did not retain finite fresh execution", task)
	}
}

func TestFailureRecoveryRejectsUnauthorizedReplacementAtomically(t *testing.T) {
	for _, scenario := range []string{"same scope", "outside goal", "unrelated requirement", "dependencies", "invented test run", "recursive", "exhausted repairs"} {
		t.Run(scenario, func(t *testing.T) {
			f, source := failureFixture(t)
			plan := followupPlan(source)
			if scenario == "exhausted repairs" {
				plan.Action = "repair_current"
				plan.Tasks = []domain.TaskProposal{}
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				if scenario != "recursive" && scenario != "exhausted repairs" {
					return nil
				}
				task := source.SourceTask
				if scenario == "recursive" {
					task.FailureSourceTaskID = "prior-task"
				} else {
					task.Repairs = task.MaxRepairs
				}
				source.SourceTask = task
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				return tx.Put("failure_assessment", source.ID, source)
			}); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "same scope":
				plan.Tasks[0].Paths = source.SourceTask.AllowedPaths
			case "outside goal":
				plan.Tasks[0].Paths = []string{"other/**"}
			case "unrelated requirement":
				plan.Tasks[0].RequirementIDs = []string{"not-authorized"}
			case "dependencies":
				plan.Tasks[0].Dependencies = []string{"pretend-ready"}
			case "invented test run":
				plan.Tasks[0].TestRunID = "fake"
			}
			got := runRecovery(t, f, plan)
			if got.State != "needs_owner" || len(f.snapshot(t).Tasks) != 2 || got.FollowupTaskID != "" {
				t.Fatal("invalid plan partially applied", got)
			}
		})
	}
}

func TestFailureRecoverySourceAndPolicyFencesDuringLeaderExecution(t *testing.T) {
	for _, change := range []string{"task", "run", "policy", "automation", "goal"} {
		t.Run(change, func(t *testing.T) {
			f, source := failureFixture(t)
			executor := planningExecFunc(func(_ context.Context, _ string, _ string) (PlanningOutcome, error) {
				err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					switch change {
					case "task":
						task := source.SourceTask
						task.Version++
						return tx.Put("task", task.ID, task)
					case "run":
						var r domain.Run
						if err := tx.Get("run", source.SourceRunID, &r); err != nil {
							return err
						}
						r.Version++
						return tx.Put("run", r.ID, r)
					case "policy":
						p, err := getProject(tx)
						if err != nil {
							return err
						}
						p.PolicyVersion++
						return tx.Put("project", p.ID, p)
					case "automation":
						a, err := automation(tx)
						if err != nil {
							return err
						}
						a.Enabled = false
						a.Version++
						return tx.Put("automation", "current", a)
					case "goal":
						d, err := goalDocument(tx)
						if err != nil {
							return err
						}
						d.AuthorizationVersion++
						return tx.Put("planning_document", "current", d)
					}
					return nil
				})
				if err != nil {
					return PlanningOutcome{}, err
				}
				raw, _ := json.Marshal(domain.FailureRecoveryPlan{Category: "unknown", Reason: "Must not apply stale diagnosis", Action: "repair_current", Tasks: []domain.TaskProposal{}})
				return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2, Output: string(raw)}, nil
			})
			if err := (&PlanningWorker{Service: f.s, Executor: executor}).Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := failureCases(t, f.s)[0]
			if got.State != "stale" && got.State != "needs_owner" {
				t.Fatal("stale recovery applied", got)
			}
			var task domain.Task
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("task", source.SourceTask.ID, &task) }); err != nil {
				t.Fatal(err)
			}
			if task.Repairs != source.SourceTask.Repairs || task.HoldReason == "" {
				t.Fatal("late diagnosis changed source", task)
			}
			if f.snapshot(t).PlanningDocument.PlanningUsedSeconds < 4 {
				t.Fatal("stale diagnosis lost cumulative spend")
			}
		})
	}
}

func TestFailureRecoveryPausedAndUnknownTreesNeverDispatch(t *testing.T) {
	for _, change := range []string{"paused", "disabled", "unknown tree"} {
		t.Run(change, func(t *testing.T) {
			f, source := failureFixture(t)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				switch change {
				case "paused":
					p, err := getProject(tx)
					if err != nil {
						return err
					}
					p.Paused = true
					return tx.Put("project", p.ID, p)
				case "disabled":
					a, err := automation(tx)
					if err != nil {
						return err
					}
					a.Enabled = false
					return tx.Put("automation", "current", a)
				default:
					var r domain.Run
					if err := tx.Get("run", source.SourceRunID, &r); err != nil {
						return err
					}
					r.Quiescent = false
					return tx.Put("run", r.ID, r)
				}
			}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			exec := planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
				calls++
				return PlanningOutcome{}, errors.New("unexpected execution")
			})
			if err := (&PlanningWorker{Service: f.s, Executor: exec}).Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatal("unsafe dispatch")
			}
		})
	}
}

func TestFailureRecoveryCaseIsOwnerOnlyAndCaptureIdempotent(t *testing.T) {
	f, source := failureFixture(t)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error { return captureTaskFailure(tx, f.s, source.SourceTask, "repeated completion") }); err != nil {
		t.Fatal(err)
	}
	if len(failureCases(t, f.s)) != 1 {
		t.Fatal("duplicate failure capture")
	}
	for _, a := range []Actor{f.node, {ID: "interaction", Role: "chat"}} {
		if _, err := (FailureRecoveries{Service: f.s}).Read(context.Background(), a, source.ID); err == nil {
			t.Fatal("private failure context leaked", a)
		}
	}
	got, err := (FailureRecoveries{Service: f.s}).Read(context.Background(), f.owner, source.ID)
	if err != nil || got.SourceRunID != source.SourceRunID {
		t.Fatal("Owner cannot inspect exact source", err)
	}
}

func TestFailureRecoveryPlannerRetriesAreFiniteAndReboundToCase(t *testing.T) {
	f, _ := failureFixture(t)
	attempts := 0
	executor := planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		attempts++
		return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2}, errors.New("fixture transport failure")
	})
	w := &PlanningWorker{Service: f.s, Executor: executor}
	for i := 0; i < 5; i++ {
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got := failureCases(t, f.s)[0]
	if attempts != 3 || got.State != "needs_owner" {
		t.Fatal("retries unbounded or lost source binding", attempts, got)
	}
	if len(f.snapshot(t).Tasks) != 2 {
		t.Fatal("transport failure minted replacement tasks")
	}
}

func TestFailureRecoveryFollowupDoesNotRefreshExhaustedSourceAllowance(t *testing.T) {
	f, source := failureFixture(t)
	got := runRecovery(t, f, followupPlan(source))
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var child domain.Task
		if err := tx.Get("task", got.FollowupTaskID, &child); err != nil {
			return err
		}
		child.State = "integrated"
		child.Version++
		if err := tx.Put("task", child.ID, child); err != nil {
			return err
		}
		// The original allowance was already exhausted when diagnosis began.
		var task domain.Task
		if err := tx.Get("task", source.SourceTask.ID, &task); err != nil {
			return err
		}
		task.Repairs = task.MaxRepairs
		source.SourceTask.Repairs = task.Repairs
		got.SourceTask.Repairs = task.Repairs
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		return tx.Put("failure_assessment", got.ID, got)
	}); err != nil {
		t.Fatal(err)
	}
	if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got = failureCases(t, f.s)[0]
	if got.State != "needs_owner" {
		t.Fatal("follow-up reset source allowance", got)
	}
}

func TestFailureRecoveryVerificationRepinsExactLandedCodeButNotChangedTestPolicy(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same suite", true: "changed suite"}[changed], func(t *testing.T) {
			f, source := failureFixture(t)
			suite := domain.TestSuite{ID: "smoke", Version: 1, Name: "Service smoke", Enabled: true, Stage: "project", Checks: []domain.Check{{Name: "probe", Command: "probe", TimeoutSeconds: 3}}}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				source.SourceTask.Verification = &domain.VerificationSpec{Suite: suite, Commit: strings.Repeat("a", 40), Digest: suiteDigest(suite)}
				source.SourceTask.HoldReason = "tests_failed"
				var failed domain.Run
				if err := tx.Get("run", source.SourceRunID, &failed); err != nil {
					return err
				}
				failed.Kind, failed.State, failed.Base = "verification", "failed", source.SourceTask.Verification.Commit
				failed.TestReport = &domain.TestReport{Commit: failed.Base, SuiteID: suite.ID, SuiteVersion: suite.Version, Digest: suiteDigest(suite)}
				if err := tx.Put("run", failed.ID, failed); err != nil {
					return err
				}

				if err := tx.Put("task", source.SourceTask.ID, source.SourceTask); err != nil {
					return err
				}
				if err := tx.Put("test_suite", suite.ID, suite); err != nil {
					return err
				}
				return tx.Put("failure_assessment", source.ID, source)
			}); err != nil {
				t.Fatal(err)
			}
			got := runRecovery(t, f, followupPlan(source))
			landed := strings.Repeat("e", 40)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var child domain.Task
				if err := tx.Get("task", got.FollowupTaskID, &child); err != nil {
					return err
				}
				child.State = "integrated"
				child.CurrentCandidateID = "landed"
				child.Version++
				if err := tx.Put("task", child.ID, child); err != nil {
					return err
				}
				if err := tx.Put("candidate", "landed", domain.Candidate{ID: "landed", TaskID: child.ID, State: "landed", Commit: landed}); err != nil {
					return err
				}
				if changed {
					suite.Version++
					return tx.Put("test_suite", suite.ID, suite)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
				t.Fatal(err)
			}
			got = failureCases(t, f.s)[0]
			var task domain.Task
			if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("task", source.SourceTask.ID, &task) }); err != nil {
				t.Fatal(err)
			}
			if changed {
				if got.State != "needs_owner" || task.Verification.Commit == landed || task.Retries != 0 {
					t.Fatal("test suite silently changed", got, task)
				}
			} else if got.State != "applied" || task.Verification.Commit != landed || task.Retries != 1 || task.Repairs != source.SourceTask.Repairs {
				t.Fatal("verification used wrong candidate or counters", got, task)
			}
		})
	}
}
