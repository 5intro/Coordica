package runtime

import "testing"

func TestSourceTerminalWatermarkAndFailureDoNotRegressWhenOlderStopArrives(t *testing.T) {
	for _, reason := range []string{"transport_lost", "closed"} {
		t.Run(reason, func(t *testing.T) {
			f, p, b, s := eventFixture(t)
			if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "first", 1)); err != nil {
				t.Fatal(err)
			}
			if err := p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 10}); err != nil {
				t.Fatal(err)
			}
			call := NativeCreate{BindingID: b.ID, OwnerID: b.OwnerID, Generation: b.Generation, Config: b.Config}
			stopped, proof, err := p.cleanup(t.Context(), call, b.NativeID)
			if err != nil || !stopped {
				t.Fatal(err)
			}
			if err = p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: reason, LastObserved: 8, Stop: proof}); err != nil {
				t.Fatal(err)
			}
			reopenFixture(t, &f)
			c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, s.ID)
			if err != nil {
				t.Fatal(err)
			}
			page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
			if err != nil || page.Stream.ObservedThrough != 10 || !page.Stream.Stop.TreeStopped || !page.Stream.Gap || page.Stream.State != "failed" || page.Stream.EndReason != "transport_lost" {
				t.Fatalf("older stop rolled back durable source facts: %+v %v", page.Stream, err)
			}
		})
	}
}

func TestLateGapDoesNotEraseKnownResourceStop(t *testing.T) {
	f, p, b, s := eventFixture(t)
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "first", 1)); err != nil {
		t.Fatal(err)
	}
	released, err := p.Release(t.Context(), f.cred, BindingRequest{Key: "release", BindingID: b.ID, ExpectedVersion: b.Version, Generation: b.Generation})
	if err != nil {
		t.Fatal(err)
	}
	_, proof, err := p.cleanup(t.Context(), NativeCreate{BindingID: b.ID, OwnerID: b.OwnerID, Generation: b.Generation, Config: b.Config}, b.NativeID)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: "closed", LastObserved: 1, Stop: proof}); err != nil {
		t.Fatal(err)
	}
	if err = p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: "transport_lost", LastObserved: 1, Gap: true}); err != nil {
		t.Fatal(err)
	}
	c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
	if err != nil || !page.Stream.Gap || page.Stream.State != "failed" || !page.Stream.Stop.TreeStopped {
		t.Fatalf("late gap was swallowed or stop lost: %+v %v", page.Stream, err)
	}
	current, err := f.ledger.ReadBinding(t.Context(), f.cred, b.ID)
	if err != nil || current != released {
		t.Fatalf("event diagnostic rewrote explicit release: %+v %v", current, err)
	}
}
