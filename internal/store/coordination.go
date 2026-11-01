package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"coordica/internal/domain"
)

func initializeCoordination(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS coordination_instance(id INTEGER PRIMARY KEY CHECK(id=1),epoch TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS coordination_entry(
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,project_id TEXT NOT NULL,
 author_id TEXT NOT NULL,anchor_kind TEXT NOT NULL,anchor_id TEXT NOT NULL,data BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS coordination_anchor ON coordination_entry(project_id,anchor_kind,anchor_id,sequence);
 CREATE INDEX IF NOT EXISTS coordination_author ON coordination_entry(project_id,author_id,sequence);
 CREATE TABLE IF NOT EXISTS coordination_reference(
 entry_sequence INTEGER NOT NULL REFERENCES coordination_entry(sequence),kind TEXT NOT NULL,object_id TEXT NOT NULL,
 PRIMARY KEY(entry_sequence,kind,object_id));`)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT OR IGNORE INTO coordination_instance(id,epoch) VALUES(1,?)", coordinationNonce())
	return err
}

func coordinationNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func CoordinationEpoch(tx *Tx) (string, error) {
	var epoch string
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT epoch FROM coordination_instance WHERE id=1").Scan(&epoch)
	return epoch, err
}

func RenewCoordinationEpoch(tx *Tx) error {
	_, err := tx.tx.ExecContext(tx.ctx, "UPDATE coordination_instance SET epoch=? WHERE id=1", coordinationNonce())
	return err
}

// Queries are scoped by the application-provided principal, never by a cursor.
// Empty AuthorID is reserved for the independently authenticated Owner.
type WorkRead struct {
	ProjectID      string
	AuthorID       string
	Anchor         domain.WorkAnchor
	Kind           string
	Before         int64
	Upper          int64
	Limit          int
	AfterReference string
}

func AppendWorkNote(tx *Tx, note domain.WorkNote) (domain.WorkNote, error) {
	data, err := json.Marshal(note)
	if err != nil {
		return note, err
	}
	result, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO coordination_entry(id,project_id,author_id,anchor_kind,anchor_id,data)
 VALUES(?,?,?,?,?,?)`, note.ID, note.ProjectID, note.AuthorID, note.Anchor.Kind, note.Anchor.ID, data)
	if err != nil {
		return note, err
	}
	note.Sequence, err = result.LastInsertId()
	if err != nil {
		return note, err
	}
	for _, ref := range note.References {
		if _, err = tx.tx.ExecContext(tx.ctx, "INSERT INTO coordination_reference(entry_sequence,kind,object_id) VALUES(?,?,?)", note.Sequence, ref.Kind, ref.ID); err != nil {
			return note, err
		}
	}
	return note, nil
}

func WorkNote(tx *Tx, id, projectID, authorID string) (domain.WorkNote, error) {
	var n domain.WorkNote
	var data []byte
	var seq int64
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT sequence,data FROM coordination_entry WHERE id=? AND project_id=?
 AND (?='' OR author_id=?)`, id, projectID, authorID, authorID).Scan(&seq, &data)
	if err == sql.ErrNoRows {
		return n, domain.ErrNotFound
	}
	if err != nil {
		return n, err
	}
	err = json.Unmarshal(data, &n)
	n.Sequence = seq
	return n, err
}

func WorkUpper(tx *Tx, projectID, authorID string) (int64, error) {
	var seq int64
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT COALESCE(MAX(sequence),0) FROM coordination_entry WHERE project_id=? AND (?='' OR author_id=?)", projectID, authorID, authorID).Scan(&seq)
	return seq, err
}

func HasWorkHistory(tx *Tx, q WorkRead) (bool, error) {
	var exists bool
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT EXISTS(SELECT 1 FROM coordination_entry
 WHERE project_id=? AND author_id=? AND anchor_kind=? AND anchor_id=?)`, q.ProjectID, q.AuthorID, q.Anchor.Kind, q.Anchor.ID).Scan(&exists)
	return exists, err
}

func ReadWorkNotes(tx *Tx, q WorkRead, topics bool) ([]domain.WorkNote, error) {
	query := `SELECT e.sequence,e.data FROM coordination_entry e WHERE e.project_id=? AND (?='' OR e.author_id=?) AND e.sequence<=?`
	args := []any{q.ProjectID, q.AuthorID, q.AuthorID, q.Upper}
	if topics {
		query += ` AND e.sequence=(SELECT MAX(x.sequence) FROM coordination_entry x WHERE x.project_id=e.project_id
 AND x.anchor_kind=e.anchor_kind AND x.anchor_id=e.anchor_id AND (?='' OR x.author_id=?) AND x.sequence<=?)`
		args = append(args, q.AuthorID, q.AuthorID, q.Upper)
		if q.Kind != "" {
			query += " AND e.anchor_kind=?"
			args = append(args, q.Kind)
		}
	} else {
		query += " AND e.anchor_kind=? AND e.anchor_id=?"
		args = append(args, q.Anchor.Kind, q.Anchor.ID)
	}
	if q.Before > 0 {
		query += " AND e.sequence<?"
		args = append(args, q.Before)
	}
	query += " ORDER BY e.sequence DESC LIMIT ?"
	args = append(args, q.Limit+1)
	rows, err := tx.tx.QueryContext(tx.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := []domain.WorkNote{}
	for rows.Next() {
		var n domain.WorkNote
		var data []byte
		var seq int64
		if err = rows.Scan(&seq, &data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &n); err != nil {
			return nil, err
		}
		n.Sequence = seq
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// References are an index of the same immutable notes, not another workflow.
func ReadWorkReferences(tx *Tx, q WorkRead) ([]domain.WorkReference, error) {
	rows, err := tx.tx.QueryContext(tx.ctx, `WITH latest AS (SELECT r.kind,r.object_id,MAX(e.sequence) AS sequence FROM coordination_reference r
 JOIN coordination_entry e ON e.sequence=r.entry_sequence
 WHERE e.project_id=? AND (?='' OR e.author_id=?) AND e.anchor_kind=? AND e.anchor_id=? AND e.sequence<=?
 AND r.kind||':'||r.object_id>? GROUP BY r.kind,r.object_id ORDER BY r.kind,r.object_id LIMIT ?)
 SELECT latest.kind,latest.object_id,e.data FROM latest JOIN coordination_entry e ON e.sequence=latest.sequence ORDER BY latest.kind,latest.object_id`,
		q.ProjectID, q.AuthorID, q.AuthorID, q.Anchor.Kind, q.Anchor.ID, q.Upper, q.AfterReference, q.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []domain.WorkReference{}
	for rows.Next() {
		var ref domain.WorkReference
		var data []byte
		if err = rows.Scan(&ref.Kind, &ref.ID, &data); err != nil {
			return nil, err
		}
		var note domain.WorkNote
		if err = json.Unmarshal(data, &note); err != nil {
			return nil, err
		}
		for _, saved := range note.References {
			if saved.Kind == ref.Kind && saved.ID == ref.ID {
				ref = saved
				break
			}
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
