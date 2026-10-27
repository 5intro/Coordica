package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestReviewInputRetryHTTPIsStrictOwnerApplicationRoute(t *testing.T) {
	f := newBoundaryFixture(t)
	in := domain.ReviewInputRetryRequest{ExpectedVersion: 1, ExpectedCandidateID: "candidate_fixed", ExpectedCandidateVersion: 1,
		ExpectedCommit: strings.Repeat("b", 40), ReviewRunID: "review_old", Reason: "Retry fixed material delivery"}
	raw, _ := json.Marshal(in)
	path := "/api/v1/tasks/missing/retry-review-input"
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", path, f.node.Token, string(raw)); w.Code == http.StatusOK {
		t.Fatal("node obtained an Owner retry")
	}
	unknown := strings.TrimSuffix(string(raw), "}") + `,"approved":true}`
	if w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, unknown); w.Code != http.StatusBadRequest {
		t.Fatal("unknown approval field accepted", w.Code, w.Body.String())
	}
	// A valid body must reach the application lookup. The generic TaskAction
	// decoder would instead reject its candidate fields as unknown input.
	if w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, string(raw)); w.Code != http.StatusNotFound {
		t.Fatal("request did not reach the shared use case", w.Code, w.Body.String())
	}
}
