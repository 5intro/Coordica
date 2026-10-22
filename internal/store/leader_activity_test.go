package store

import (
	"context"
	"coordica/internal/domain"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaderActivityRetentionPagingDuplicatesAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := domain.LeaderExecutionRef{Kind: "planning", ID: "run", Generation: 2, InputVersion: 3}
	events := make([]domain.LeaderActivityEvent, domain.MaxLeaderEvents+10)
	for i := range events {
		events[i] = domain.LeaderActivityEvent{Execution: ref, Key: fmt.Sprint(i), Kind: "message", Text: "公开记录", Time: time.Now()}
	}
	if err := db.Update(ctx, func(tx *Tx) error { return AppendLeaderActivity(tx, events) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx *Tx) error { return AppendLeaderActivity(tx, events[len(events)-1:]) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := domain.LeaderActivityQuery{Limit: 3}
	read := func() domain.LeaderActivityPage {
		t.Helper()
		var page domain.LeaderActivityPage
		if err := db.View(ctx, func(tx *Tx) error {
			var err error
			page, err = ReadLeaderActivity(tx, query, []domain.LeaderExecutionRef{ref})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return page
	}
	latest := read()
	if len(latest.Events) != 3 || !latest.Truncated || latest.Events[2].Key != fmt.Sprint(len(events)-1) {
		t.Fatal(latest)
	}
	query.After = 1
	first := read()
	if !first.Truncated || !first.HasMore || first.Oldest != 11 || first.Cursor != first.Events[2].Sequence {
		t.Fatal(first)
	}
	query.After = first.Cursor
	second := read()
	if len(second.Events) != 3 || second.Events[0].Sequence <= first.Cursor {
		t.Fatal(second)
	}
	query.After = latest.Cursor + 1
	if err := db.View(ctx, func(tx *Tx) error { _, err := ReadLeaderActivity(tx, query, nil); return err }); err == nil {
		t.Fatal("future cursor accepted")
	}
}

func TestStoreSubscriptionsWakeOnlyAfterCommit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	changed := db.Changes()
	_ = db.View(context.Background(), func(*Tx) error { return nil })
	_ = db.Update(context.Background(), func(*Tx) error { return fmt.Errorf("rollback") })
	select {
	case <-changed:
		t.Fatal("read/rollback woke subscription")
	default:
	}
	_ = db.Update(context.Background(), func(*Tx) error { return nil })
	select {
	case <-changed:
	default:
		t.Fatal("commit wake lost")
	}
}

func TestLeaderHeadsPreservePurposeDefaults(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	err = db.Update(ctx, func(tx *Tx) error {
		for id, purpose := range map[string]string{"evaluation": "", "consultation": "consultation"} {
			if err := tx.Put("planning_run", id, domain.PlanningRun{ID: id, Purpose: purpose}); err != nil {
				return err
			}
		}
		return tx.Put("initialization_operation", "init", domain.InitializationOperation{ID: "init"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ kind, id, purpose string }{
		{"planning", "evaluation", ""}, {"planning", "consultation", "consultation"}, {"initialization", "init", "initialization"},
	} {
		err := db.View(ctx, func(tx *Tx) error {
			heads, err := LeaderExecutionHeads(tx, test.kind, test.id)
			if err == nil && (len(heads) != 1 || heads[0].Purpose != test.purpose) {
				t.Fatal("wrong execution purpose", heads)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLeaderInitialSmallHistoryMarksRetentionLoss(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ref := domain.LeaderExecutionRef{Kind: "planning", ID: "old", Generation: 1, InputVersion: 1}
	events := []domain.LeaderActivityEvent{}
	for i := 0; i < domain.MaxLeaderEvents+40; i++ {
		current := ref
		if i >= 50 {
			current.ID = "new"
		}
		events = append(events, domain.LeaderActivityEvent{Execution: current, Key: fmt.Sprint(i), Kind: "message", Text: "activity"})
	}
	if err := db.Update(context.Background(), func(tx *Tx) error { return AppendLeaderActivity(tx, events) }); err != nil {
		t.Fatal(err)
	}
	if err := db.View(context.Background(), func(tx *Tx) error {
		page, err := ReadLeaderActivity(tx, domain.LeaderActivityQuery{Limit: 100}, []domain.LeaderExecutionRef{ref})
		if err == nil && (!page.Truncated || len(page.Events) != 10) {
			t.Fatalf("small incomplete history presented as complete: %+v", page)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
