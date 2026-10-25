package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
)

type leaderCatalogFixture struct{}

func (leaderCatalogFixture) DiscoverLeaders(context.Context) ([]domain.Capability, error) {
	return []domain.Capability{{Tool: "dsh", Installed: true, Available: true, Structured: true, Cancel: true,
		Review: true, ModelDiscovery: "reported", Models: []domain.ModelOption{{ID: "flash", ReasoningEfforts: []string{"high"}}}}}, nil
}

func TestLeaderConfigurationIsOwnerOnlyAndUnavailableOnRemoteSurface(t *testing.T) {
	b := newPreparationBridge(t)
	f := b.fixture
	f.server.Service.LeaderCatalog = leaderCatalogFixture{}
	path := "/api/v1/leader/configuration"
	body := `{"expected_version":1,"selection":{"tool":"dsh","model":"flash","reasoning_effort":"high"}}`
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.RunnerHandler()} {
		for _, token := range []string{f.node.Token, b.chat.Token} {
			for _, method := range []string{"GET", "POST"} {
				if w := boundaryRequest(handler, method, path, token, body); w.Code == 200 {
					t.Fatal("limited credential gained Leader model authority", method)
				}
			}
		}
	}
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", path, f.owner, body); w.Code == 200 {
		t.Fatal("remote surface accepted Owner mutation")
	}
	w := boundaryRequest(f.server.AdminHandler(), "GET", path, f.owner, "")
	var config domain.LeaderConfiguration
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &config) != nil || len(config.Capabilities) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"leader_tool":"dsh"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// An ordinary unified-MCP conversation uses the saved Owner choice; it
	// does not acquire a new tool for changing that choice or granting work.
	opened := b.call(t, "open_project", map[string]any{"kind": "new", "expected_version": 0, "idempotency_key": "selected-open"})
	if opened.Leader == nil || opened.Leader.Tool != "dsh" || opened.Leader.Model != "flash" {
		t.Fatal(opened)
	}
	assertPreparationPaused(t, b)
}
