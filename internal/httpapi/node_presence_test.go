package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"coordica/internal/domain"
)

func TestNodePresenceBeforeProjectInitialization(t *testing.T) {
	f := newBoundaryFixture(t)
	now := time.Now().UTC().Add(time.Hour)
	f.server.Service.Now = func() time.Time { return now }
	handler := f.server.RunnerHandler()
	body := `{"capabilities":[{"tool":"codex","available":true,"structured":true,"cancel":true}]}`
	w := boundaryRequest(handler, "POST", "/api/v1/runner/presence", f.node.Token, body)
	if w.Code != http.StatusOK {
		t.Fatal("paired node cannot report before initialization", w.Code, w.Body.String())
	}
	w = boundaryRequest(handler, "GET", "/api/v1/runner/state", f.node.Token, "")
	var state domain.NodeSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || !state.Node.LastSeen.Equal(now) || len(state.Node.Capabilities) != 1 || len(state.Runs) != 0 {
		t.Fatal("presence did not refresh independently of work", err, w.Body.String())
	}
	before := state.Node
	for _, body := range []string{`{"capabilities":[],"node_id":"another"}`, `{"capabilities":[{"tool":"../shell"}]}`} {
		if w := boundaryRequest(handler, "POST", "/api/v1/runner/presence", f.node.Token, body); w.Code != http.StatusBadRequest {
			t.Fatal("malformed presence accepted", w.Code, w.Body.String())
		}
	}
	w = boundaryRequest(handler, "GET", "/api/v1/runner/state", f.node.Token, "")
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || !reflect.DeepEqual(before, state.Node) {
		t.Fatal("rejected presence changed node", err, w.Body.String())
	}
	if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/runner/presence", f.owner, body); w.Code != http.StatusNotFound {
		t.Fatal("presence exposed on owner-only listener", w.Code)
	}
}
