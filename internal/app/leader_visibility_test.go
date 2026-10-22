package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLeaderVisibilityFiltersBeforeInventoryLimit(t *testing.T) {
	f := newServiceFixture(t)
	chat, _ := chatFixture(t, f)
	err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		own := domain.ConversationRequest{ID: "own", Source: domain.ChatSource{ClientID: chat.ID}}
		if err := tx.Put("conversation", own.ID, own); err != nil {
			return err
		}
		if err := tx.Put("planning_run", "own_run", domain.PlanningRun{ID: "own_run", State: "succeeded", Generation: 1,
			Purpose: domain.ConsultationPurpose, ConversationID: own.ID}); err != nil {
			return err
		}
		if err := tx.Put("planning_run", "public", domain.PlanningRun{ID: "public", State: "succeeded", Generation: 1}); err != nil {
			return err
		}
		for i := 0; i < 25; i++ {
			id := fmt.Sprintf("other_%d", i)
			if err := tx.Put("conversation", id, domain.ConversationRequest{ID: id, Source: domain.ChatSource{ClientID: "other"}}); err != nil {
				return err
			}
			if err := tx.Put("planning_run", id, domain.PlanningRun{ID: id, State: "running", Generation: 1,
				Purpose: domain.ConsultationPurpose, ConversationID: id}); err != nil {
				return err
			}
		}
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{
			{Execution: domain.LeaderExecutionRef{Kind: "planning", ID: "own_run", Generation: 1}, Key: "own", Text: "my consultation"},
			{Execution: domain.LeaderExecutionRef{Kind: "planning", ID: "public", Generation: 1}, Key: "public", Text: "public planning"},
			{Execution: domain.LeaderExecutionRef{Kind: "planning", ID: "other_24", Generation: 1}, Key: "private", Text: "other private"},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	activities := LeaderActivities{Service: f.s}
	page, err := activities.Read(context.Background(), chat, domain.LeaderActivityQuery{Limit: 100})
	if err != nil || len(page.Executions) != 2 || len(page.Events) != 2 {
		t.Fatal("other clients crowded out visible records", page, err)
	}
	for _, head := range page.Executions {
		if head.Ref.ID != "own_run" && head.Ref.ID != "public" {
			t.Fatal("private execution exposed", head)
		}
	}
	if _, err := activities.Read(context.Background(), chat, domain.LeaderActivityQuery{
		Limit: 10, Kind: "planning", ID: "other_24",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("exact private ID was exposed", err)
	}
	page, err = activities.Read(context.Background(), f.owner, domain.LeaderActivityQuery{Limit: 100})
	if err != nil || len(page.Executions) != 20 {
		t.Fatal("Owner inventory changed bounds", page, err)
	}
}
