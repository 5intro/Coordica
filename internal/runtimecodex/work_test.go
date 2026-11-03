package runtimecodex

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"coordica/internal/runtime"
)

func prepareWork(t *testing.T, f *fixture, b runtime.Binding, key, text string) runtime.Work {
	t.Helper()
	drained, err := f.producer.DrainEvents(t.Context(), f.cred, b.ID, 32)
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.producer.PrepareWork(t.Context(), f.cred, runtime.PrepareWorkRequest{Key: key, BindingID: b.ID, StreamID: drained.StreamID,
		Generation: b.Generation, ExpectedVersion: b.Version, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func drainUntilEnded(t *testing.T, f *fixture, id string) runtime.Work {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := f.producer.DrainEvents(t.Context(), f.cred, f.binding.ID, 32); err != nil {
			t.Fatal(err)
		}
		w, err := f.ledger.ReadWork(t.Context(), f.cred, id)
		if err != nil {
			t.Fatal(err)
		}
		if w.State == "native_ended" {
			return w
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("native terminal was not correlated")
	return runtime.Work{}
}
func TestExplicitWorkTwoTurnsSamePeerAndCompletionIsNotTreeStop(t *testing.T) {
	f := setup(t, "work-normal")
	b := produce(t, f)
	for _, text := range []string{"explicit-work-one", "explicit-work-two"} {
		w := prepareWork(t, f, b, text, text)
		w, err := f.producer.ProduceWork(t.Context(), f.cred, w.ID)
		if err != nil || w.State != "native_accepted" {
			t.Fatalf("native acceptance: %+v %v", w, err)
		}
		w = drainUntilEnded(t, f, w.ID)
		if w.Observed.NativeTurnID == "" || w.Observed.Evidence != "source_terminal" {
			t.Fatal(w)
		}
		b, err = f.ledger.ReadBinding(t.Context(), f.cred, b.ID)
		if err != nil || b.State != "ready" || b.CleanupStopped {
			t.Fatal(b, err)
		}
		if _, err = f.ledger.CancelRun(t.Context(), f.cred, runtime.CancelRequest{Key: text + "-cancel", TaskID: b.TaskID, RunID: b.RunID, RunGeneration: b.RunGeneration, ExpectedVersion: 3}); !errors.Is(err, runtime.ErrStopUnconfirmed) {
			t.Fatal("Turn completion invented tree stop", err)
		}
	}
	journal, err := os.ReadFile(f.journal)
	if err != nil {
		t.Fatal(err)
	}
	if journalCount(t, f, "turn/start") != 2 || journalCount(t, f, "thread/start") != 1 || bytes.Contains(journal, []byte("TASK-SECRET-NEVER-SEND")) {
		t.Fatalf("work input or native count: %s", journal)
	}
	released := release(t, f, b)
	if !released.CleanupStopped {
		t.Fatal("explicit release lacked tree stop")
	}
}
func TestNativeTerminalBeforeStartReplyIsSavedAndCorrelatedAfterReply(t *testing.T) {
	f := setup(t, "work-early")
	b := produce(t, f)
	w := prepareWork(t, f, b, "work", "explicit-early-work")
	done := make(chan error, 1)
	go func() { _, err := f.producer.ProduceWork(t.Context(), f.cred, w.ID); done <- err }()
	awaitMethod(t, f, "turn/start")
	deadline := time.Now().Add(2 * time.Second)
	var accepted int
	for time.Now().Before(deadline) {
		r, err := f.producer.DrainEvents(t.Context(), f.cred, b.ID, 32)
		if err != nil {
			t.Fatal(err)
		}
		accepted += r.Accepted
		if accepted >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	before, err := f.ledger.ReadWork(t.Context(), f.cred, w.ID)
	if err != nil || before.State != "claimed" || accepted != 2 {
		t.Fatalf("early event guessed native acknowledgement: %+v count=%d %v", before, accepted, err)
	}
	if err = os.WriteFile(f.proceed, []byte("reply"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	after, err := f.ledger.ReadWork(t.Context(), f.cred, w.ID)
	if err != nil || after.State != "native_ended" || after.Observed.Evidence != "source_terminal" || journalCount(t, f, "turn/start") != 1 {
		t.Fatalf("early terminal lost/replayed work: %+v %v", after, err)
	}
}
