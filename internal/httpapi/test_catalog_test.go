package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestCatalogHTTPAuthorityStrictInputAndPublicContract(t *testing.T) {
	f := newBoundaryFixture(t)
	s := f.server.Service
	a, err := s.Authenticate(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(context.Background(), a, "catalog-project", domain.Project{
		Name: "Catalog", RepositoryURL: "https://example.com/repo", TargetBranch: "main",
		Checks: []domain.Check{{Name: "check", Command: "git", TimeoutSeconds: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := s.CreateChatCredential(context.Background(), a, "catalog-chat", domain.ChatCredentialRequest{Name: "Limited"})
	if err != nil {
		t.Fatal(err)
	}
	var grant domain.ChatCredentialGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"tests/cases", "tests/acceptance", "tests/supplements", "tests/acceptance/plan",
		"tests/acceptance/signoff"} {
		for _, method := range []string{"GET", "POST"} {
			for _, token := range []string{f.node.Token, grant.Token} {
				w := boundaryRequest(f.server.AdminHandler(), method, "/api/v1/"+path, token, `{}`)
				if w.Code != http.StatusForbidden {
					t.Fatal("Owner authority leaked", path, method, w.Code)
				}
			}
			w := boundaryRequest(f.server.RunnerHandler(), method, "/api/v1/"+path, f.owner, `{}`)
			if w.Code != http.StatusNotFound {
				t.Fatal("test policy exposed remotely", path, method, w.Code)
			}
		}
	}
	for _, query := range []string{"limit=51", "limit=1&limit=2", "after=../x", "category=invalid", "unknown=1"} {
		if w := boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/tests/cases?"+query, f.owner, ""); w.Code != 400 {
			t.Fatal("invalid query accepted", query, w.Code)
		}
	}
	body := `{"expected_version":0,"case":{"id":"case_one","name":"一次支付","purpose":"避免重复扣款",` +
		`"categories":["regression"],"steps":["重试"],"assertions":["仅一次"],"enabled":true,"implemented":false}}`
	for _, invalid := range []string{`null`, body + " {}", strings.Replace(body, `"expected_version":0`, `"extra":1`, 1)} {
		if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/tests/cases", f.owner, invalid); w.Code != 400 {
			t.Fatal("invalid body accepted", w.Code, w.Body.String())
		}
	}
	if w := boundaryRequest(f.server.AdminHandler(), "POST", "/api/v1/tests/cases", f.owner, body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := boundaryRequest(f.server.AdminHandler(), "GET", "/api/v1/tests/cases?limit=1", f.owner, "")
	var page domain.TestCasePage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 {
		t.Fatal("catalog contract", w.Code, err)
	}
}
