package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
)

func TestNativeStopDispositionIsOnlyANoGrantHoldAndNeverStopProof(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown tree", true: "known stop missing receipt"}[quiet], func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			scheduleTest(t, f, d, "intent-plan")
			w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
			if err := w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			a := f.claim(t, "intent-claim")
			if a == nil {
				t.Fatal("missing assignment")
			}
			req := domain.CompleteRequest{Generation: a.Run.Generation, Status: "stopped", Quiescent: quiet, ElapsedSeconds: 3, IntentStatus: "scope_requested", Detail: "No receipt available"}
			b, err := f.s.Complete(context.Background(), f.node, "intent-stop", a.Run.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := f.s.Complete(context.Background(), f.node, "intent-stop", a.Run.ID, req)
			if err != nil || string(b) != string(replay) {
				t.Fatal("native stop replay changed", err)
			}
			snap := f.snapshot(t)
			var task domain.Task
			for _, v := range snap.Tasks {
				if v.ID == a.Task.ID {
					task = v
				}
			}
			run := recoveryRun(t, snap, a.Run.ID)
			if len(snap.ScopeRequests) != 0 || len(snap.Candidates) != 0 || task.HoldReason != "human_decision" || task.Retries != 0 || task.Repairs != 0 || run.Quiescent != quiet {
				t.Fatal("no-grant marker invented proof/authority/retry", task, run)
			}
			if !quiet && (run.State != "lost" || task.UsedSeconds != run.GrantSeconds) {
				t.Fatal("intent marker released unknown-tree fence or refunded grant", task, run)
			}
			if quiet && (run.State != "stopped" || task.UsedSeconds != 3) {
				t.Fatal("native quiet stop accounting lost", task, run)
			}
		})
	}
}

func TestNativeStopDispositionRejectsSuccessAndUnknownValues(t *testing.T) {
	f := newServiceFixture(t)
	a := f.claim(t, "intent-invalid-claim")
	for _, req := range []domain.CompleteRequest{
		{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, IntentStatus: "scope_requested"},
		{Generation: a.Run.Generation, Status: "stopped", Quiescent: true, IntentStatus: "approved"},
	} {
		if _, err := f.s.Complete(context.Background(), f.node, ID("invalid-intent-stop"), a.Run.ID, req); err == nil {
			t.Fatal("invalid marker accepted", req)
		}
	}
}
