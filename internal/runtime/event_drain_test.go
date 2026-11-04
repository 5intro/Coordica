package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coordica/internal/store"
)

type capturedEventPort struct {
	countedBindingPort
	batch    func(NativeSessionID, int) []EventInput
	release  func(int64) error
	releases []int64
}

func (p *capturedEventPort) Captured(_ context.Context, _ NativeCreate, id NativeSessionID, limit int) (SourceStamp, []EventInput, *SourceEnd, error) {
	return SourceStamp{Epoch: "capture-source", Evidence: "local_observed"}, p.batch(id, limit), nil, nil
}

func (p *capturedEventPort) ReleaseCaptured(_ context.Context, _ NativeCreate, _ NativeSessionID, through int64) error {
	p.releases = append(p.releases, through)
	if p.release != nil {
		return p.release(through)
	}
	return nil
}

func captureFixture(t *testing.T, port *capturedEventPort) (ledgerFixture, *BindingProducer, Binding, EventStream) {
	t.Helper()
	f, p, task, run := bindingFixture(t, port)
	binding := prepareFixture(t, f, p, task, run)
	binding, err := p.Produce(t.Context(), f.cred, binding.ID)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := p.OpenEventStream(t.Context(), f.cred, binding.ID, SourceStamp{Epoch: "capture-source", Evidence: "local_observed"})
	if err != nil {
		t.Fatal(err)
	}
	return f, p, binding, stream
}

func TestEventDrainCommitsBeforeReleaseAndRetriesLostCaptureAck(t *testing.T) {
	port := &capturedEventPort{batch: func(id NativeSessionID, limit int) []EventInput {
		if limit != 1 {
			t.Fatalf("capture limit = %d", limit)
		}
		return []EventInput{eventInput(Binding{NativeID: id}, "captured-1", 1)}
	}}
	f, p, binding, stream := captureFixture(t, port)
	ackLost := errors.New("fixture capture ACK lost")
	port.release = func(through int64) error {
		// A separate transaction must see the committed row before capture is
		// released. This also checks that port I/O is outside the write transaction.
		if err := f.db.View(t.Context(), func(tx *store.Tx) error {
			_, err := tx.RuntimeEventBySource(stream.ApplicationID, stream.ID, through)
			return err
		}); err != nil {
			t.Fatalf("released an uncommitted observation: %v", err)
		}
		return ackLost
	}
	first, err := p.DrainEvents(t.Context(), f.cred, binding.ID, 1)
	if !errors.Is(err, ackLost) || first.Accepted != 1 || first.AcceptedThrough != 1 {
		t.Fatalf("first drain: %+v %v", first, err)
	}
	before := eventCounts(t, f)
	port.release = nil
	second, err := p.DrainEvents(t.Context(), f.cred, binding.ID, 1)
	if err != nil || second != first || len(port.releases) != 2 || port.stops.Load() != 0 {
		t.Fatalf("replayed capture: %+v %v releases=%v", second, err, port.releases)
	}
	sameEventCounts(t, before, eventCounts(t, f))
	reopenFixture(t, &f)
	cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64)
	if err != nil || len(page.Events) != 1 || page.Events[0].Input.EventID != "captured-1" || page.Stream.Source.Evidence != "local_observed" {
		t.Fatalf("reopened capture: %+v %v", page, err)
	}
}

func TestEventDrainRejectsOversizedOrUnorderedCaptureBeforeRelease(t *testing.T) {
	for _, scenario := range []string{"oversized", "unordered", "duplicate-sequence"} {
		t.Run(scenario, func(t *testing.T) {
			port := &capturedEventPort{batch: func(id NativeSessionID, _ int) []EventInput {
				b := Binding{NativeID: id}
				switch scenario {
				case "unordered":
					return []EventInput{eventInput(b, "second", 2), eventInput(b, "first", 1)}
				case "duplicate-sequence":
					return []EventInput{eventInput(b, "first", 1), eventInput(b, "first", 1)}
				default:
					return []EventInput{eventInput(b, "first", 1), eventInput(b, "second", 2)}
				}
			}}
			f, p, binding, stream := captureFixture(t, port)
			beforeB, beforeR, beforeS := sourceState(t, f, binding, stream)
			before := eventCounts(t, f)
			limit := 2
			if scenario == "oversized" {
				limit = 1
			}
			receipt, err := p.DrainEvents(t.Context(), f.cred, binding.ID, limit)
			if !errors.Is(err, ErrInvalid) || receipt != (DrainReceipt{}) || len(port.releases) != 0 || port.stops.Load() != 0 {
				t.Fatalf("invalid capture changed source: receipt=%+v err=%v releases=%v", receipt, err, port.releases)
			}
			afterB, afterR, afterS := sourceState(t, f, binding, stream)
			if beforeB != afterB || beforeR != afterR || beforeS != afterS {
				t.Fatal("invalid capture changed durable resource state")
			}
			sameEventCounts(t, before, eventCounts(t, f))
		})
	}
}

