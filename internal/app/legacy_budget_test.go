package app

import (
	"context"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLegacyProjectCapHoldRequiresExplicitSafeOwnerResume(t *testing.T) {
	for _, scenario := range []string{"safe", "task_exhausted", "goal_exhausted", "stale_goal", "stale_policy", "stale_task", "stale_generation", "missing_run", "nonquiescent", "unsettled", "retries_exhausted", "other_unknown"} {
		t.Run(scenario, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			ctx := context.Background()
			scheduleTest(t, f, d, "admit")
			if e := (&PlanningWorker{Service: f.s, Executor: resultExecutor(result)}).Tick(ctx); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			var task domain.Task
			for _, v := range snap.Tasks {
				if v.GoalVersion == d.Version {
					task = v
				}
			}
			if task.ID == "" {
				t.Fatal("no admitted task")
			}
			task.State, task.HoldReason, task.CurrentRunID, task.Generation = "executing", "budget", "run_legacy", 1
			task.UsedSeconds, task.ReservedSeconds = 10, 0
			r := domain.Run{BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, ID: "run_legacy", Version: 2, TaskID: task.ID, State: "stopped", Kind: "implement", Quiescent: true, FinishedAt: &f.now, Generation: task.Generation, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: snap.Project.PolicyVersion, GoalVersion: task.GoalVersion, GoalAuthorizationVersion: task.GoalAuthorizationVersion, GrantSeconds: 10, ElapsedSeconds: 10}
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				p, e := getProject(tx)
				if e != nil {
					return e
				}
				p.TotalBudgetSeconds, p.UsedSeconds, p.ReservedSeconds = 10, 10, 0
				var goal domain.PlanningDocument
				if e = tx.Get("planning_document", "current", &goal); e != nil {
					return e
				}
				switch scenario {
				case "task_exhausted":
					task.UsedSeconds = task.BudgetSeconds
				case "goal_exhausted":
					goal.UsedSeconds = goal.BudgetSeconds
				case "stale_goal":
					task.GoalVersion--
				case "stale_policy":
					r.PolicyVersion++
				case "stale_task":
					r.AuthorizationVersion++
				case "stale_generation":
					r.Generation++
				case "missing_run":
					task.CurrentRunID = "run_missing"
				case "nonquiescent":
					r.Quiescent = false
				case "unsettled":
					task.ReservedSeconds = 1
				case "retries_exhausted":
					task.Retries = task.MaxRetries
				case "other_unknown":
					old := r
					old.ID, old.Generation, old.State, old.Quiescent = "run_unknown", 0, "lost", false
					if e = tx.Put("run", old.ID, old); e != nil {
						return e
					}
				}
				for _, record := range []struct {
					kind, id string
					value    any
				}{{"project", p.ID, p}, {"planning_document", "current", goal}, {"task", task.ID, task}, {"run", r.ID, r}} {
					if e = tx.Put(record.kind, record.id, record.value); e != nil {
						return e
					}
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			// Background recovery cannot establish which old cap caused the hold.
			if e := f.s.Expire(ctx); e != nil {
				t.Fatal(e)
			}
			before := f.snapshot(t)
			for _, v := range before.Tasks {
				if v.ID == task.ID {
					task = v
				}
			}
			if task.HoldReason != "budget" {
				t.Fatal("unproven automatic migration")
			}
			counts := chatMutationCounts(t, f)
			raw, e := f.s.TaskAction(ctx, f.owner, "upgrade_resume", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version})
			if scenario != "safe" {
				if e == nil {
					t.Fatal("unsafe legacy resume", scenario)
				}
				if !reflect.DeepEqual(before, f.snapshot(t)) || !reflect.DeepEqual(counts, chatMutationCounts(t, f)) {
					t.Fatal("rejected resume mutated state")
				}
				return
			}
			resumed := decodeTest[domain.Task](t, raw, e)
			if resumed.HoldReason != "" || resumed.State != "ready" || resumed.UsedSeconds != task.UsedSeconds || resumed.Retries != task.Retries+1 || resumed.CurrentRunID != "" {
				t.Fatal("unsafe counters/state", resumed)
			}
			if after := f.snapshot(t); !reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.Project, after.Project) {
				t.Fatal("resume invented stop evidence or reset project statistics")
			}
		})
	}
}
