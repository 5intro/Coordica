package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"coordica/internal/domain"
)

func catalogMissing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func TestCases(tx *Tx, q domain.TestCaseQuery) (domain.TestCasePage, error) {
	page := domain.TestCasePage{Items: []domain.TestCase{}}
	query := `SELECT data FROM records WHERE kind='test_case' AND id>?`
	args := []any{q.After}
	if q.Category != "" {
		query += ` AND EXISTS(SELECT 1 FROM json_each(data,'$.categories') WHERE value=?)`
		args = append(args, q.Category)
	}
	if q.RequirementID != "" {
		query += ` AND EXISTS(SELECT 1 FROM json_each(data,'$.coverage') WHERE json_extract(value,'$.requirement_id')=?)`
		args = append(args, q.RequirementID)
	}
	query += ` ORDER BY id LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := tx.tx.QueryContext(tx.ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var item domain.TestCase
		if err := rows.Scan(&raw); err != nil {
			return page, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.Next = page.Items[q.Limit-1].ID
	}
	return page, rows.Err()
}

// Latest request wins, including queued, failed and cancelled attempts. Never
// fall back to an older green run when a newer attempt has no accepted result.
func LatestVerification(tx *Tx, suite, commit string) (domain.Task, domain.Run, error) {
	var task domain.Task
	var run domain.Run
	query := `SELECT data FROM records WHERE kind='task' AND json_type(data,'$.verification')='object'
 AND json_extract(data,'$.verification.suite.id')=? AND json_extract(data,'$.verification.commit')=?
 ORDER BY rowid DESC LIMIT 1`
	var raw []byte
	if err := tx.tx.QueryRowContext(tx.ctx, query, suite, commit).Scan(&raw); err != nil {
		return task, run, catalogMissing(err)
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return task, run, err
	}
	query = `SELECT data FROM records WHERE kind='run' AND json_extract(data,'$.task_id')=?
 ORDER BY json_extract(data,'$.generation') DESC,rowid DESC LIMIT 1`
	if err := tx.tx.QueryRowContext(tx.ctx, query, task.ID).Scan(&raw); err != nil {
		return task, run, catalogMissing(err)
	}
	err := json.Unmarshal(raw, &run)
	return task, run, err
}

func PinTestCatalog(tx *Tx, taskID, suiteID string) error {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT id,json_extract(data,'$.version') FROM records
 WHERE kind='test_case' AND json_extract(data,'$.suite_id')=?
 AND json_extract(data,'$.enabled')=1 ORDER BY id LIMIT 257`, suiteID)
	if err != nil {
		return err
	}
	pin := domain.TestCatalogPin{Versions: map[string]int64{}}
	for rows.Next() {
		var id string
		var version int64
		if err = rows.Scan(&id, &version); err != nil {
			break
		}
		pin.Versions[id] = version
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(pin.Versions) > 256 {
		return domain.Fail("invalid_input", "单套件最多绑定 256 个测试条目，请按职责拆分套件")
	}
	return tx.Put("test_catalog_pin", taskID, pin)
}
