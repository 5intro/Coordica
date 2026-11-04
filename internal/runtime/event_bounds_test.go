package runtime

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventInvalidInputsAndPageBoundsDoNotWrite(t *testing.T) {
	f, p, binding, stream := eventFixture(t)
	valid := eventInput(binding, "first", 1)
	for _, scenario := range []struct {
		name   string
		change func(*EventInput)
	}{
		{"missing-id", func(in *EventInput) { in.EventID = "" }},
		{"long-id", func(in *EventInput) { in.EventID = strings.Repeat("x", 257) }},
		{"zero-sequence", func(in *EventInput) { in.SourceSequence = 0 }},
		{"sequence-overflow", func(in *EventInput) { in.SourceSequence = math.MaxInt64 }},
		{"missing-time", func(in *EventInput) { in.OccurredAt = time.Time{} }},
		{"invalid-kind", func(in *EventInput) { in.Kind = "native.raw" }},
		{"invalid-state", func(in *EventInput) { in.Fact.State = "approved" }},
		{"negative-bytes", func(in *EventInput) { in.Fact.Bytes = -1 }},
		{"oversized-bytes", func(in *EventInput) { in.Fact.Bytes = (256 << 10) + 1 }},
		{"negative-tokens", func(in *EventInput) { in.Fact.Tokens = -1 }},
		{"oversized-tokens", func(in *EventInput) { in.Fact.Tokens = (1 << 50) + 1 }},
		{"invalid-turn", func(in *EventInput) { in.Fact.NativeTurnID = "turn\ninvalid" }},
		{"oversized-request", func(in *EventInput) { in.Fact.NativeRequestID = strings.Repeat("x", 513) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			input := valid
			scenario.change(&input)
			before := eventCounts(t, f)
			if receipt, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, input); !errors.Is(err, ErrInvalid) || receipt != (EventReceipt{}) {
				t.Fatalf("invalid input accepted: %+v %v", receipt, err)
			}
			sameEventCounts(t, before, eventCounts(t, f))
		})
	}
	before := eventCounts(t, f)
	wrongNative := valid
	wrongNative.NativeID.Value = "different-session"
	if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, wrongNative); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for _, limit := range []int{-1, 0, 65} {
		if _, err := f.ledger.ReadEvents(t.Context(), f.cred, "cursor", limit); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 0, 33} {
		if _, err := p.DrainEvents(t.Context(), f.cred, binding.ID, limit); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	sameEventCounts(t, before, eventCounts(t, f))
}

func TestEventConcurrentDuplicateHasOneDurableFact(t *testing.T) {
	f, p, binding, stream := eventFixture(t)
	before := eventCounts(t, f)
	input := eventInput(binding, "concurrent", 1)
	var wait sync.WaitGroup
	start := make(chan struct{})
	results := make(chan EventReceipt, 8)
	failures := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			receipt, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, input)
			results <- receipt
			failures <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	duplicates := 0
	for receipt := range results {
		if receipt.Sequence != 1 || receipt.SourceSequence != 1 {
			t.Fatalf("duplicate changed sequence: %+v", receipt)
		}
		if receipt.Duplicate {
			duplicates++
		}
	}
	after := eventCounts(t, f)
	if duplicates != 7 || after["runtime_events"] != before["runtime_events"]+1 ||
		after["audit"] != before["audit"]+1 || after["outbox"] != before["outbox"]+1 {
		t.Fatalf("duplicate writes survived: duplicates=%d before=%v after=%v", duplicates, before, after)
	}
}
