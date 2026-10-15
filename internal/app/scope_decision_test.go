package app

import (
	"context"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestCompletedScopeDecisionRetiresWithoutGrantingAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, state, kind, currentRun string
		quiet                         bool
		retired                       bool
	}{
		{"integrated", "integrated", "scope", "", true, true},
		{"verified", "verified", "scope", "", true, true},
		{"working", "executing", "scope", "", true, false},
		{"cancelled", "cancelled", "scope", "", true, false},
		{"unknown-tree", "integrated", "scope", "", false, false},
		{"current-run", "integrated", "scope", "run_current", true, false},
		{"stop-decision", "integrated", "unknown_live", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, d, _ := planningFixture(t, false)
			ctx := context.Background()
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				var task domain.Task
				if err := tx.Get("task", f.task.ID, &task); err != nil {
					return err
				}
				// The decision predates the Owner's scope revision and completion.
				if err := f.s.decision(tx, "task", task.ID, task.Version, d, tc.kind, "scope was requested", []string{"dismiss", "edit_scope"}); err != nil {
					return err
				}
				task.State, task.CurrentRunID = tc.state, tc.currentRun
				task.Version++
				task.AuthorizationVersion++
				if err := tx.Put("task", task.ID, task); err != nil {
					return err
				}
				return tx.Put("run", "run_scope_history", domain.Run{ID: "run_scope_history", TaskID: task.ID, State: "stopped", Quiescent: tc.quiet})
			}); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			w := &PlanningWorker{Service: f.s}
			if err := w.queueScopes(ctx); err != nil {
				t.Fatal(err)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) || !reflect.DeepEqual(before.Project, after.Project) {
				t.Fatal("retirement changed execution, accounting or authority")
			}
			old, current := before.Decisions[0], after.Decisions[0]
			if tc.retired {
				if current.Status != "superseded" || current.Version != old.Version+1 || current.SourceVersion != old.SourceVersion || len(after.Events) != len(before.Events)+1 {
					t.Fatal("missing versioned history and audit", current)
				}
				if _, err := f.s.ResolveDecision(ctx, f.owner, "late-scope-action", old.ID, domain.DecisionResolveRequest{ExpectedVersion: old.Version, ExpectedSourceVersion: old.SourceVersion, Action: "edit_scope", Evidence: "obsolete browser form"}); err == nil {
					t.Fatal("retired decision accepted a late Owner action")
				}
			} else if !reflect.DeepEqual(before.Decisions, after.Decisions) || len(after.Events) != len(before.Events) {
				t.Fatal("retired an unresolved or non-scope decision")
			}
			if err := w.queueScopes(ctx); err != nil {
				t.Fatal(err)
			}
			again := f.snapshot(t)
			if !reflect.DeepEqual(after.Decisions, again.Decisions) || len(after.Events) != len(again.Events) {
				t.Fatal("unchanged tick repeated retirement or audit")
			}
		})
	}
}
