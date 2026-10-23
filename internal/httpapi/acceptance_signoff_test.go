package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/app"
	"coordica/internal/domain"
	"coordica/internal/gitops"
)

func TestAcceptanceSignoffHTTPVersionedReplayAndRevocation(t *testing.T) {
	f := newBoundaryFixture(t)
	s := f.server.Service
	ctx := context.Background()
	a, err := s.Authenticate(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateProject(ctx, a, "signoff-project", domain.Project{Name: "Acceptance", RepositoryURL: "https://example.com/repo",
		TargetBranch: "main", Checks: []domain.Check{{Name: "check", Command: "git", TimeoutSeconds: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.CreateRequirement(ctx, a, "signoff-requirement", domain.Requirement{Title: "Feedback", Body: "Clear feedback"})
	if err != nil {
		t.Fatal(err)
	}
	var requirement domain.Requirement
	if err := json.Unmarshal(raw, &requirement); err != nil {
		t.Fatal(err)
	}
	catalog := app.TestCatalog{Service: s}
	if _, err := catalog.SaveAcceptance(ctx, a, "signoff-plan", domain.AcceptanceSave{ExpectedRequirementVersion: requirement.Version,
		Plan: domain.AcceptancePlan{RequirementID: requirement.ID, Revision: 1, Criteria: []domain.AcceptanceCriterion{
			{ID: "clear", Title: "Feedback is clear", Method: "manual"}}}}); err != nil {
		t.Fatal(err)
	}
	commit := signoffHTTPCommit(t, s.Dir)
	coverage, err := catalog.Coverage(ctx, a, requirement.ID, commit)
	if err != nil {
		t.Fatal(err)
	}
	in := domain.AcceptanceSignoffRequest{RequirementID: requirement.ID, Commit: commit, ExpectedDigest: coverage.Digest,
		ManualCriteria: []string{"clear"}, Action: "accept", Note: "Checked feedback."}
	post := func(key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/tests/acceptance/signoff", strings.NewReader(string(data)))
		r.RemoteAddr, r.Host = "127.0.0.1:12345", "127.0.0.1:8080"
		r.Header.Set("Authorization", "Bearer "+f.owner)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		f.server.AdminHandler().ServeHTTP(w, r)
		return w
	}
	first, replay := post("http-signoff", in), post("http-signoff", in)
	if first.Code != 200 || first.Body.String() != replay.Body.String() {
		t.Fatal("HTTP signoff replay failed", first.Code, first.Body.String(), replay.Body.String())
	}
	in.Action, in.Note, in.ExpectedVersion = "revoke", "Needs another check.", 1
	in.ExpectedDigest, in.ManualCriteria = "", nil
	if w := post("http-revoke", in); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"revoked"`) {
		t.Fatal("HTTP revocation failed", w.Code, w.Body.String())
	}
	if w := post("unknown-field", map[string]any{"authority": "owner"}); w.Code != 400 {
		t.Fatal("unknown request fields accepted", w.Code)
	}
}

func signoffHTTPCommit(t *testing.T, dir string) string {
	t.Helper()
	g := gitops.New("git")
	repo := filepath.Join(dir, "repository")
	if err := g.Init(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("feedback"), 0600); err != nil {
		t.Fatal(err)
	}
	commit, err := g.CommitAll(context.Background(), repo, "fixed feedback", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	return string(commit)
}
