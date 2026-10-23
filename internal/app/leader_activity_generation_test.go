package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLeaderActivityExactGenerationNeverBorrowsCurrentExecutionMetadata(t *testing.T) {
	f := newServiceFixture(t)
	ref := activityRun(t, f)
	old := ref
	old.Generation, old.InputVersion = 1, 1
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{
			{Execution: old, Key: "old", Kind: "message", Text: "旧轮次"},
			{Execution: ref, Key: "current", Kind: "message", Text: "当前轮次"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	query := domain.LeaderActivityQuery{Kind: ref.Kind, ID: ref.ID, Generation: 1, Limit: 10}
	for _, after := range []int64{0, 2} {
		query.After = after
		page, err := (LeaderActivities{f.s}).Read(context.Background(), f.owner, query)
		if err != nil || len(page.Executions) != 1 {
			t.Fatal(page, err)
		}
		head := page.Executions[0]
		if head.Ref != old || head.Version != 0 || head.TreeStopped || head.Settled || head.State != "history_only" {
			t.Fatal("old generation borrowed live state or input version", head)
		}
		for _, event := range page.Events {
			if event.Execution != head.Ref {
				t.Fatal("selected events and execution metadata disagree", page)
			}
		}
	}
	query.Generation, query.After = 2, 0
	page, err := (LeaderActivities{f.s}).Read(context.Background(), f.owner, query)
	if err != nil || len(page.Executions) != 1 || page.Executions[0].Ref != ref || page.Executions[0].State != "running" {
		t.Fatal("current generation lost authoritative state", page, err)
	}
}

func TestLeaderActivityMissingHistoryRemainsExplicitlyUnknown(t *testing.T) {
	f := newServiceFixture(t)
	ref := activityRun(t, f)
	query := domain.LeaderActivityQuery{Kind: ref.Kind, ID: ref.ID, Generation: 1, Limit: 10}
	page, err := (LeaderActivities{f.s}).Read(context.Background(), f.owner, query)
	if err != nil || len(page.Executions) != 1 {
		t.Fatal(page, err)
	}
	head := page.Executions[0]
	if head.Ref.Generation != 1 || head.Ref.InputVersion != 0 || head.State != "history_unavailable" || head.Settled || head.TreeStopped {
		t.Fatal("missing history borrowed current facts", head)
	}
	query.Generation = 3
	if _, err := (LeaderActivities{f.s}).Read(context.Background(), f.owner, query); err == nil {
		t.Fatal("future generation accepted")
	}
}
