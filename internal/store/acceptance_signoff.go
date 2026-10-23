package store

import (
	"encoding/json"

	"coordica/internal/domain"
)

// The fixed, application-generated key selects a primary-key range; unrelated
// requirements and old history never enter the in-memory projection.
func RecentAcceptanceSignoffs(tx *Tx, key string) ([]domain.AcceptanceSignoff, error) {
	items := []domain.AcceptanceSignoff{}
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT data FROM records
 WHERE kind='acceptance_signoff_revision' AND id>=? AND id<? ORDER BY id DESC LIMIT 10`, key+"_v", key+"_w")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var item domain.AcceptanceSignoff
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
