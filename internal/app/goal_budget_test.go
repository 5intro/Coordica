package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"strings"
	"testing"
	"time"
)

func TestExistingGrantCanFillRemainingGoalBudget(t *testing.T) {
	for _, kind := range []string{"implement", "validation"} {
		for _, scenario := range []struct {
			name          string
			wall, elapsed int64
			accepted      bool
		}{{"quick", 1, 1, true}, {"deadline", 30, 30, false}, {"reported-final", 1, 30, true}} {
			if kind == "validation" && scenario.name == "reported-final" {
				continue
			}
			elapsed := scenario.elapsed
			name := kind + "/" + scenario.name
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				f, d, result := planningFixture(t, true)
				scheduleTest(t, f, d, "budget-plan")
				worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
				if err := worker.Tick(ctx); err != nil {
					t.Fatal(err)
				}
				if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					var goal domain.PlanningDocument
					if err := tx.Get("planning_document", "current", &goal); err != nil {
						return err
					}
					goal.BudgetSeconds = goal.UsedSeconds + 30
					return tx.Put("planning_document", "current", goal)
				}); err != nil {
					t.Fatal(err)
				}
				a := f.claim(t, "whole-remaining-grant")
				if a == nil || a.Run.GrantSeconds != 30 {
					t.Fatal("no full remaining grant", a)
				}
				before := f.snapshot(t)
				if before.PlanningDocument.UsedSeconds+before.PlanningDocument.ReservedSeconds != before.PlanningDocument.BudgetSeconds {
					t.Fatal("fixture did not fill goal budget")
				}
				var cand domain.Candidate
				if kind == "validation" {
					cand = domain.Candidate{BaselineVersion: a.Run.BaselineVersion, BaselineDigest: a.Run.BaselineDigest, ID: "fixed-goal-budget-candidate", Version: 1, TaskID: a.Task.ID, State: "validating", Commit: strings.Repeat("c", 40), AuthorizationVersion: a.Task.AuthorizationVersion, PolicyVersion: a.Project.PolicyVersion, Review: &domain.Review{Candidate: strings.Repeat("c", 40), Verdict: "approve", Summary: "Fixed candidate approved", Findings: []string{}}}
					a.Run.Kind, a.Run.CandidateID, a.Run.Base = "validation", cand.ID, cand.Commit
					if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
						var task domain.Task
						if err := tx.Get("task", a.Task.ID, &task); err != nil {
							return err
						}
						task.State, task.CurrentCandidateID = "awaiting_integration", cand.ID
						if err := tx.Put("task", task.ID, task); err != nil {
							return err
						}
						if err := tx.Put("candidate", cand.ID, cand); err != nil {
							return err
						}
						return tx.Put("run", a.Run.ID, a.Run)
					}); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "implement" {
					artifact, err := f.s.UploadArtifact(ctx, f.node, a.Run.ID, strings.NewReader("immutable fixture bundle"))
					if err != nil {
						t.Fatal(err)
					}
					f.now = f.now.Add(time.Duration(scenario.wall) * time.Second)
					if _, err := f.s.Complete(ctx, f.node, "budget-complete", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Head: strings.Repeat("b", 40), ArtifactID: artifact.ID, Quiescent: true, ElapsedSeconds: elapsed}); err != nil {
						t.Fatal(err)
					}
				} else {
					f.now = f.now.Add(time.Duration(scenario.wall) * time.Second)
					integrator := &Integrator{Service: f.s}
					if err := integrator.finishValidation(ctx, a.Run, cand, []domain.CheckResult{{Name: "git", Passed: true}}, nil, true, false); err != nil {
						t.Fatal(err)
					}
				}
				after := f.snapshot(t)
				goal := after.PlanningDocument
				if goal.ReservedSeconds != 0 || goal.UsedSeconds != before.PlanningDocument.UsedSeconds+elapsed || after.Project.ReservedSeconds != 0 || after.Project.UsedSeconds != before.Project.UsedSeconds+elapsed {
					t.Fatal("reservation refunded spent work or lost release", goal, after.Project)
				}
				var run domain.Run
				var task domain.Task
				for _, r := range after.Runs {
					if r.ID == a.Run.ID {
						run = r
					}
				}
				for _, q := range after.Tasks {
					if q.ID == a.Task.ID {
						task = q
					}
				}
				if run.ElapsedSeconds != elapsed || task.UsedSeconds != elapsed || task.ReservedSeconds != 0 {
					t.Fatal("cumulative grant settlement wrong", run, task)
				}
				if scenario.accepted {
					if run.State != "succeeded" || task.HoldReason != "" || len(after.Candidates) != 1 {
						t.Fatal("legitimate filled reservation lost authority", run, task)
					}
					if kind == "validation" && after.Candidates[0].State != "ready_to_land" {
						t.Fatal("validation candidate not accepted", after.Candidates)
					}
				} else if run.State == "succeeded" {
					t.Fatal("expired grant accepted", run)
				}
				if elapsed == 30 {
					if goal.UsedSeconds != goal.BudgetSeconds {
						t.Fatal("deadline/exhaustion mishandled", run, goal)
					}
					// Remove the ordinary hold so availability itself must prevent admission.
					if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
						task.HoldReason, task.CurrentRunID = "", ""
						if kind == "implement" {
							task.State = "ready"
						} else {
							cand.State, cand.Review = "reviewing", nil
							if err := tx.Put("candidate", cand.ID, cand); err != nil {
								return err
							}
						}
						return tx.Put("task", task.ID, task)
					}); err != nil {
						t.Fatal(err)
					}
					if next := f.claim(t, "after-goal-exhaustion"); next != nil {
						t.Fatal("new work admitted after goal exhaustion", next)
					}
					final := f.snapshot(t)
					if len(final.Runs) != len(after.Runs) || final.PlanningDocument.ReservedSeconds != 0 || final.PlanningDocument.UsedSeconds != goal.UsedSeconds {
						t.Fatal("new reservation or refund after exhaustion")
					}
				}
			})
		}
	}
}

