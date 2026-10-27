package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestGoalBudgetHTTPRequiresOwnerAndPreservesGoalIdentity(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	if err := f.server.Service.DB.Update(ctx, func(tx *store.Tx) error {
		return tx.Put("planning_document", "current", domain.PlanningDocument{Version: 3, AuthorizationVersion: 2,
			ApprovedVersion: 3, ApprovedAuthorizationVersion: 2, Status: "admitted", Objective: "Unchanged goal",
			BudgetSeconds: 30, UsedSeconds: 10, ReservedSeconds: 5})
	}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/planning/budget"
	body := `{"expected_version":0,"expected_goal_version":3,"expected_authorization_version":2,"budget_seconds":60,"reason":"Owner grants finite room"}`
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", path, f.node.Token, body); w.Code == http.StatusOK {
		t.Fatal("node increased Owner goal allowance")
	}
	if w := boundaryRequest(f.server.RunnerHandler(), "GET", path, f.node.Token, ""); w.Code == http.StatusOK {
		t.Fatal("node read Owner allowance controls")
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, body)
	if w.Code != http.StatusOK {
		t.Fatal("finite allowance update failed", w.Code, w.Body.String())
	}
	w = boundaryRequest(f.server.AdminHandler(), "GET", path, f.owner, "")
	var state domain.GoalBudgetState
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Version != 1 ||
		state.GoalVersion != 3 || state.GoalAuthorizationVersion != 2 || state.UsedSeconds != 10 || state.ReservedSeconds != 5 || state.BudgetSeconds != 60 {
		t.Fatal("HTTP did not use the central allowance rules", w.Code, w.Body.String())
	}
}
