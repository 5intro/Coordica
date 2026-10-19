package httpapi

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestPublicationRoutesStayOwnerOnly(t *testing.T) {
	f := newBoundaryFixture(t)
	a, e := f.server.Service.Authenticate(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.server.Service.CreateProject(context.Background(), a, "create", domain.Project{Name: "Hosted", RepositoryURL: "https://github.com/example/repo.git", TargetBranch: "main", Checks: []domain.Check{{Name: "check", Command: "git", TimeoutSeconds: 10}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/project/publication", "/api/v1/candidates/cand_example/sync"} {
		body := `{"expected_version":1,"policy":{"mode":"github","required_checks":["ci"]}}`
		if strings.Contains(path, "candidates") {
			body = `{"expected_version":1}`
		}
		if w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.node.Token, body); w.Code != 403 {
			t.Fatal("node reached owner publication API", w.Code, w.Body.String())
		}
		if w := boundaryRequest(f.server.RunnerHandler(), "POST", path, f.owner, body); w.Code != 403 && w.Code != 404 {
			t.Fatal("runner listener exposed publication API", w.Code)
		}
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/project/publication", f.owner, `{"expected_version":1,"policy":{"mode":"github","required_checks":["ci"]}}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"paused":true`) {
		t.Fatal("owner configure failed or resumed project", w.Code, w.Body.String())
	}
	if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/project/publication", f.owner, `{"expected_version":2,"policy":{"mode":"github","required_checks":["ci"],"token":"forbidden"}}`); w.Code != 400 {
		t.Fatal("credential accepted into policy", w.Code)
	}
}
