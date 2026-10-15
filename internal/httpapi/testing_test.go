package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestTestingAPIOwnerBoundaryAndStrictContracts(t *testing.T) {
	f := newBoundaryFixture(t)
	owner, e := f.server.Service.Authenticate(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.server.Service.CreateProject(context.Background(), owner, "project", domain.Project{Name: "Testing", RepositoryURL: "https://example.com/repo", TargetBranch: "main", Checks: []domain.Check{{Name: "minimum", Command: "git", TimeoutSeconds: 1}}}); e != nil {
		t.Fatal(e)
	}
	seedHTTPBaseline(t, f.server.Service)
	raw, e := f.server.Service.CreateChatCredential(context.Background(), owner, "chat", domain.ChatCredentialRequest{Name: "Limited"})
	if e != nil {
		t.Fatal(e)
	}
	var chat domain.ChatCredentialGrant
	if e = json.Unmarshal(raw, &chat); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"tests/suites", "tests/run", "requirements/req_a/link"} {
		for _, token := range []string{f.node.Token, chat.Token} {
			if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/"+path, token, `{}`); w.Code != http.StatusForbidden {
				t.Fatal("non-owner reached test controls", path, w.Code)
			}
		}
		if w := boundaryRequest(f.server.RunnerHandler(), "POST", "/api/v1/"+path, f.owner, `{}`); w.Code != http.StatusNotFound {
			t.Fatal("test controls exposed remotely", path, w.Code)
		}
	}
	for _, body := range []string{`{"unexpected":true}`, `{"suite":{},"expected_version":0} {}`, `null`} {
		if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/tests/suites", f.owner, body); w.Code != 400 {
			t.Fatal("invalid test input accepted", w.Code, w.Body.String())
		}
	}
	body := `{"expected_version":0,"suite":{"id":"smoke","name":"Smoke","description":"Start up","stage":"project","enabled":true,"categories":["smoke"],"match_paths":[],"platforms":[],"requirement_ids":[],"checks":[{"name":"version","command":"git","args":["--version"],"timeout_seconds":1}]}}`
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/tests/suites", f.owner, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/tests/readiness?commit="+strings.Repeat("a", 40), f.owner, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ready":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
