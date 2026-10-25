package store

import (
	"database/sql"
	"encoding/json"
	"strings"

	"coordica/internal/domain"
)

func initializeLeaderActivity(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS leader_activity(
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, execution_id TEXT NOT NULL,
 generation INTEGER NOT NULL, input_version INTEGER NOT NULL, event_key TEXT NOT NULL, data BLOB NOT NULL,
 UNIQUE(kind,execution_id,generation,event_key));
 CREATE INDEX IF NOT EXISTS leader_activity_execution ON leader_activity(kind,execution_id,generation,sequence);`)
	return err
}

func AppendLeaderActivity(tx *Tx, events []domain.LeaderActivityEvent) error {
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		ref := event.Execution
		_, err = tx.tx.ExecContext(tx.ctx, `INSERT OR IGNORE INTO leader_activity
 (kind,execution_id,generation,input_version,event_key,data) VALUES(?,?,?,?,?,?)`,
			ref.Kind, ref.ID, ref.Generation, ref.InputVersion, event.Key, data)
		if err != nil {
			return err
		}
	}
	_, err := tx.tx.ExecContext(tx.ctx, `DELETE FROM leader_activity WHERE sequence <
 (SELECT sequence FROM leader_activity ORDER BY sequence DESC LIMIT 1 OFFSET ?)`, domain.MaxLeaderEvents-1)
	return err
}

func ReadLeaderActivity(tx *Tx, query domain.LeaderActivityQuery, refs []domain.LeaderExecutionRef) (domain.LeaderActivityPage, error) {
	page := domain.LeaderActivityPage{Version: domain.LeaderActivityVersion, Events: []domain.LeaderActivityEvent{}}
	var oldest, latest int64
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT COALESCE(MIN(sequence),0),COALESCE(MAX(sequence),0) FROM leader_activity`).Scan(&oldest, &latest)
	if err != nil {
		return page, err
	}
	page.Oldest, page.Cursor = oldest, latest
	if query.After > latest {
		return page, domain.Fail("invalid_cursor", "activity cursor is ahead of this center; reconnect from recent history")
	}
	page.Truncated = oldest > query.After+1
	if len(refs) == 0 {
		return page, nil
	}
	var clauses []string
	args := []any{query.After}
	for _, ref := range refs {
		comparison := "<="
		if query.Generation > 0 {
			comparison = "="
			ref.Generation = query.Generation
		}
		clauses = append(clauses, "(kind=? AND execution_id=? AND generation"+comparison+"?)")
		args = append(args, ref.Kind, ref.ID, ref.Generation)
	}
	order := "ASC"
	if query.After == 0 {
		order = "DESC"
	}
	args = append(args, query.Limit+1)
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT sequence,data FROM leader_activity WHERE sequence>? AND (`+
		strings.Join(clauses, " OR ")+") ORDER BY sequence "+order+" LIMIT ?", args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var data []byte
		var event domain.LeaderActivityEvent
		if err := rows.Scan(&seq, &data); err != nil {
			return page, err
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return page, err
		}
		event.Sequence = seq
		page.Events = append(page.Events, event)
	}
	return finishLeaderActivityPage(page, query), rows.Err()
}

func finishLeaderActivityPage(page domain.LeaderActivityPage, query domain.LeaderActivityQuery) domain.LeaderActivityPage {
	if len(page.Events) > query.Limit {
		page.Events = page.Events[:query.Limit]
		page.HasMore = query.After > 0
		page.Truncated = page.Truncated || query.After == 0
	}
	if query.After == 0 {
		for i, j := 0, len(page.Events)-1; i < j; i, j = i+1, j-1 {
			page.Events[i], page.Events[j] = page.Events[j], page.Events[i]
		}
	}
	if page.HasMore && len(page.Events) > 0 {
		page.Cursor = page.Events[len(page.Events)-1].Sequence
	}
	return page
}