func TestOldCandidateResumeAndReviewClaimCannotRebindGoal(t *testing.T) {
	ctx := context.Background()
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "old-candidate-plan")
	worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	for _, q := range f.snapshot(t).Tasks {
		if q.GoalVersion == d.Version {
			task = q
		}
	}
	cand := domain.Candidate{BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ID: "old-review-candidate", Version: 1, TaskID: task.ID, State: "reviewing", Commit: strings.Repeat("c", 40), AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		task.CurrentCandidateID, task.State = cand.ID, "awaiting_integration"
		if err := tx.Put("candidate", cand.ID, cand); err != nil {
			return err
		}
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	d.Architecture = "Changed same-root architecture"
	b, err := f.s.SavePlanning(ctx, f.owner, "new-candidate-goal", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
	d = decodeTest[domain.PlanningDocument](t, b, err)
	b, err = f.s.AuthorizePlanning(ctx, f.owner, "new-candidate-auth", domain.VersionRequest{ExpectedVersion: d.Version})
	d = decodeTest[domain.PlanningDocument](t, b, err)
	result.Tasks = []domain.TaskProposal{}
	scheduleTest(t, f, d, "new-candidate-admission")
	worker.Executor = resultExecutor(result)
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	for _, q := range before.Tasks {
		if q.ID == task.ID {
			task = q
		}
	}
	if _, err := f.s.TaskAction(ctx, f.owner, "old-candidate-resume", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); err == nil {
		t.Fatal("resume rebound old candidate to new goal")
	}
	for _, q := range f.snapshot(t).Tasks {
		if q.ID == task.ID && (q.HoldReason != "goal_changed" || q.Version != task.Version || q.GoalVersion != task.GoalVersion) {
			t.Fatal("rejected resume changed authority", q)
		}
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { task.HoldReason = ""; return tx.Put("task", task.ID, task) }); err != nil {
		t.Fatal(err)
	}
	if next := f.claim(t, "old-candidate-review"); next != nil {
		t.Fatal("review claimed old goal candidate", next)
	}
	after := f.snapshot(t)
	if len(after.Runs) != len(before.Runs) || after.Project.ReservedSeconds != before.Project.ReservedSeconds || after.PlanningDocument.ReservedSeconds != before.PlanningDocument.ReservedSeconds {
		t.Fatal("stale candidate created run/reservation")
	}
}
