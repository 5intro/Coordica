package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"coordica/internal/domain"
)

// Additive schema-v1 extension. Old records and queries are unchanged; backups
// include this table. Scope and normalization belong to the runtime consumer.
func initializeRuntimeEvents(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS runtime_events(
 sequence INTEGER NOT NULL CHECK(sequence>0),
 application_id TEXT NOT NULL,stream_id TEXT NOT NULL,event_id TEXT NOT NULL,
 source_sequence INTEGER NOT NULL CHECK(source_sequence>0),digest TEXT NOT NULL,data BLOB NOT NULL,
 PRIMARY KEY(application_id,stream_id,sequence),
 UNIQUE(application_id,stream_id,event_id),UNIQUE(application_id,stream_id,source_sequence));
 CREATE INDEX IF NOT EXISTS runtime_events_page ON runtime_events(application_id,stream_id,sequence);`)
	return err
}

type RuntimeEventRow struct {
	Sequence       int64
	EventID        string
	SourceSequence int64
	Digest         string
	Data           json.RawMessage
}

func scanRuntimeEvent(row *sql.Row) (RuntimeEventRow, error) {
	var r RuntimeEventRow
	err := row.Scan(&r.Sequence, &r.EventID, &r.SourceSequence, &r.Digest, &r.Data)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return r, err
}

func (t *Tx) RuntimeEventByID(app, stream, id string) (RuntimeEventRow, error) {
	return scanRuntimeEvent(t.tx.QueryRowContext(t.ctx, `SELECT sequence,event_id,source_sequence,digest,data
 FROM runtime_events WHERE application_id=? AND stream_id=? AND event_id=?`, app, stream, id))
}
func (t *Tx) RuntimeEventBySource(app, stream string, sequence int64) (RuntimeEventRow, error) {
	return scanRuntimeEvent(t.tx.QueryRowContext(t.ctx, `SELECT sequence,event_id,source_sequence,digest,data
 FROM runtime_events WHERE application_id=? AND stream_id=? AND source_sequence=?`, app, stream, sequence))
}
func (t *Tx) AppendRuntimeEvent(app, stream string, row RuntimeEventRow) (int64, error) {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO runtime_events(application_id,stream_id,sequence,event_id,source_sequence,digest,data)
 VALUES(?,?,?,?,?,?,?)`, app, stream, row.Sequence, row.EventID, row.SourceSequence, row.Digest, []byte(row.Data))
	if err != nil {
		return 0, err
	}
	return row.Sequence, nil
}
func (t *Tx) RuntimeEventPage(app, stream string, after int64, limit int) ([]RuntimeEventRow, error) {
	if after < 0 || limit < 1 || limit > 65 {
		return nil, fmt.Errorf("invalid bounded event page")
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT sequence,event_id,source_sequence,digest,data FROM runtime_events
 WHERE application_id=? AND stream_id=? AND sequence>? ORDER BY sequence LIMIT ?`, app, stream, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RuntimeEventRow, 0, limit)
	for rows.Next() {
		var r RuntimeEventRow
		if err := rows.Scan(&r.Sequence, &r.EventID, &r.SourceSequence, &r.Digest, &r.Data); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
