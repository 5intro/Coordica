package runtime

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func eventCounts(t *testing.T, f ledgerFixture) map[string]int {
	t.Helper()
	result := storageCounts(t, f.path)
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT count(*) FROM runtime_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	result["runtime_events"] = count
	return result
}
func sameEventCounts(t *testing.T, before, after map[string]int) {
	t.Helper()
	for key, count := range before {
		if after[key] != count {
			t.Fatalf("partial event transaction: %s %d→%d", key, count, after[key])
		}
	}
}

func TestEventConflictLateOutOfOrderGapAndTerminalDuplicate(t *testing.T) {
	f, p, b, s := eventFixture(t)
	first := eventInput(b, "event-3", 3)
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, first); err != nil {
		t.Fatal(err)
	}
	before := eventCounts(t, f)
	changed := first
	changed.Fact.Bytes++
	for _, input := range []EventInput{changed, eventInput(b, "another-id", 3), eventInput(b, "event-3", 4)} {
		if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	sameEventCounts(t, before, eventCounts(t, f))
	for _, n := range []int64{2, 1} {
		if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, fmt.Sprintf("event-%d", n), n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.ReportSourceEnd(t.Context(), f.cred, s.ID, SourceEnd{Reason: "transport_lost", Gap: true, LastObserved: 3}); err != nil {
		t.Fatal(err)
	}
	before = eventCounts(t, f)
	if receipt, err := p.AcceptEvent(t.Context(), f.cred, s.ID, first); err != nil || !receipt.Duplicate {
		t.Fatalf("terminal duplicate lost original fact: %+v %v", receipt, err)
	}
	sameEventCounts(t, before, eventCounts(t, f))
	c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
	if err != nil || !page.Stream.Gap || page.Stream.ContiguousSourceSequence != 3 {
		t.Fatalf("gap hidden: %+v %v", page, err)
	}
	if _, err = f.ledger.AckEvents(t.Context(), f.cred, c.ID, page.Cursor.Offered); err != nil {
		t.Fatal(err)
	}
	page, err = f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
	if err != nil || !page.Stream.Gap || len(page.Events) != 0 {
		t.Fatalf("ACK erased gap: %+v %v", page, err)
	}
}

func TestEventFinalOutboxFailureRollsBackRowSequenceStreamAndAudit(t *testing.T) {
	f, p, b, s := eventFixture(t)
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "first", 1)); err != nil {
		t.Fatal(err)
	}
	before := eventCounts(t, f)
	bindingSQL(t, f.path, `CREATE TRIGGER fixture_event_outbox_failure BEFORE INSERT ON outbox
 WHEN NEW.kind='runtime.events.accepted' BEGIN SELECT RAISE(ABORT,'fixture last event write failed'); END`)
	input := eventInput(b, "second", 2)
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err == nil {
		t.Fatal("event accepted after last transaction write failed")
	}
	sameEventCounts(t, before, eventCounts(t, f))
	bindingSQL(t, f.path, `DROP TRIGGER fixture_event_outbox_failure`)
	if receipt, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err != nil || receipt.Sequence != 2 {
		t.Fatalf("rollback consumed sequence/receipt: %+v %v", receipt, err)
	}
}

func TestEventCursorScopeRevocationAndGenerationExpiry(t *testing.T) {
	for _, change := range []string{"other-app", "other-principal", "revoked", "generation"} {
		t.Run(change, func(t *testing.T) {
			f, p, b, s := eventFixture(t)
			input := eventInput(b, "first", 1)
			if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, input); err != nil {
				t.Fatal(err)
			}
			c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64); err != nil {
				t.Fatal(err)
			}
			credential := f.cred
			expected := ErrForbidden
			if change == "other-app" || change == "other-principal" {
				grant := f.config
				grant.ID = "other-principal"
				grant.Token = strings.Repeat("other-fixture-secret-", 3)
				if change == "other-app" {
					grant.ApplicationID = "other-app"
				}
				if _, err = f.manager.ConfigureGrant(t.Context(), grant, 0); err != nil {
					t.Fatal(err)
				}
				credential = Credential{PrincipalID: grant.ID, Token: grant.Token}
			} else if change == "revoked" {
				f.config.Enabled = false
				if _, err = f.manager.ConfigureGrant(t.Context(), f.config, 2); err != nil {
					t.Fatal(err)
				}
			} else {
				expected = ErrCursorExpired
				if _, err = p.RecoverUnknown(t.Context(), f.cred, BindingRequest{Key: "recover", BindingID: b.ID, ExpectedVersion: b.Version, Generation: b.Generation}); err != nil {
					t.Fatal(err)
				}
			}
			if page, err := f.ledger.ReadEvents(t.Context(), credential, c.ID, 64); !errors.Is(err, expected) || len(page.Events) != 0 {
				t.Fatalf("cursor scope/epoch bypass: %+v %v", page, err)
			}
			if _, err = f.ledger.AckEvents(t.Context(), credential, c.ID, 1); !errors.Is(err, expected) {
				t.Fatal(err)
			}
			before := eventCounts(t, f)
			if change == "other-principal" {
				receipt, err := p.AcceptEvent(t.Context(), credential, s.ID, input)
				if err != nil || !receipt.Duplicate {
					t.Fatalf("authorized historical host fact was made cursor-private: %+v %v", receipt, err)
				}
				if _, err = p.AcceptEvent(t.Context(), credential, s.ID, eventInput(b, "new-event", 2)); !errors.Is(err, ErrForbidden) {
					t.Fatalf("historical receipt granted new-write authority: %v", err)
				}
			} else if _, err = p.AcceptEvent(t.Context(), credential, s.ID, input); err == nil {
				t.Fatal("event replay ignored current authority/generation")
			}
			sameEventCounts(t, before, eventCounts(t, f))
		})
	}
}

func TestEventHardCapacityCommitsGapButDoesNotLoseDedupeKeys(t *testing.T) {
	f, p, b, s := eventFixture(t)
	for n := int64(1); n <= maxStreamEvents; n++ {
		if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, fmt.Sprintf("event-%d", n), n)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "overflow", maxStreamEvents+1)); !errors.Is(err, ErrEventBackpressure) {
		t.Fatal(err)
	}
	before := eventCounts(t, f)
	if receipt, err := p.AcceptEvent(t.Context(), f.cred, s.ID, eventInput(b, "event-1", 1)); err != nil || !receipt.Duplicate {
		t.Fatalf("capacity forgot dedupe: %+v %v", receipt, err)
	}
	sameEventCounts(t, before, eventCounts(t, f))
	c, err := f.ledger.OpenEventCursor(t.Context(), f.cred, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, c.ID, 64)
	if err != nil || !page.Stream.Gap || page.Stream.Count != maxStreamEvents || !page.More || page.Stream.EndReason != "capacity" {
		t.Fatalf("capacity silently accepted/dropped: %+v %v", page, err)
	}
}
