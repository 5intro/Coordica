package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

var (
	ErrInvalid   = errors.New("invalid runtime ledger request")
	ErrForbidden = errors.New("runtime ledger access denied")
	ErrConflict  = errors.New("runtime ledger identity or version conflict")
	ErrCancelled = errors.New("runtime task or run is cancelled")
)

const (
	kindGrant    = "runtime.grant"
	kindInstance = "runtime.instance"
	kindTask     = "runtime.task"
	kindContext  = "runtime.context"
	kindRun      = "runtime.run"
	kindInput    = "runtime.input"
	maxContent   = 64 << 10
)

// Ledger is an in-process experimental API. No transport, worker, native
// session, autonomous wake or effect consumer is installed by constructing it.
type Ledger struct{ db *store.Store }

func New(db *store.Store) *Ledger { return &Ledger{db: db} }

func identifier(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) &&
		!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func content(s string) bool { return len(s) <= maxContent && utf8.ValidString(s) }

func sourceValid(s Source) bool { return identifier(s.ID, 128) && identifier(s.EventID, 256) }

func digest(v any) string {
	raw, _ := json.Marshal(v) // Callers use only the concrete string/integer DTOs in this package.
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func compound(parts ...string) string { return digest(parts) }

func freshID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func putNew(tx *store.Tx, kind, id string, value any) error {
	var old json.RawMessage
	err := tx.Get(kind, id, &old)
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return tx.Put(kind, id, value)
}

func taskFor(tx *store.Tx, grant Grant, id string) (Task, error) {
	var task Task
	err := tx.Get(kindTask, id, &task)
	if err == nil && task.ApplicationID != grant.ApplicationID {
		err = ErrForbidden
	}
	return task, err
}

func taskSnapshot(tx *store.Tx, task Task) (TaskSnapshot, error) {
	var material TaskContext
	err := tx.Get(kindContext, task.ContextID, &material)
	if err == nil && (material.TaskID != task.ID || material.ApplicationID != task.ApplicationID || material.Version != task.ContextVersion) {
		err = ErrConflict
	}
	return TaskSnapshot{Task: task, Context: material}, err
}

func writable(task Task, expected int64) error {
	if task.State == "cancelled" {
		return ErrCancelled
	}
	if task.State != "open" || task.Version != expected || task.Version <= 0 || task.Version == math.MaxInt64 {
		return ErrConflict
	}
	return nil
}

func audit(tx *store.Tx, actor, operation, id string) error {
	// The legacy owner can read global audit. Never copy credentials, hashes,
	// context, answers, external references or native private data into Detail.
	return tx.Audit(domain.Event{Time: time.Now().UTC(), Actor: actor, Kind: "runtime." + operation, ObjectID: id})
}

func mutate[T any](l *Ledger, ctx context.Context, credential Credential, action Action, key, op string,
	request any, check func(*store.Tx, Grant) error, change func(*store.Tx, Grant) (T, string, bool, error)) (T, error) {
	var value T
	if l == nil || l.db == nil || !identifier(key, 256) {
		return value, ErrInvalid
	}
	d := digest(struct {
		Operation string `json:"operation"`
		Request   any    `json:"request"`
	}{op, request})
	err := l.db.Update(ctx, func(tx *store.Tx) error {
		grant, err := authorize(tx, credential, action)
		if err != nil {
			return err
		}
		if err = check(tx, grant); err != nil {
			return err
		}
		actor := "runtime:" + compound(grant.ApplicationID, grant.ID)
		raw, found, err := tx.Replay(actor, key, d)
		if err != nil {
			return err
		}
		if found {
			return json.Unmarshal(raw, &value)
		}
		var id string
		var changed bool
		value, id, changed, err = change(tx, grant)
		if err != nil {
			return err
		}
		if changed {
			if err = audit(tx, grant.ID, op, id); err != nil {
				return err
			}
		}
		return tx.Remember(actor, key, d, value)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}