func TestEventDrainConflictingOldCapturePersistsNonregressingTerminal(t *testing.T) {
	port := &capturedEventPort{batch: func(id NativeSessionID, _ int) []EventInput {
		changed := eventInput(Binding{NativeID: id}, "first", 1)
		changed.Fact.Bytes++
		return []EventInput{changed}
	}}
	f, p, binding, stream := captureFixture(t, port)
	for i, id := range []string{"first", "second"} {
		if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, id, int64(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.DrainEvents(t.Context(), f.cred, binding.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	currentB, currentR, currentS := sourceState(t, f, binding, stream)
	if currentB.State != "unknown" || !currentB.CleanupStopped || currentR.NativeDisposition != "session_unknown" ||
		currentS.State != "failed" || !currentS.Gap || !currentS.Stop.TreeStopped || currentS.ObservedThrough != 2 || len(port.releases) != 0 {
		t.Fatalf("conflicting old capture lost terminal facts: binding=%+v stream=%+v", currentB, currentS)
	}
}

func TestEventDrainCleanupKeepsConcurrentObservationAndStop(t *testing.T) {
	port := &capturedEventPort{batch: func(id NativeSessionID, _ int) []EventInput {
		changed := eventInput(Binding{NativeID: id}, "first", 1)
		changed.Fact.Bytes++
		return []EventInput{changed}
	}}
	f, p, binding, stream := captureFixture(t, port)
	if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, "first", 1)); err != nil {
		t.Fatal(err)
	}
	port.stop = func(ctx context.Context, call NativeCreate, id NativeSessionID) (BindingStop, error) {
		if _, err := p.AcceptEvent(ctx, f.cred, stream.ID, eventInput(binding, "during-stop", 2)); err != nil {
			t.Fatal(err)
		}
		return BindingStop{BindingID: call.BindingID, OwnerID: call.OwnerID, NativeID: id, TreeStopped: true}, nil
	}
	if _, err := p.DrainEvents(t.Context(), f.cred, binding.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	currentB, _, currentS := sourceState(t, f, binding, stream)
	if currentB.State != "unknown" || !currentB.CleanupStopped || currentS.State != "failed" ||
		!currentS.Stop.TreeStopped || currentS.LastSourceSequence != 2 || currentS.ObservedThrough != 2 {
		t.Fatalf("concurrent observation prevented cleanup receipt: binding=%+v stream=%+v", currentB, currentS)
	}
}

func TestEventDrainReportsTerminalPersistenceFailure(t *testing.T) {
	port := &capturedEventPort{batch: func(id NativeSessionID, _ int) []EventInput {
		changed := eventInput(Binding{NativeID: id}, "first", 1)
		changed.Fact.Bytes++
		return []EventInput{changed}
	}}
	f, p, binding, stream := captureFixture(t, port)
	if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, "first", 1)); err != nil {
		t.Fatal(err)
	}
	beforeB, beforeR, beforeS := sourceState(t, f, binding, stream)
	before := eventCounts(t, f)
	bindingSQL(t, f.path, `CREATE TRIGGER fixture_capture_terminal_failure BEFORE INSERT ON outbox
 WHEN NEW.kind='runtime.events.source_ended' BEGIN SELECT RAISE(ABORT,'fixture terminal persistence failure'); END`)
	_, err := p.DrainEvents(t.Context(), f.cred, binding.ID, 1)
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "fixture terminal persistence failure") || port.stops.Load() != 1 {
		t.Fatalf("terminal persistence failure was hidden: %v stops=%d", err, port.stops.Load())
	}
	afterB, afterR, afterS := sourceState(t, f, binding, stream)
	if beforeB != afterB || beforeR != afterR || beforeS != afterS {
		t.Fatal("failed terminal transaction left partial state")
	}
	sameEventCounts(t, before, eventCounts(t, f))
}
