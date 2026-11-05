package runtime

import (
	"errors"
	"reflect"
	"testing"

	"coordica/internal/store"
)

func storedEventCursor(t *testing.T, f ledgerFixture, id string) EventCursor {
	t.Helper()
	var cursor EventCursor
	if err := f.db.View(t.Context(), func(tx *store.Tx) error {
		return tx.Get(kindEventCursor, id, &cursor)
	}); err != nil {
		t.Fatal(err)
	}
	return cursor
}

func TestEventCursorLostPageRepeatsAfterReopenAndAckIsMonotonic(t *testing.T) {
	f, p, binding, stream := eventFixture(t)
	for i, id := range []string{"first", "second"} {
		if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, id, int64(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, &f)
	repeated, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1)
	if err != nil || !reflect.DeepEqual(first, repeated) {
		t.Fatalf("unacknowledged page did not replay: first=%+v repeated=%+v err=%v", first, repeated, err)
	}
	if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, 1); err != nil {
		t.Fatal(err)
	}
	before := eventCounts(t, f)
	if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("ACK moved backward: %v", err)
	}
	sameEventCounts(t, before, eventCounts(t, f))
	page, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 64)
	if err != nil || len(page.Events) != 1 || page.Events[0].Input.EventID != "second" || page.More {
		t.Fatalf("catchup skipped or repeated an acknowledged event: %+v %v", page, err)
	}
}

func TestEventCursorAndTerminalOutboxFailuresRollbackState(t *testing.T) {
	for _, operation := range []string{"cursor_opened", "page_offered", "cursor_acked", "source_ended"} {
		t.Run(operation, func(t *testing.T) {
			f, p, binding, stream := eventFixture(t)
			if _, err := p.AcceptEvent(t.Context(), f.cred, stream.ID, eventInput(binding, "first", 1)); err != nil {
				t.Fatal(err)
			}
			cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "cursor_acked" {
				if _, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1); err != nil {
					t.Fatal(err)
				}
			}
			before := eventCounts(t, f)
			beforeC := storedEventCursor(t, f, cursor.ID)
			beforeB, beforeR, beforeS := sourceState(t, f, binding, stream)
			bindingSQL(t, f.path, `CREATE TRIGGER fixture_event_cursor_failure BEFORE INSERT ON outbox
 WHEN NEW.kind='runtime.events.`+operation+`' BEGIN SELECT RAISE(ABORT,'fixture event transaction failed'); END`)
			switch operation {
			case "cursor_opened":
				result, openErr := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
				err = openErr
				if result != (EventCursor{}) {
					t.Fatal("failed open exposed an uncommitted cursor")
				}
			case "page_offered":
				result, readErr := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1)
				err = readErr
				if !reflect.DeepEqual(result, EventPage{}) {
					t.Fatal("failed offer exposed an uncommitted page")
				}
			case "cursor_acked":
				result, ackErr := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, 1)
				err = ackErr
				if result != (EventCursor{}) {
					t.Fatal("failed ACK exposed an uncommitted cursor")
				}
			default:
				err = p.ReportSourceEnd(t.Context(), f.cred, stream.ID, SourceEnd{Reason: "transport_lost", LastObserved: 1})
			}
			if err == nil {
				t.Fatal("final outbox failure did not fail operation")
			}
			afterB, afterR, afterS := sourceState(t, f, binding, stream)
			if beforeC != storedEventCursor(t, f, cursor.ID) || beforeB != afterB || beforeR != afterR || beforeS != afterS {
				t.Fatal("failed event transaction left partial state")
			}
			sameEventCounts(t, before, eventCounts(t, f))
		})
	}
}

func TestEventCursorExpiresWhenDatasetOrSourceEpochChanges(t *testing.T) {
	for _, epoch := range []string{"dataset", "source"} {
		t.Run(epoch, func(t *testing.T) {
			f, _, _, stream := eventFixture(t)
			cursor, err := f.ledger.OpenEventCursor(t.Context(), f.cred, stream.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.db.Update(t.Context(), func(tx *store.Tx) error {
				if epoch == "dataset" {
					return tx.Put(kindEventDataset, "current", eventDataset{Epoch: "replacement-dataset"})
				}
				stream.Source.Epoch = "replacement-source"
				return tx.Put(kindEventStream, stream.ID, stream)
			}); err != nil {
				t.Fatal(err)
			}
			before := eventCounts(t, f)
			if page, err := f.ledger.ReadEvents(t.Context(), f.cred, cursor.ID, 1); !errors.Is(err, ErrCursorExpired) || len(page.Events) != 0 {
				t.Fatalf("stale epoch returned events: %+v %v", page, err)
			}
			if _, err := f.ledger.AckEvents(t.Context(), f.cred, cursor.ID, 0); !errors.Is(err, ErrCursorExpired) {
				t.Fatalf("stale epoch accepted ACK: %v", err)
			}
			sameEventCounts(t, before, eventCounts(t, f))
		})
	}
}
