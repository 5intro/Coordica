package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestFailureRecoveryDetailsStayOnOwnerLoopbackSurface(t *testing.T) {
	f := newBoundaryFixture(t)
	item := domain.FailureAssessment{ID: "failure_private", Version: 1, Detail: "private source contract"}
	if err := f.server.Service.DB.Update(context.Background(), func(tx *store.Tx) error { return tx.Put("failure_assessment", item.ID, item) }); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/failure-assessments/" + item.ID
	owner := boundaryRequest(f.server.AdminHandler(), "GET", path, f.owner, "")
	if owner.Code != http.StatusOK || !strings.Contains(owner.Body.String(), item.Detail) {
		t.Fatal("Owner query unavailable", owner.Code, owner.Body.String())
	}
	for _, token := range []string{"", f.owner, f.node.Token} {
		response := boundaryRequest(f.server.RunnerHandler(), "GET", path, token, "")
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), item.Detail) {
			t.Fatal("private case leaked to network", response.Code)
		}
	}
	if response := boundaryRequest(f.server.AdminHandler(), "GET", path, f.node.Token, ""); response.Code != http.StatusForbidden {
		t.Fatal("node accessed private case", response.Code)
	}
}
