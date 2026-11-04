package runtime

import (
	"errors"
	"reflect"
	"testing"
)

func readFixture(t *testing.T, f ledgerFixture, id string) TaskSnapshot {
	t.Helper()
	result, err := f.ledger.ReadTask(t.Context(), f.cred, id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func registerFixture(t *testing.T, f ledgerFixture, task TaskSnapshot, key string) RunSnapshot {
	t.Helper()
	run, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: key, TaskID: task.Task.ID,
		ExpectedVersion: task.Task.Version, ExpectedContextVersion: task.Task.ContextVersion, Target: f.target})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestContextSnapshotsAndContinuationNeverRetargetOldRun(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	first := registerFixture(t, f, task, "first-run")
	update := ContextRequest{Key: "context", TaskID: task.Task.ID, ExpectedVersion: 2, ExpectedContextVersion: 1,
		Source: Source{ID: "program", EventID: "context-v2"}, Content: "context-v2"}
	if _, err := f.ledger.UpdateContext(t.Context(), f.cred, update); err != nil {
		t.Fatal(err)
	}
	pinned, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, first.Run.ID)
	if err != nil || pinned.Run != first.Run || pinned.Context.Content != "context-v1" {
		t.Fatalf("context update retargeted old run: %+v %v", pinned, err)
	}
	continuation := ContinuationRequest{Key: "continue", TaskID: task.Task.ID, ExpectedVersion: 3,
		RunID: first.Run.ID, RunGeneration: 1, ContextVersion: 1, Source: Source{ID: "program", EventID: "continue-1"}, Content: "record only"}
	input, err := f.ledger.RecordContinuation(t.Context(), f.cred, continuation)
	if err != nil || input.Disposition != "recorded" || input.ContextID != first.Run.ContextID || input.RunID != first.Run.ID {
		t.Fatalf("continuation fabricated delivery or rebound context: %+v %v", input, err)
	}
	if _, err := f.ledger.CancelRun(t.Context(), f.cred, CancelRequest{Key: "cancel-run", TaskID: task.Task.ID,
		ExpectedVersion: 4, RunID: first.Run.ID, RunGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	f.target.Model = "explicit-second-model"
	second := registerFixture(t, f, readFixture(t, f, task.Task.ID), "second-run")
	if second.Run.Generation != 2 || second.Run.Attempt != 2 || second.Context.Content != "context-v2" || second.Run.Target.Model != f.target.Model {
		t.Fatalf("explicit second registration lost immutable target: %+v", second)
	}
	assertStaleContinuations(t, f, task.Task.ID, continuation, second.Run)
	replayed, err := f.ledger.RecordContinuation(t.Context(), f.cred, continuation)
	if err != nil || !reflect.DeepEqual(replayed, input) {
		t.Fatalf("historical receipt changed or executed again: %+v %v", replayed, err)
	}
	current := readFixture(t, f, task.Task.ID)
	if current.Task.Version != 6 || current.Task.CurrentRunID != second.Run.ID {
		t.Fatalf("old continuation changed current binding: %+v", current.Task)
	}
	reopenFixture(t, &f)
	recovered, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, first.Run.ID)
	if err != nil || recovered.Context.Content != "context-v1" || recovered.Run.State != "cancelled" || recovered.Run.Target != first.Run.Target {
		t.Fatalf("restart lost old snapshot/cancellation: %+v %v", recovered, err)
	}
	replayed, err = f.ledger.RecordContinuation(t.Context(), f.cred, continuation)
	if err != nil || !reflect.DeepEqual(replayed, input) || readFixture(t, f, task.Task.ID).Task != current.Task {
		t.Fatalf("restart replay changed input or task: %+v %v", replayed, err)
	}
}

func assertStaleContinuations(t *testing.T, f ledgerFixture, taskID string, old ContinuationRequest, current Run) {
	t.Helper()
	old.Key, old.Source.EventID, old.ExpectedVersion = "late-old-run", "late-old-run", 6
	if _, err := f.ledger.RecordContinuation(t.Context(), f.cred, old); !errors.Is(err, ErrConflict) {
		t.Fatalf("late input fell into next run: %v", err)
	}
	old.Key, old.Source.EventID, old.RunID, old.ContextVersion = "old-generation", "old-generation", current.ID, current.ContextVersion
	if _, err := f.ledger.RecordContinuation(t.Context(), f.cred, old); !errors.Is(err, ErrConflict) {
		t.Fatalf("old generation was accepted: %v", err)
	}
}

func TestCancelledTaskRejectsNewWorkButReplaysCancellation(t *testing.T) {
	f := fixtureLedger(t)
	task := submitFixture(t, f, "submit")
	run := registerFixture(t, f, task, "run")
	request := CancelRequest{Key: "cancel", TaskID: task.Task.ID, ExpectedVersion: 2}
	cancelled, err := f.ledger.CancelTask(t.Context(), f.cred, request)
	if err != nil || cancelled.State != "cancelled" || cancelled.Version != 3 {
		t.Fatalf("cancel failed: %+v %v", cancelled, err)
	}
	reopenFixture(t, &f)
	again, err := f.ledger.CancelTask(t.Context(), f.cred, request)
	if err != nil || again != cancelled {
		t.Fatalf("lost-ACK cancellation could not replay: %+v %v", again, err)
	}
	if _, err := f.ledger.UpdateContext(t.Context(), f.cred, ContextRequest{Key: "late", TaskID: task.Task.ID,
		ExpectedVersion: 3, ExpectedContextVersion: 1, Source: Source{ID: "event", EventID: "late"}, Content: "late"}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("late input reopened cancelled task: %v", err)
	}
	if _, err := f.ledger.RegisterRun(t.Context(), f.cred, RegisterRunRequest{Key: "late-run", TaskID: task.Task.ID,
		ExpectedVersion: 3, ExpectedContextVersion: 1, Target: f.target}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled task registered new work: %v", err)
	}
	bound, err := f.ledger.ReadRun(t.Context(), f.cred, task.Task.ID, run.Run.ID)
	if err != nil || bound.Run.State != "cancelled" || bound.Run.NativeDisposition != "not_started" {
		t.Fatalf("cancel invented native stop evidence: %+v %v", bound, err)
	}
}
