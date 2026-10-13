package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
	"strings"
	"testing"
	"time"
)

func TestSameRootGoalChangeHoldsEveryCandidateAndStopsItsRuns(t *testing.T) {
	for _, state := range []string{"accepted", "reviewing", "validating", "ready_to_land"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			f, d, result := planningFixture(t, true)
			scheduleTest(t, f, d, "candidate-initial")
			worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
			if e := worker.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			var task domain.Task
			for _, q := range f.snapshot(t).Tasks {
				if q.GoalVersion == d.Version {
					task = q
				}
			}
			cand := domain.Candidate{BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ID: "fixed-candidate", Version: 1, TaskID: task.ID, Base: strings.Repeat("a", 40), Head: strings.Repeat("b", 40), Commit: strings.Repeat("c", 40), State: state, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1}
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				task.State = "awaiting_integration"
				task.CurrentCandidateID = cand.ID
				if state == "reviewing" || state == "validating" {
					kind := "review"
					if state == "validating" {
						kind = "validation"
					}
					task.CurrentRunID = "old-candidate-run"
					task.Generation++
					task.ReservedSeconds = 10
					r := domain.Run{BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ID: task.CurrentRunID, Version: 1, TaskID: task.ID, NodeID: f.node.NodeID, Kind: kind, State: "running", Generation: task.Generation, GoalVersion: task.GoalVersion, GoalAuthorizationVersion: task.GoalAuthorizationVersion, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1, GrantSeconds: 10, LeaseUntil: f.now.Add(time.Minute), Deadline: f.now.Add(time.Minute), CandidateID: cand.ID, Base: cand.Commit}
					if e := tx.Put("run", r.ID, r); e != nil {
						return e
					}
					if e := reserveGoal(tx, task, 10); e != nil {
						return e
					}
					p, e := getProject(tx)
					if e != nil {
						return e
					}
					p.ReservedSeconds += 10
					if e = tx.Put("project", p.ID, p); e != nil {
						return e
					}
				}
				if e := tx.Put("candidate", cand.ID, cand); e != nil {
					return e
				}
				return tx.Put("task", task.ID, task)
			}); e != nil {
				t.Fatal(e)
			}
			d.Architecture = "TypeScript modules"
			d.Acceptance = "New TypeScript acceptance"
			b, e := f.s.SavePlanning(ctx, f.owner, "same-root-goal", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
			d = decodeTest[domain.PlanningDocument](t, b, e)
			snap := f.snapshot(t)
			for _, q := range snap.Tasks {
				if q.ID == task.ID && (q.HoldReason != "goal_changed" || q.CurrentCandidateID != cand.ID) {
					t.Fatal("candidate-bearing task escaped goal hold")
				}
			}
			if snap.Candidates[0].Commit != cand.Commit || snap.Candidates[0].State != state {
				t.Fatal("candidate history destroyed")
			}
			for _, r := range snap.Runs {
				if r.TaskID == task.ID && r.State != "cancel_requested" {
					t.Fatal("old candidate run not stopped")
				}
			}
			b, e = f.s.AuthorizePlanning(ctx, f.owner, "same-root-authorize", domain.VersionRequest{ExpectedVersion: d.Version})
			d = decodeTest[domain.PlanningDocument](t, b, e)
			result.Tasks = []domain.TaskProposal{}
			scheduleTest(t, f, d, "same-root-evaluate")
			worker.Executor = resultExecutor(result)
			if e = worker.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			// Clear the display hold as a hostile/stale caller might: exact admitted goal
			// versions must still independently fence every new authority boundary.
			if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
				var q domain.Task
				if e := tx.Get("task", task.ID, &q); e != nil {
					return e
				}
				q.HoldReason = ""
				q.CurrentRunID = ""
				return tx.Put("task", q.ID, q)
			}); e != nil {
				t.Fatal(e)
			}
			integrator := &Integrator{Service: f.s, Git: gitops.New("")}
			eligible, e := integrator.eligible(ctx, cand)
			if e != nil || eligible {
				t.Fatal("same roots admitted old candidate authority", e)
			}
			if e = integrator.prepare(ctx, cand); e == nil || !strings.Contains(e.Error(), "older goal") {
				t.Fatal("prepare did not check exact goal authority", e)
			}
			if state == "validating" {
				if e = integrator.validate(ctx, cand); e == nil {
					t.Fatal("validation admitted old authority")
				}
			}
			if state == "reviewing" {
				if a := f.claim(t, "same-root-review-claim"); a != nil {
					t.Fatal("review claim admitted old goal")
				}
			}
		})
	}
}

func TestCandidateBlockDisplayUsesCurrentLiveAuthority(t *testing.T) {
	ctx := context.Background()
	f := newServiceFixture(t)
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		task := f.task
		task.CurrentCandidateID = "current"
		task.State = "awaiting_integration"
		if e := tx.Put("task", task.ID, task); e != nil {
			return e
		}
		for _, c := range []domain.Candidate{
			{BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ID: "current", TaskID: task.ID, State: "ready_to_land", Commit: strings.Repeat("b", 40), Protected: true, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1},
			{ID: "superseded", TaskID: task.ID, State: "reviewing", Commit: strings.Repeat("c", 40), Protected: true, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1},
			{ID: "landed", TaskID: task.ID, State: "landed", Commit: strings.Repeat("d", 40), Protected: true, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: 1},
		} {
			if e := tx.Put("candidate", c.ID, c); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	blocks := 0
	for _, b := range snap.Blocks {
		if b.Kind == "candidate" {
			blocks++
			if b.ObjectID != "current" {
				t.Fatal("historical candidate blocked display")
			}
		}
	}
	if blocks != 1 {
		t.Fatal("live candidate absent")
	}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.PolicyVersion++
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	for _, b := range f.snapshot(t).Blocks {
		if b.Kind == "candidate" {
			t.Fatal("stale policy approval displayed")
		}
	}
}
