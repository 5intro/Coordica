// Package store owns SQLite durability. Business decisions belong to app.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	_ "modernc.org/sqlite"
)

// Store serializes transactions in this single-center application. SQLite also
// enforces uniqueness; a second writer cannot bypass idempotency constraints.
type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	changes chan struct{}
}
type Tx struct {
	tx  *sql.Tx
	ctx context.Context
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := localfile.Protect(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, changes: make(chan struct{})}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
 INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
 CREATE TABLE IF NOT EXISTS records(kind TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(kind,id));
 CREATE TABLE IF NOT EXISTS audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS idempotency(actor TEXT NOT NULL,key TEXT NOT NULL,digest TEXT NOT NULL,response BLOB NOT NULL,PRIMARY KEY(actor,key));
 CREATE TABLE IF NOT EXISTS outbox(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,object_id TEXT NOT NULL,delivered INTEGER NOT NULL DEFAULT 0);`)
	if err == nil {
		err = initializeLeaderActivity(db)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	var version int
	err = db.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil || version != 1 {
		db.Close()
		return nil, fmt.Errorf("unsupported database schema %d: %v", version, err)
	}
	if err = localfile.Protect(path); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, e := os.Stat(path + suffix); e == nil {
			if e = localfile.Protect(path + suffix); e != nil {
				db.Close()
				return nil, e
			}
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Update(ctx context.Context, fn func(*Tx) error) error {
	return s.transact(ctx, fn, true)
}
func (s *Store) transact(ctx context.Context, fn func(*Tx) error, notify bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before, after int64
	if notify {
		if err = tx.QueryRowContext(ctx, "SELECT total_changes()").Scan(&before); err != nil {
			return err
		}
	}
	if err = fn(&Tx{tx, ctx}); err != nil {
		return err
	}
	if notify {
		if err = tx.QueryRowContext(ctx, "SELECT total_changes()").Scan(&after); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err == nil && notify && after != before {
		close(s.changes)
		s.changes = make(chan struct{})
	}
	return err
}
func (s *Store) View(ctx context.Context, fn func(*Tx) error) error {
	return s.transact(ctx, fn, false)
}

// Subscribe before reading so a committed change cannot be missed between
// a bounded query and waiting. Read-only transactions never wake subscribers.
func (s *Store) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}
func (t *Tx) Get(kind, id string, out any) error {
	var data []byte
	err := t.tx.QueryRowContext(t.ctx, "SELECT data FROM records WHERE kind=? AND id=?", kind, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func (t *Tx) Put(kind, id string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO records(kind,id,data) VALUES(?,?,?)
 ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data WHERE records.data IS NOT excluded.data`, kind, id, b)
	return err
}
func (t *Tx) Delete(kind, id string) error {
	_, err := t.tx.ExecContext(t.ctx, "DELETE FROM records WHERE kind=? AND id=?", kind, id)
	return err
}
func List[T any](t *Tx, kind string) ([]T, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT data FROM records WHERE kind=? ORDER BY rowid", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		var b []byte
		var v T
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (t *Tx) Audit(e domain.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO audit(data) VALUES(?)", b)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO outbox(kind,object_id) VALUES(?,?)", e.Kind, e.ObjectID)
	return err
}
func (t *Tx) Events(limit int) ([]domain.Event, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT sequence,data FROM audit ORDER BY sequence DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Event, 0)
	for rows.Next() {
		var seq int64
		var b []byte
		var e domain.Event
		if err = rows.Scan(&seq, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		e.Sequence = seq
		result = append(result, e)
	}
	return result, rows.Err()
}
func (t *Tx) Replay(actor, key, digest string) (json.RawMessage, bool, error) {
	var old string
	var response []byte
	err := t.tx.QueryRowContext(t.ctx, "SELECT digest,response FROM idempotency WHERE actor=? AND key=?", actor, key).Scan(&old, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if old != digest {
		return nil, false, domain.Fail("idempotency_conflict", "同一幂等键不能用于不同请求")
	}
	return response, true, nil
}
func (t *Tx) Remember(actor, key, digest string, response any) error {
	b, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO idempotency(actor,key,digest,response) VALUES(?,?,?,?)", actor, key, digest, b)
	return err
}
func (s *Store) Backup(ctx context.Context, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(destination); err == nil {
		return errors.New("backup destination already exists")
	}
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", destination)
	if err == nil {
		err = os.Chmod(destination, 0600)
	}
	return err
}
func (s *Store) DrainOutbox(ctx context.Context) error {
	// The durable audit is the cursor-based event feed. Marking its notification
	// delivered does not delete the event, so reconnecting consumers can replay it.
	_, err := s.db.ExecContext(ctx, "UPDATE outbox SET delivered=1 WHERE delivered=0")
	return err
}
