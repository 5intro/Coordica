package app

import (
	"context"
	"errors"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func preparationFailure(t *testing.T) *itFixture {
	t.Helper()
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	c := f.candidate(t)
	// Recreate a failure after Main invalidation, before another fixed C exists.
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		c.Main = ""
		c.Commit = ""
		c.Review = nil
		c.Checks = nil
		c.CheckPlan = nil
		c.CheckRunID = ""
		return tx.Put("candidate", c.ID, c)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.w.reject(context.Background(), f.candidate(t), errors.New("fixture program preparation failure"), false); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPreparationRetryKeepsStoppedMaterialAndFiniteAccounting(t *testing.T) {
	f := preparationFailure(t)
	before := f.snapshot(t).Tasks[0]
	c := f.candidate(t)
	ctx := context.Background()
	raw, err := f.s.TaskAction(ctx, f.owner, "retry-prepare", before.ID, "retry-integration", domain.VersionRequest{ExpectedVersion: before.Version})
	got := itDecode[domain.Task](t, raw, err)
	if got.Retries != before.Retries+1 || got.Repairs != before.Repairs || got.UsedSeconds != before.UsedSeconds || got.ReservedSeconds != before.ReservedSeconds || got.CurrentCandidateID != c.ID || got.HoldReason != "" {
		t.Fatal("retry changed accounting/material", before, got)
	}
	_, err = f.s.TaskAction(ctx, f.owner, "retry-prepare", before.ID, "retry-integration", domain.VersionRequest{ExpectedVersion: before.Version})
	if err != nil {
		t.Fatal("exact replay failed", err)
	}
	if f.snapshot(t).Tasks[0].Retries != got.Retries {
		t.Fatal("duplicate consumed another retry")
	}
	if err = f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	after := f.candidate(t)
	if after.State != "prechecking" || after.Commit == "" || after.Head != c.Head || after.SourceRunID != c.SourceRunID || after.Review != nil {
		t.Fatal("retry did not recreate unchecked fixed candidate", after)
	}
	if len(f.snapshot(t).Runs) != 2 {
		t.Fatal("retry created another implementation Run")
	}
}

func TestPreparationRetryRejectsStaleUnsafeAndUnboundedRequests(t *testing.T) {
	for _, scenario := range []string{"stale", "node", "retry_limit", "budget", "unknown_stop", "authority", "fixed_candidate", "missing_material", "unknown_preparation_stop"} {
		t.Run(scenario, func(t *testing.T) {
			f := preparationFailure(t)
			task := f.snapshot(t).Tasks[0]
			actor := f.owner
			expected := task.Version
			if scenario == "stale" {
				expected--
			}
			if scenario == "node" {
				actor = f.node
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var current domain.Task
				if err := tx.Get("task", task.ID, &current); err != nil {
					return err
				}
				var c domain.Candidate
				if err := tx.Get("candidate", current.CurrentCandidateID, &c); err != nil {
					return err
				}
				switch scenario {
				case "unknown_preparation_stop":
					c.PreparationStopped = false
					if err := tx.Put("candidate", c.ID, c); err != nil {
						return err
					}
				case "retry_limit":
					current.Retries = current.MaxRetries
				case "budget":
					current.UsedSeconds = current.BudgetSeconds
				case "authority":
					current.AuthorizationVersion++
				case "fixed_candidate":
					c.Commit = c.Head
					if err := tx.Put("candidate", c.ID, c); err != nil {
						return err
					}
				case "missing_material":
					if err := tx.Delete("candidate_artifact", c.ID); err != nil {
						return err
					}
				case "unknown_stop":
					var r domain.Run
					if err := tx.Get("run", c.SourceRunID, &r); err != nil {
						return err
					}
					r.State = "lost"
					r.Quiescent = false
					if err := tx.Put("run", r.ID, r); err != nil {
						return err
					}
				}
				return tx.Put("task", current.ID, current)
			}); err != nil {
				t.Fatal(err)
			}
			_, err := f.s.TaskAction(context.Background(), actor, "denied-retry", task.ID, "retry-integration", domain.VersionRequest{ExpectedVersion: expected})
			if err == nil {
				t.Fatal("unsafe preparation retry accepted")
			}
			if f.candidate(t).State != "rejected" {
				t.Fatal("denied request mutated candidate")
			}
		})
	}
}

func TestPreparationStopConfirmationDoesNotApproveCodeOrClearRunFences(t *testing.T) {
	f := preparationFailure(t)
	ctx := context.Background()
	task := f.snapshot(t).Tasks[0]
	c := f.candidate(t)
	c.PreparationStopped = false
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("candidate", c.ID, c) }); err != nil {
		t.Fatal(err)
	}
	req := domain.StopConfirmation{ExpectedVersion: task.Version, Evidence: "Fixture observer verified the original preparation process exited"}
	if _, err := f.s.ConfirmPreparationStopped(ctx, f.node, "node-stop", task.ID, req); err == nil {
		t.Fatal("node asserted Owner stop proof")
	}
	if _, err := f.s.ConfirmPreparationStopped(ctx, f.owner, "empty-stop", task.ID, domain.StopConfirmation{ExpectedVersion: task.Version}); err == nil {
		t.Fatal("empty stop evidence accepted")
	}
	raw, err := f.s.ConfirmPreparationStopped(ctx, f.owner, "owner-stop", task.ID, req)
	observed := itDecode[domain.Task](t, raw, err)
	if observed.HoldReason != task.HoldReason || observed.Retries != task.Retries || !f.candidate(t).PreparationStopped || f.candidate(t).Review != nil {
		t.Fatal("stop observation granted authority or review")
	}
	_, err = f.s.TaskAction(ctx, f.owner, "after-stop-retry", task.ID, "retry-integration", domain.VersionRequest{ExpectedVersion: observed.Version})
	if err != nil {
		t.Fatal(err)
	}
	if f.candidate(t).PreparationStopped || f.candidate(t).State != "accepted" {
		t.Fatal("new attempt retained old stop receipt")
	}
	if _, err = f.s.ConfirmPreparationStopped(ctx, f.owner, "owner-stop", task.ID, req); err != nil {
		t.Fatal("exact observation replay failed", err)
	}
	if f.candidate(t).PreparationStopped {
		t.Fatal("late observation mutated new preparation")
	}
}
