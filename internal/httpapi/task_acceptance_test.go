package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestTaskAcceptanceRevisionHTTPUsesOwnerAndStrictContract(t *testing.T) {
	f := newBoundaryFixture(t)
	s := f.server.Service
	ctx := context.Background()
	owner, err := s.Authenticate(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateProject(ctx, owner, "acceptance-project", domain.Project{Name: "Contract repair",
		RepositoryURL: s.Dir, TargetBranch: "main", Checks: []domain.Check{{Name: "check", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	seedHTTPBaseline(t, s)
	raw, err := s.CreateTask(ctx, owner, "module-task", domain.Task{Title: "Module", Description: "Implement module",
		Acceptance: "Original derived acceptance", AllowedPaths: []string{"src/**"}, Tool: "codex",
		BudgetSeconds: 600, MaxRunSeconds: 300, MaxRepairs: 2, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if err = json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	raw, err = s.TaskAction(ctx, owner, "approve-module", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.Update(ctx, func(tx *store.Tx) error {
		var project domain.Project
		if err := tx.Get("project", "project", &project); err != nil {
			return err
		}
		task.State, task.HoldReason, task.ApprovedVersion = "awaiting_integration", "human_decision", task.Version
		task.CurrentCandidateID, task.UsedSeconds = "candidate_fixed", 5
		at := time.Now()
		source := domain.Run{ID: "source_stopped", TaskID: task.ID, Kind: "implement", State: "succeeded", Quiescent: true,
			AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: project.PolicyVersion, Head: strings.Repeat("b", 40), FinishedAt: &at,
			BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest}
		candidate := domain.Candidate{ID: task.CurrentCandidateID, TaskID: task.ID, SourceRunID: source.ID,
			AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: project.PolicyVersion,
			BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, State: "rejected",
			Commit: strings.Repeat("c", 40), Main: strings.Repeat("a", 40), Head: source.Head,
			Review: &domain.Review{Candidate: strings.Repeat("c", 40), Verdict: "blocked"}}
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		if err := tx.Put("run", source.ID, source); err != nil {
			return err
		}
		return tx.Put("candidate", candidate.ID, candidate)
	}); err != nil {
		t.Fatal(err)
	}
	in := domain.TaskAcceptanceRequest{ExpectedVersion: task.Version, ExpectedCandidateID: task.CurrentCandidateID,
		Acceptance: "Module verification now; production acceptance remains mandatory in integration", Reason: "Owner repairs task boundary"}
	encoded, _ := json.Marshal(in)
	path := "/api/v1/tasks/" + task.ID + "/acceptance"
	if w := boundaryRequest(f.server.RunnerHandler(), "POST", path, f.node.Token, string(encoded)); w.Code == http.StatusOK {
		t.Fatal("node revised Owner task acceptance")
	}
	unknown := strings.TrimSuffix(string(encoded), "}") + `,"approved":true}`
	if w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, unknown); w.Code != http.StatusBadRequest {
		t.Fatal("unknown input field accepted", w.Code, w.Body.String())
	}
	w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, string(encoded))
	if w.Code != http.StatusOK {
		t.Fatal("acceptance revision failed", w.Code, w.Body.String())
	}
	var revised domain.Task
	if err = json.Unmarshal(w.Body.Bytes(), &revised); err != nil || revised.Acceptance != in.Acceptance ||
		revised.UsedSeconds != 5 || revised.AuthorizationVersion != task.AuthorizationVersion+1 || revised.HoldReason != "paused" {
		t.Fatal("HTTP entry changed the revision semantics", revised, err)
	}
}
