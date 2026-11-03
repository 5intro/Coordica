package runtimecodex

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"coordica/internal/runtime"
)

func TestCapturedProtocolFactsPersistWithoutTextAndKeepPeerSourceEpoch(t *testing.T) {
	f := setup(t, "events")
	ready := produce(t, f)
	var receipt runtime.DrainReceipt
	deadline := time.Now().Add(2 * time.Second)
	for receipt.Accepted < 4 && time.Now().Before(deadline) {
		next, err := f.producer.DrainEvents(t.Context(), f.cred, ready.ID, 32)
		if err != nil {
			t.Fatal(err)
		}
		receipt.StreamID = next.StreamID
		receipt.Accepted += next.Accepted
		time.Sleep(5 * time.Millisecond)
	}
	if receipt.Accepted != 4 {
		t.Fatalf("native notifications missing: %+v", receipt)
	}
	cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, receipt.StreamID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64)
	if err != nil || len(page.Events) != 4 {
		t.Fatalf("events page: %+v %v", page, err)
	}
	if page.Stream.Source.Epoch != ready.CommandID || page.Stream.Source.Evidence != "local_observed" {
		t.Fatal("source epoch did not identify concrete Peer")
	}
	deltas := 0
	requests := 0
	for _, e := range page.Events {
		if e.Input.Kind == "output.delta" {
			deltas++
		}
		if e.Input.Kind == "request.created" {
			requests++
		}
	}
	raw, _ := json.Marshal(page)
	if deltas != 2 || requests != 1 || bytes.Contains(raw, []byte("private-event-text-sentinel")) {
		t.Fatalf("deduped identical deltas or persisted text: %s", raw)
	}
	if _, err := f.producer.ReadNative(t.Context(), f.cred, ready.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.producer.DrainEvents(t.Context(), f.cred, ready.ID, 32); err != nil {
		t.Fatal(err)
	}
	if journalCount(t, f, "thread/start") != 1 || journalCount(t, f, "thread/resume") != 0 || journalCount(t, f, "turn/start") != 0 {
		t.Fatal("event drain sent native work")
	}
	released := release(t, f, ready)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = f.producer.DrainEvents(t.Context(), f.cred, ready.ID, 32); err != nil {
			t.Fatal(err)
		}
		page, err = f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64)
		if err != nil {
			t.Fatal(err)
		}
		if page.Stream.State == "closed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if page.Stream.State != "closed" || page.Stream.Gap || page.Stream.Source.Epoch != ready.CommandID {
		t.Fatalf("release changed source epoch or invented gap: %+v", page.Stream)
	}
	resumed, err := f.producer.ResumeBinding(t.Context(), f.cred, runtime.BindingRequest{Key: "resume-events", BindingID: released.ID, ExpectedVersion: released.Version, Generation: released.Generation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64); !errors.Is(err, runtime.ErrCursorExpired) {
		t.Fatalf("old source cursor survived generation change: %v", err)
	}
	var next runtime.DrainReceipt
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.producer.DrainEvents(t.Context(), f.cred, resumed.ID, 32)
		if err != nil {
			t.Fatal(err)
		}
		next.StreamID = r.StreamID
		next.Accepted += r.Accepted
		if next.Accepted == 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	newCursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, next.StreamID)
	if err != nil {
		t.Fatal(err)
	}
	newPage, err := f.ledger.ReadEvents(t.Context(), f.cred, newCursor.ID, 64)
	if err != nil || len(newPage.Events) != 4 || newPage.Stream.DatasetEpoch != page.Stream.DatasetEpoch || newPage.Stream.Source.Epoch == page.Stream.Source.Epoch {
		t.Fatalf("reconnect merged source epochs or changed dataset: %+v %v", newPage, err)
	}
}

func TestSlowConsumerOverflowStopsPeerAndPersistsExplicitGapWithUnknown(t *testing.T) {
	f := setup(t, "events-overflow")
	ready := produce(t, f)
	initial, err := f.producer.DrainEvents(t.Context(), f.cred, ready.ID, 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.producer.ReadNative(t.Context(), f.cred, ready.ID, false); err != nil && !errors.Is(err, runtime.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err = f.producer.DrainEvents(t.Context(), f.cred, ready.ID, 8)
		if err != nil {
			t.Fatal(err)
		}
		b := readBinding(t, f)
		if b.State == "unknown" {
			c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, initial.StreamID)
			if err != nil {
				t.Fatal(err)
			}
			page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
			if err != nil {
				t.Fatal(err)
			}
			if page.Stream.State == "failed" {
				if !page.Stream.Gap || !page.Stream.Stop.TreeStopped || len(page.Events) != captureLimit || page.Stream.ObservedThrough <= page.Stream.LastSourceSequence {
					t.Fatalf("overflow hidden: %+v", page)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("event overflow did not persist terminal gap")
}
