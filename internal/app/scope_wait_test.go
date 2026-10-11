package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func scopeWait(t *testing.T, snap domain.Snapshot, id string) domain.Decision {
	t.Helper()
	var found domain.Decision
	for _, v := range snap.Decisions {
		if v.SourceKind == "scope_request" && v.SourceID == id && v.Kind == "scope_wait_stop" && v.Status == "open" {
			if found.ID != "" {
				t.Fatal("duplicate open scope stop prerequisite")
			}
			found = v
		}
	}
	return found
}

func TestScopeWaitRepeatedTicksReuseExactPrerequisiteAndRefreshRunEvidence(t *testing.T) {
	f, w, a, scope := scopeFixture(t)
	ctx := context.Background()
	f.now = a.Run.LeaseUntil.Add(time.Second)
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	first := scopeWait(t, before, scope.ID)
	if first.ID == "" || !reflect.DeepEqual(first.Actions, []string{"inspect_run"}) {
		t.Fatal("offered impossible stop mutation", first)
	}
	for i := 0; i < 5; i++ {
		if err := w.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(first, scopeWait(t, after, scope.ID)) || len(before.Decisions) != len(after.Decisions) || len(before.Events) != len(after.Events) {
		t.Fatal("unchanged Tick duplicated/updated/audited stop prerequisite", before.Decisions, after.Decisions)
	}
	var old domain.Run
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		if err := tx.Get("run", a.Run.ID, &old); err != nil {
			return err
		}
		old.Version++
		return tx.Put("run", old.ID, old)
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	refreshed := scopeWait(t, f.snapshot(t), scope.ID)
	if refreshed.ID != first.ID || refreshed.Version != first.Version+1 || refreshed.SourceVersion != scope.Version || !strings.Contains(refreshed.Evidence, fmt.Sprintf("expected_version=%d", old.Version)) {
		t.Fatal("did not refresh exact original Run proof version", refreshed)
	}
	events := len(f.snapshot(t).Events)
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.snapshot(t).Events) != events {
		t.Fatal("same bound Run evidence repeated audit")
	}
	for _, action := range []string{"inspect_run", "confirm_run_stopped", "dismiss"} {
		if _, err := f.s.ResolveDecision(ctx, f.owner, ID("false-stop"), refreshed.ID, domain.DecisionResolveRequest{ExpectedVersion: refreshed.Version, ExpectedSourceVersion: refreshed.SourceVersion, Action: action, Evidence: "Prose is not original Run inspection"}); err == nil {
			t.Fatal("wait source navigation mutated stop prerequisite", action)
		}
	}
	if _, err := f.s.ConfirmStopped(ctx, f.owner, "actual-stop", old.ID, domain.StopConfirmation{ExpectedVersion: old.Version, Evidence: "Inspected exact original process tree; all stopped"}); err != nil {
		t.Fatal(err)
	}
	// Observe queueing separately from assessment; the wait decision must close
	// even when the Leader has not yet produced a result.
	if err := w.queueScopes(ctx); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	if scopeWait(t, snap, scope.ID).ID != "" || snap.ScopeRequests[0].State != "queued" {
		t.Fatal("real stop did not close prerequisite when queuing assessment", snap.Decisions)
	}
	for _, v := range snap.Decisions {
		if v.ID == first.ID && (v.Status != "resolved" || v.Version != refreshed.Version+1) {
			t.Fatal("wait did not close exactly once", v)
		}
	}
}

func TestScopeWaitAuthorityChangeSupersedesUnknownStopPrerequisite(t *testing.T) {
	f, w, a, scope := scopeFixture(t)
	ctx := context.Background()
	f.now = a.Run.LeaseUntil.Add(time.Second)
	if err := f.s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	wait := scopeWait(t, f.snapshot(t), scope.ID)
	if wait.ID == "" {
		t.Fatal("missing stop prerequisite")
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", a.Task.ID, &task); err != nil {
			return err
		}
		task.AuthorizationVersion++
		task.Version++
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snap := f.snapshot(t)
	if snap.ScopeRequests[0].State != "blocked" || scopeWait(t, snap, scope.ID).ID != "" {
		t.Fatal("changed authority kept impossible open prerequisite", snap)
	}
	for _, v := range snap.Decisions {
		if v.ID == wait.ID && v.Status != "superseded" {
			t.Fatal("changed authority did not supersede stop prerequisite", v)
		}
	}
	events, decisions := len(snap.Events), len(snap.Decisions)
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snap = f.snapshot(t)
	if len(snap.Events) != events || len(snap.Decisions) != decisions {
		t.Fatal("blocked source repeated decisions/audits")
	}
}
