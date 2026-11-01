package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"coordica/internal/domain"
)

func TestWorkHTTPSharedUseCaseAndListenerAudience(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.server.Service.CreateProject(ctx, owner, "project", domain.Project{Name: "Work", RepositoryURL: "https://example.test/repo", TargetBranch: "main", Checks: []domain.Check{{Name: "minimum", Command: "git", Args: []string{"status"}, TimeoutSeconds: 1}}})
	if e != nil {
		t.Fatal(e)
	}
	var p domain.Project
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	b, e = f.server.Service.CreateChatCredential(ctx, owner, "chat", domain.ChatCredentialRequest{Name: "Center"})
	if e != nil {
		t.Fatal(e)
	}
	var grant domain.ChatCredentialGrant
	_ = json.Unmarshal(b, &grant)
	node, e := f.server.Service.Authenticate(ctx, f.node.Token)
	if e != nil {
		t.Fatal(e)
	}
	b, e = f.server.Service.CreateNodeChatCredential(ctx, node, "node-chat", domain.ChatCredentialRequest{Name: "Node"})
	if e != nil {
		t.Fatal(e)
	}
	var nodeGrant domain.ChatCredentialGrant
	_ = json.Unmarshal(b, &nodeGrant)
	query, _ := json.Marshal(domain.WorkContextQuery{Anchor: domain.WorkAnchor{Kind: "project", ID: p.ID}, Limit: 10})
	for _, test := range []struct {
		handler     http.Handler
		token, path string
		want        int
	}{
		{f.server.AdminHandler(), f.owner, "/api/v1/work/context", 200},
		{f.server.AdminHandler(), grant.Token, "/api/v1/chat/work/context", 200},
		{f.server.RunnerHandler(), nodeGrant.Token, "/api/v1/chat/work/context", 200},
		{f.server.RunnerHandler(), grant.Token, "/api/v1/chat/work/context", 403},
		{f.server.AdminHandler(), nodeGrant.Token, "/api/v1/chat/work/context", 403},
		{f.server.RunnerHandler(), f.owner, "/api/v1/work/context", 404},
		{f.server.RunnerHandler(), f.owner, "/api/v1/chat/work/context", 403},
		{f.server.AdminHandler(), grant.Token, "/api/v1/work/context", 403},
	} {
		w := boundaryRequest(test.handler, "POST", test.path, test.token, string(query))
		if w.Code != test.want {
			t.Fatal(test.path, w.Code, w.Body.String())
		}
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/chat/work/context", grant.Token, string(query))
	var view domain.WorkContext
	if e = json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	in := domain.WorkNoteInput{Anchor: view.Anchor, ExpectedStamp: view.Current.Reference.Stamp, Kind: "note", OriginalText: "Save only", References: []domain.WorkReference{}, Source: domain.ChatSource{SessionID: "s", MessageID: "m"}}
	body, _ := json.Marshal(in)
	w = boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/chat/work/notes", grant.Token, string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/work/context", f.owner, string(query))
	if e = json.Unmarshal(w.Body.Bytes(), &view); e != nil || len(view.Notes) != 1 || view.Notes[0].OriginalText != "Save only" {
		t.Fatal("HTTP used separate state", w.Body.String(), e)
	}
	for _, invalid := range []string{`null`, `{"limit":1,"limit":2}`, `{"limit":null}`, `{"limit":1,"url":"https://evil"}`, `{"limit":1,"kind":"run"}`} {
		w = boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/chat/work/contexts", grant.Token, invalid)
		if w.Code != 400 {
			t.Fatal("strict work decoder", invalid, w.Code, w.Body.String())
		}
	}
}
