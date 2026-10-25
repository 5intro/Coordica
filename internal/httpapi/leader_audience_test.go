package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestLeaderOriginalProseCannotCrossLimitedHTTPOrMCP(t *testing.T) {
	b := newPreparationBridge(t)
	f := b.fixture
	secret := "未公布的部署约定松石钟楼"
	if err := f.server.Service.DB.Update(context.Background(), func(tx *store.Tx) error {
		run := domain.PlanningRun{ID: "private_display", State: "running", Generation: 1, DocumentVersion: 1}
		if err := tx.Put("planning_run", run.ID, run); err != nil {
			return err
		}
		return store.AppendLeaderActivity(tx, []domain.LeaderActivityEvent{{Execution: domain.LeaderExecutionRef{
			Kind: "planning", ID: run.ID, Generation: 1, InputVersion: 1}, Key: "one", Kind: "message", Text: secret}})
	}); err != nil {
		t.Fatal(err)
	}
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.Handler()} {
		owner := boundaryRequest(handler, "GET", "/api/v1/leader/activity", f.owner, "")
		if owner.Code != http.StatusOK || !strings.Contains(owner.Body.String(), secret) {
			t.Fatal("Owner display lost original text", owner.Code, owner.Body.String())
		}
		limited := boundaryRequest(handler, "POST", "/api/v1/chat/leader/activity", b.chat.Token, `{"limit":10}`)
		if limited.Code != http.StatusOK || strings.Contains(limited.Body.String(), secret) ||
			!strings.Contains(limited.Body.String(), "Leader 已更新进展") {
			t.Fatal("private prose crossed limited HTTP projection", limited.Code, limited.Body.String())
		}
		raw, rejected := preparationMCP(t, handler, b.chat.Token, "read_leader_activity", map[string]any{"limit": 10})
		if rejected || strings.Contains(string(raw), secret) || !strings.Contains(string(raw), "Leader 已更新进展") {
			t.Fatal("private prose crossed limited MCP projection", string(raw), rejected)
		}
		if w := boundaryRequest(handler, "GET", "/api/v1/leader/activity/stream", b.chat.Token, ""); w.Code != http.StatusForbidden {
			t.Fatal("limited chat gained Owner stream", w.Code)
		}
	}
}
