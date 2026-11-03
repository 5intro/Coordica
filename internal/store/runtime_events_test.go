package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRuntimeEventSQLUniquenessAndScopedKeyset(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row := RuntimeEventRow{Sequence: 1, EventID: "event-a", SourceSequence: 1, Digest: "digest", Data: json.RawMessage(`{"fixture":true}`)}
	if err = s.Update(t.Context(), func(tx *Tx) error { _, err := tx.AppendRuntimeEvent("app-a", "stream", row); return err }); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"same-event-id", "same-source-sequence"} {
		changed := row
		changed.Sequence = 2
		if scenario == "same-event-id" {
			changed.SourceSequence = 2
		} else {
			changed.EventID = "event-b"
		}
		if err = s.Update(t.Context(), func(tx *Tx) error { _, err := tx.AppendRuntimeEvent("app-a", "stream", changed); return err }); err == nil {
			t.Fatalf("missing SQL uniqueness: %s", scenario)
		}
	}
	if err = s.Update(t.Context(), func(tx *Tx) error { _, err := tx.AppendRuntimeEvent("app-b", "stream", row); return err }); err != nil {
		t.Fatal(err)
	}
	if err = s.View(t.Context(), func(tx *Tx) error {
		rows, err := tx.RuntimeEventPage("app-a", "stream", 0, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].EventID != "event-a" {
			t.Fatal(rows)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
