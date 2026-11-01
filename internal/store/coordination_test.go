package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
)

func TestWorkHistoryScopesBeforePaginationAndKeepsSnapshotOrder(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, in := range []struct{ id, author, task string }{{"note_a", "chat_a", "task_one"}, {"note_b", "chat_b", "task_one"}, {"note_c", "chat_a", "task_two"}} {
		err = db.Update(ctx, func(tx *Tx) error {
			_, e := AppendWorkNote(tx, domain.WorkNote{ID: in.id, ProjectID: "project", AuthorID: in.author, Anchor: domain.WorkAnchor{Kind: "task", ID: in.task}, References: []domain.WorkReference{{Kind: "run", ID: "run_one"}}})
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var upper int64
	var page []domain.WorkNote
	err = db.View(ctx, func(tx *Tx) error {
		var e error
		upper, e = WorkUpper(tx, "project", "chat_a")
		if e != nil {
			return e
		}
		page, e = ReadWorkNotes(tx, WorkRead{ProjectID: "project", AuthorID: "chat_a", Upper: upper, Limit: 1}, true)
		return e
	})
	if err != nil || len(page) != 2 || page[0].ID != "note_c" || page[1].ID != "note_a" {
		t.Fatal("scope/extra row pagination", page, err)
	}
	err = db.Update(ctx, func(tx *Tx) error {
		_, e := AppendWorkNote(tx, domain.WorkNote{ID: "note_d", ProjectID: "project", AuthorID: "chat_a", Anchor: domain.WorkAnchor{Kind: "task", ID: "task_one"}})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(ctx, func(tx *Tx) error {
		rows, e := ReadWorkNotes(tx, WorkRead{ProjectID: "project", AuthorID: "chat_a", Upper: upper, Before: page[0].Sequence, Limit: 1}, true)
		if e == nil && (len(rows) != 1 || rows[0].ID != "note_a") {
			t.Fatal("new notes changed an existing page", rows)
		}
		refs, e := ReadWorkReferences(tx, WorkRead{ProjectID: "project", AuthorID: "chat_b", Anchor: domain.WorkAnchor{Kind: "task", ID: "task_two"}, Upper: upper, Limit: 16})
		if len(refs) != 0 {
			t.Fatal("foreign topic references leaked", refs)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkNotesRollbackWithReferencesAndRotateOnlyOnRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var before string
	if err = db.View(ctx, func(tx *Tx) error { var e error; before, e = CoordinationEpoch(tx); return e }); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("rollback")
	err = db.Update(ctx, func(tx *Tx) error {
		if _, e := AppendWorkNote(tx, domain.WorkNote{ID: "rollback", ProjectID: "p", AuthorID: "a", Anchor: domain.WorkAnchor{Kind: "task", ID: "t"}, References: []domain.WorkReference{{Kind: "run", ID: "r"}}}); e != nil {
			return e
		}
		if e := RenewCoordinationEpoch(tx); e != nil {
			return e
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.View(ctx, func(tx *Tx) error {
		epoch, e := CoordinationEpoch(tx)
		if e != nil {
			return e
		}
		if epoch != before {
			t.Fatal("normal restart or rollback rotated epoch")
		}
		if _, e = WorkNote(tx, "rollback", "p", "a"); !errors.Is(e, domain.ErrNotFound) {
			t.Fatal("rolled back note survived", e)
		}
		refs, e := ReadWorkReferences(tx, WorkRead{ProjectID: "p", AuthorID: "a", Anchor: domain.WorkAnchor{Kind: "task", ID: "t"}, Upper: 99, Limit: 16})
		if len(refs) != 0 {
			t.Fatal("orphan reference", refs)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Update(ctx, RenewCoordinationEpoch); err != nil {
		t.Fatal(err)
	}
	if err = db.View(ctx, func(tx *Tx) error {
		epoch, e := CoordinationEpoch(tx)
		if epoch == before {
			t.Fatal("recovery did not rotate epoch")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
