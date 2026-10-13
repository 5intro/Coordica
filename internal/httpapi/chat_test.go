package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestChatRoutesIsolateOwnerNodeRunAndRemoteSurfaces(t *testing.T) {
	f := newBoundaryFixture(t)
	owner, e := f.server.Service.Authenticate(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.server.Service.CreateProject(context.Background(), owner, "project", domain.Project{Name: "Project", RepositoryURL: "https://example.com/repo", TargetBranch: "main", TotalBudgetSeconds: 10000, Checks: []domain.Check{{Name: "minimum", Command: "git", Args: []string{"status"}, TimeoutSeconds: 1}}}); e != nil {
		t.Fatal(e)
	}
	raw, e := f.server.Service.CreateChatCredential(context.Background(), owner, "client", domain.ChatCredentialRequest{Name: "Chat"})
	if e != nil {
		t.Fatal(e)
	}
	var grant domain.ChatCredentialGrant
	if json.Unmarshal(raw, &grant) != nil {
		t.Fatal("grant")
	}
	for _, handler := range []http.Handler{f.server.AdminHandler(), f.server.Handler()} {
		if w := boundaryRequest(handler, "GET", "/api/v1/chat/overview", grant.Token, ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, token := range []string{f.owner, f.node.Token} {
			if w := boundaryRequest(handler, "GET", "/api/v1/chat/overview", token, ""); w.Code != 403 {
				t.Fatal("wrong role gained chat rights", w.Code)
			}
		}
	}
	for _, route := range []string{"state", "export", "system/connection", "runner/state", "runs/run_abc/context"} {
		if w := boundaryRequest(f.server.Handler(), "GET", "/api/v1/"+route, grant.Token, ""); w.Code != 403 {
			t.Fatal("chat read protected route", route, w.Code)
		}
	}
	for _, route := range []string{"planning/save", "planning/evaluate", "planning/authorize", "pairings", "tasks", "runner/claim", "nodes/n/revoke", "project/pause", "chat-credentials", "runs/r/confirm-stopped"} {
		if w := boundaryRequest(f.server.Handler(), "POST", "/api/v1/"+route, grant.Token, `{}`); w.Code != 403 {
			t.Fatal("chat reached owner/run write", route, w.Code)
		}
	}
	for _, route := range []string{"state", "export", "system/connection", "chat-credentials"} {
		if w := boundaryRequest(f.server.RunnerHandler(), "GET", "/api/v1/"+route, f.owner, ""); w.Code != 404 {
			t.Fatal("Owner on remote listener", route, w.Code)
		}
	}
	if w := boundaryRequest(f.server.RunnerHandler(), "GET", "/", f.owner, ""); w.Code != 404 {
		t.Fatal("console on remote surface")
	}
	for _, path := range []string{"/api/v1/chat/state", "/api/v1/chat/approve", "/api/v1/chat/runs"} {
		if w := boundaryRequest(f.server.Handler(), "POST", path, grant.Token, `{}`); w.Code != 404 {
			t.Fatal("unknown chat route", w.Code)
		}
	}
	// Remote chat routes are bounded JSON only; they cannot accept Owner tokens.
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/chat/items", grant.Token, `{"kind":"tasks","limit":21}`); w.Code != 403 {
		t.Fatal("center-chat on remote page", w.Code)
	}
}
func TestChatHTTPStrictInputRedactionAndRevokedReplay(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.server.Service.CreateProject(ctx, owner, "p", domain.Project{Name: "Project", RepositoryURL: "https://example.com/repo", TargetBranch: "main", TotalBudgetSeconds: 10000, Checks: []domain.Check{{Name: "minimum", Command: "git", Args: []string{"status"}, TimeoutSeconds: 1}}})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.server.Service.CreateChatCredential(ctx, owner, "client", domain.ChatCredentialRequest{Name: "Chat"})
	if e != nil {
		t.Fatal(e)
	}
	var grant domain.ChatCredentialGrant
	_ = json.Unmarshal(raw, &grant)
	handler := f.server.AdminHandler()
	for _, body := range []string{`{"kind":"tasks","kind":"requirements","limit":1}`, `{"kind":"tasks","limit":1,"url":"https://evil"}`, `null`, `[]`, `{} {}`, `{"kind":"tasks","limit":1.5}`, `{"kind":"tasks","limit":0}`} {
		w := boundaryRequest(handler, "POST", "/api/v1/chat/items", grant.Token, body)
		if w.Code != 400 {
			t.Fatal("invalid accepted", body, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"original_text":"advice","source":{"session_id":"s","message_id":"m"}}`, `{"expected_goal_version":null,"original_text":"advice","source":{"session_id":"s","message_id":"m"}}`, `{"expected_goal_version":0,"original_text":"advice","source":{"session_id":"s","session_id":"x","message_id":"m"}}`} {
		if w := boundaryRequest(handler, "POST", "/api/v1/chat/consultations", grant.Token, body); w.Code != 400 {
			t.Fatal("missing/ambiguous goal gate", w.Code, w.Body.String())
		}
	}
	body := `{"title":"Explicit","original_text":"/Users/private/owner.token token=sample-secret","assistant_summary":"Summary","source":{"session_id":"s","message_id":"m"}}`
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/chat/requirements", strings.NewReader(body))
		r.Host = "127.0.0.1:8080"
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+grant.Token)
		r.Header.Set("Idempotency-Key", "stable")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first := request()
	if first.Code != 200 || strings.Contains(first.Body.String(), "sample-secret") || strings.Contains(first.Body.String(), "/Users/private") {
		t.Fatal(first.Code, first.Body.String())
	}
	if second := request(); second.Body.String() != first.Body.String() {
		t.Fatal("not idempotent")
	}
	if _, e = f.server.Service.RevokeChatCredential(ctx, owner, "revoke", grant.Credential.ID); e != nil {
		t.Fatal(e)
	}
	if w := request(); w.Code != 401 {
		t.Fatal("revoked replay", w.Code)
	}
}
