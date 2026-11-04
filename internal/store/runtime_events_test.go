package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"coordica/internal/domain"
)

func eventStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func eventRow(sequence int64) RuntimeEventRow {
	return RuntimeEventRow{Sequence: sequence, EventID: fmt.Sprintf("event-%d", sequence),
		SourceSequence: sequence + 100, Digest: fmt.Sprintf("digest-%d", sequence),
		Data: json.RawMessage(fmt.Sprintf(`{"fixture":%d,"text":"离线"}`, sequence))}
}

func appendEvent(t *testing.T, s *Store, app, stream string, row RuntimeEventRow) error {
	t.Helper()
	return s.Update(t.Context(), func(tx *Tx) error {
		sequence, err := tx.AppendRuntimeEvent(app, stream, row)
		if err == nil && sequence != row.Sequence {
			t.Errorf("sequence = %d, want %d", sequence, row.Sequence)
		}
		return err
	})
}

func eventPage(t *testing.T, s *Store, app, stream string, after int64, limit int) []RuntimeEventRow {
	t.Helper()
	var rows []RuntimeEventRow
	if err := s.View(t.Context(), func(tx *Tx) error {
		var err error
		rows, err = tx.RuntimeEventPage(app, stream, after, limit)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRuntimeEventSQLUniquenessAndScopedLookup(t *testing.T) {
	s := eventStore(t, filepath.Join(t.TempDir(), "events.db"))
	original := eventRow(1)
	if err := appendEvent(t, s, "app-a", "stream", original); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"duplicate", "sequence", "event-id", "source-sequence", "zero-sequence", "negative-source"} {
		t.Run(scenario, func(t *testing.T) {
			row := eventRow(2)
			switch scenario {
			case "duplicate":
				row = original
			case "sequence":
				row.Sequence = original.Sequence
			case "event-id":
				row.EventID = original.EventID
			case "source-sequence":
				row.SourceSequence = original.SourceSequence
			case "zero-sequence":
				row.Sequence = 0
			case "negative-source":
				row.SourceSequence = -1
			}
			if err := appendEvent(t, s, "app-a", "stream", row); err == nil {
				t.Fatal("invalid or conflicting row accepted")
			}
		})
	}
	// Reusing all three identities is valid in another application or stream.
	for _, scope := range [][2]string{{"app-b", "stream"}, {"app-a", "other-stream"}} {
		row := original
		row.Digest, row.Data = "other", json.RawMessage(`{"other":true}`)
		if err := appendEvent(t, s, scope[0], scope[1], row); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.View(t.Context(), func(tx *Tx) error {
		byID, err := tx.RuntimeEventByID("app-a", "stream", original.EventID)
		if err != nil {
			return err
		}
		bySource, err := tx.RuntimeEventBySource("app-a", "stream", original.SourceSequence)
		if err == nil && (!reflect.DeepEqual(byID, original) || !reflect.DeepEqual(bySource, original)) {
			t.Fatalf("row changed: ID=%+v, source=%+v", byID, bySource)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertMissingRuntimeEvent(t, s, original)
	if rows := eventPage(t, s, "app-a", "stream", 0, 65); len(rows) != 1 || !reflect.DeepEqual(rows[0], original) {
		t.Fatalf("rejected inserts changed history: %+v", rows)
	}
}

func assertMissingRuntimeEvent(t *testing.T, s *Store, original RuntimeEventRow) {
	t.Helper()
	for _, scope := range [][2]string{{"missing", "stream"}, {"app-a", "missing"}} {
		if err := s.View(t.Context(), func(tx *Tx) error {
			if _, err := tx.RuntimeEventByID(scope[0], scope[1], original.EventID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("missing ID lookup: %v", err)
			}
			if _, err := tx.RuntimeEventBySource(scope[0], scope[1], original.SourceSequence); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("missing source lookup: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeEventScopedKeysetAndPageBounds(t *testing.T) {
	s := eventStore(t, filepath.Join(t.TempDir(), "events.db"))
	if err := s.Update(t.Context(), func(tx *Tx) error {
		// Insert in reverse order with source order different from center order.
		for sequence := int64(70); sequence >= 1; sequence-- {
			row := eventRow(sequence)
			row.SourceSequence = 71 - sequence
			for _, scope := range [][2]string{{"app", "stream"}, {"app", "other"}, {"other", "stream"}} {
				if _, err := tx.AppendRuntimeEvent(scope[0], scope[1], row); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var after int64
	for _, limit := range []int{1, 4, 65} {
		rows := eventPage(t, s, "app", "stream", after, limit)
		if len(rows) != limit {
			t.Fatalf("page length = %d, want %d", len(rows), limit)
		}
		for i, row := range rows {
			want := eventRow(after + int64(i) + 1)
			want.SourceSequence = 71 - want.Sequence
			if !reflect.DeepEqual(row, want) {
				t.Fatalf("wrong row: %+v, want %+v", row, want)
			}
		}
		after = rows[len(rows)-1].Sequence
	}
	for _, cursor := range []int64{after, math.MaxInt64} {
		if rows := eventPage(t, s, "app", "stream", cursor, 65); len(rows) != 0 {
			t.Fatalf("end cursor returned rows: %+v", rows)
		}
	}
	for _, query := range []struct {
		after int64
		limit int
	}{{-1, 1}, {0, 0}, {0, -1}, {0, 66}, {0, math.MaxInt}} {
		if err := s.View(t.Context(), func(tx *Tx) error {
			_, err := tx.RuntimeEventPage("app", "stream", query.after, query.limit)
			return err
		}); err == nil {
			t.Fatalf("unbounded/invalid query accepted: %+v", query)
		}
	}
}

func TestRuntimeEventRollbackWithStateAuditAndOutbox(t *testing.T) {
	s := eventStore(t, filepath.Join(t.TempDir(), "events.db"))
	changed := s.Changes()
	rollback := errors.New("caller aborts transaction")
	write := func(tx *Tx) error {
		if _, err := tx.AppendRuntimeEvent("app", "stream", eventRow(1)); err != nil {
			return err
		}
		if err := tx.Put("fixture", "state", "updated"); err != nil {
			return err
		}
		return tx.Audit(domain.Event{Kind: "fixture", ObjectID: "state"})
	}
	for _, constraintFailure := range []bool{false, true} {
		err := s.Update(t.Context(), func(tx *Tx) error {
			if err := write(tx); err != nil {
				return err
			}
			if constraintFailure {
				_, err := tx.AppendRuntimeEvent("app", "stream", eventRow(1))
				return err
			}
			return rollback
		})
		if err == nil || !constraintFailure && !errors.Is(err, rollback) {
			t.Fatalf("expected rollback: %v", err)
		}
		assertRuntimeRollback(t, s)
		select {
		case <-changed:
			t.Fatal("rollback woke subscribers")
		default:
		}
	}
	if err := s.Update(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("commit did not wake subscribers")
	}
	if rows := eventPage(t, s, "app", "stream", 0, 1); len(rows) != 1 {
		t.Fatalf("commit lost event: %+v", rows)
	}
}

func assertRuntimeRollback(t *testing.T, s *Store) {
	t.Helper()
	if err := s.View(t.Context(), func(tx *Tx) error {
		var state string
		if err := tx.Get("fixture", "state", &state); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("state survived rollback: %q, %v", state, err)
		}
		for _, table := range []string{"runtime_events", "audit", "outbox"} {
			var count int
			if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("%s survived rollback: %d", table, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeEventConcurrentStoresEnforceUniqueness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	stores := []*Store{eventStore(t, path), eventStore(t, path)}
	start := make(chan struct{})
	results := make(chan error, len(stores))
	var workers sync.WaitGroup
	for _, s := range stores {
		workers.Go(func() {
			<-start
			results <- s.Update(t.Context(), func(tx *Tx) error {
				_, err := tx.AppendRuntimeEvent("app", "stream", eventRow(1))
				return err
			})
		})
	}
	close(start)
	workers.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful writers = %d, want 1", successes)
	}
	if rows := eventPage(t, stores[0], "app", "stream", 0, 65); len(rows) != 1 {
		t.Fatalf("duplicate stored: %+v", rows)
	}
}

func TestRuntimeEventSchemaV1UpgradeReopenAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A pre-extension schema-v1 file with a legacy record, without runtime data.
	_, err = legacy.Exec(`CREATE TABLE schema_version(version INTEGER NOT NULL);
 INSERT INTO schema_version VALUES(1);
 CREATE TABLE records(kind TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(kind,id));
 INSERT INTO records VALUES('fixture','legacy','"preserved"');`)
	closeErr := legacy.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("legacy fixture: %v, close: %v", err, closeErr)
	}
	s := eventStore(t, path)
	if err := s.Update(t.Context(), func(tx *Tx) error {
		if err := tx.Audit(domain.Event{Kind: "fixture", ObjectID: "legacy"}); err != nil {
			return err
		}
		if err := tx.Remember("actor", "key", "digest", "response"); err != nil {
			return err
		}
		_, err := tx.AppendRuntimeEvent("app", "stream", eventRow(1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	if err := appendEvent(t, s, "app", "stream", eventRow(2)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for i, file := range []string{path, backup} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			reopened := eventStore(t, file)
			assertEventLegacyState(t, reopened)
			rows := eventPage(t, reopened, "app", "stream", 0, 65)
			if len(rows) != 2-i || !reflect.DeepEqual(rows[0], eventRow(1)) {
				t.Fatalf("event snapshot not durable: %+v", rows)
			}
			if err := appendEvent(t, reopened, "app", "stream", eventRow(1)); err == nil {
				t.Fatal("uniqueness lost after reopen/backup")
			}
			if err := appendEvent(t, reopened, "app", "stream", eventRow(3)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertEventLegacyState(t *testing.T, s *Store) {
	t.Helper()
	if err := s.View(t.Context(), func(tx *Tx) error {
		var state string
		if err := tx.Get("fixture", "legacy", &state); err != nil || state != "preserved" {
			t.Fatalf("legacy record lost: %q, %v", state, err)
		}
		events, err := tx.Events(10)
		if err != nil || len(events) != 1 || events[0].ObjectID != "legacy" {
			t.Fatalf("legacy audit lost: %+v, %v", events, err)
		}
		response, found, err := tx.Replay("actor", "key", "digest")
		if err != nil || !found || string(response) != `"response"` {
			t.Fatalf("legacy replay lost: %s, %t, %v", response, found, err)
		}
		var version, pending int
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT version FROM schema_version").Scan(&version); err != nil {
			return err
		}
		if err := tx.tx.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM outbox WHERE delivered=0").Scan(&pending); err != nil {
			return err
		}
		if version != 1 || pending != 1 {
			t.Fatalf("legacy metadata changed: schema=%d, pending=%d", version, pending)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
