package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLeaderHistoricalGenerationMatchesAcrossOwnerHTTPAndUnifiedMCP(t *testing.T) {
	b := newPreparationBridge(t)
	f := b.fixture
	old := domain.LeaderExecutionRef{Kind: "planning", ID: "generation_case", Generation: 1, InputVersion: 1}
	if err := f.server.Service.DB.Update(context.Background(), func(tx *store.Tx) error {
		run := domain.PlanningRun{ID: old.ID, Generation: 2, DocumentVersion: 3, State: "running"}
		if err := tx.Put("planning_run", run.ID, run); err != nil {
			return err
		}
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{{Execution: old, Key: "old", Kind: "message", Text: "公开活动"}})
	}); err != nil {
		t.Fatal(err)
	}
	assertPage := func(raw []byte) {
		t.Helper()
		var page domain.LeaderActivityPage
		if err := json.Unmarshal(raw, &page); err != nil || len(page.Executions) != 1 || page.Executions[0].Ref != old ||
			page.Executions[0].State != "history_only" || len(page.Events) != 1 || page.Events[0].Execution != old {
			t.Fatal("historical reference mismatch", string(raw), err)
		}
	}
	w := boundaryRequest(f.server.AdminHandler(), "GET",
		"/api/v1/leader/activity?kind=planning&id=generation_case&generation=1&limit=10", f.owner, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertPage(w.Body.Bytes())
	args := map[string]any{"kind": "planning", "id": old.ID, "generation": 1, "limit": 10}
	raw, rejected := preparationMCP(t, f.server.AdminHandler(), b.chat.Token, "read_leader_activity", args)
	if rejected {
		t.Fatal("limited MCP history read rejected", string(raw))
	}
	assertPage(raw)
}
