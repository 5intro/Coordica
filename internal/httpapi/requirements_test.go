package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"coordica/internal/domain"
)

func TestRequirementLifecycleRoutesAreVersionedAndOwnerOnly(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, e := f.server.Service.Authenticate(ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.server.Service.CreateProject(ctx, owner, "project", domain.Project{Name: "Test", RepositoryURL: f.server.Service.Dir, TargetBranch: "main", Checks: []domain.Check{{Name: "check", Command: "git", TimeoutSeconds: 1}}})
	if e != nil {
		t.Fatal(e)
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/requirements", f.owner, `{"title":"Initial","body":"Expected behavior","state":"withdrawn","content_revision":99}`)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var req domain.Requirement
	if e = json.Unmarshal(w.Body.Bytes(), &req); e != nil {
		t.Fatal(e)
	}
	if req.State != "active" || req.ContentRevision != 1 {
		t.Fatal("forged record metadata accepted")
	}
	path := "/api/v1/requirements/" + req.ID
	for _, h := range []http.Handler{f.server.RunnerHandler(), f.server.AdminHandler()} {
		w = boundaryRequest(h, "POST", path+"/withdraw", f.node.Token, `{"expected_version":1,"reason":"forged"}`)
		if w.Code == 200 {
			t.Fatal("node edited formal requirement")
		}
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", path+"/revise", f.owner, `{"expected_version":1,"title":"Changed","body":"Revised expected behavior","reason":"Need changed"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", path+"/withdraw", f.owner, `{"expected_version":1,"reason":"stale form"}`)
	if w.Code == 200 {
		t.Fatal("stale write accepted")
	}
	w = boundaryRequest(f.server.AdminHandler(), "GET", path, f.owner, "")
	var detail domain.RequirementDetail
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || len(detail.Revisions) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	if detail.Requirement.Title != "Changed" || detail.Revisions[1].Requirement.Title != "Initial" {
		t.Fatal("history changed")
	}
	w = boundaryRequest(f.server.AdminHandler(), "POST", path+"/withdraw", f.owner, `{"expected_version":2,"reason":"No longer needed"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?before=-1", "?before=bad"} {
		w = boundaryRequest(f.server.AdminHandler(), "GET", path+suffix, f.owner, "")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Center chat may propose a change through its existing advisory channel;
	// it never inherits these formal Owner routes.
	b, e := f.server.Service.CreateChatCredential(ctx, owner, "chat", domain.ChatCredentialRequest{Name: "Test chat"})
	if e != nil {
		t.Fatal(e)
	}
	var grant domain.ChatCredentialGrant
	json.Unmarshal(b, &grant)
	w = boundaryRequest(f.server.AdminHandler(), "POST", path+"/revise", grant.Token, `{"expected_version":3,"title":"forged","body":"forged","reason":"no"}`)
	if w.Code == 200 {
		t.Fatal("interaction credential changed decision")
	}
}
