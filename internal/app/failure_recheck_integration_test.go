package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
)

func admitRecoveryGoal(t *testing.T, f *itFixture) domain.Requirement {
	t.Helper()
	ctx := context.Background()
	for _, task := range f.snapshot(t).Tasks {
		if _, err := f.s.TaskAction(ctx, f.owner, ID("cancel_"), task.ID, "cancel", domain.VersionRequest{ExpectedVersion: task.Version}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := f.s.CreateRequirement(ctx, f.owner, ID("req_"), domain.Requirement{Title: "Feature", Body: "Deliver the feature with executable verification"})
	req := itDecode[domain.Requirement](t, raw, err)
	raw, err = f.s.SavePlanning(ctx, f.owner, ID("goal_"), domain.PlanningSaveRequest{Document: domain.PlanningDocument{
		Objective: "Feature", NonGoals: "No publication changes", Acceptance: "Feature present", RequirementIDs: []string{req.ID},
		Architecture: "Independent modules", DirectoryOutline: "src/: source", AllowedRoots: []string{"src"}, BudgetSeconds: 1000,
	}})
	goal := itDecode[domain.PlanningDocument](t, raw, err)
	raw, err = f.s.AuthorizePlanning(ctx, f.owner, ID("authorize_"), domain.VersionRequest{ExpectedVersion: goal.Version})
	goal = itDecode[domain.PlanningDocument](t, raw, err)
	settings := f.snapshot(t).Automation
	settings.Enabled = true
	if _, err := f.s.ConfigureAutomation(ctx, f.owner, ID("auto_"), domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); err != nil {
		t.Fatal(err)
	}
	sf := &serviceFixture{s: f.s, owner: f.owner, node: f.node}
	scheduleTest(t, sf, goal, ID("plan_"))
	result := domain.LeaderResult{Reply: "Admitted", Ready: true, Tasks: []domain.TaskProposal{}, Scopes: []domain.ScopeProposal{},
		MajorDecisions: []domain.MajorDecisionProposal{}, MissingItems: []string{}, Clarifications: []string{}}
	if err := (&PlanningWorker{Service: f.s, Executor: resultExecutor(result)}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestFailureRealVerificationRepairsReviewsLandsAndRechecksWithoutOriginalRetries(t *testing.T) {
	f := newITFixture(t)
	req := admitRecoveryGoal(t, f)
	ctx := context.Background()
	raw, err := f.s.SaveTestSuite(ctx, f.owner, ID("suite_"), domain.TestSuiteSave{Suite: domain.TestSuite{
		ID: "feature-contract", Name: "Feature contract", Enabled: true, Stage: "project", BlockMergesOnFailure: true, RequirementIDs: []string{req.ID},
		Checks: []domain.Check{{Name: "feature-present", Command: "git", Args: []string{"cat-file", "-e", "HEAD:src/feature.txt"}, TimeoutSeconds: 10}},
	}})
	suite := itDecode[domain.TestSuite](t, raw, err)
	raw, err = f.s.StartVerification(ctx, f.owner, ID("verify_"), domain.VerificationRequest{
		SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: f.b.String(), RequirementIDs: []string{req.ID}, BudgetSeconds: 120, MaxRunSeconds: 30,
	})
	source := itDecode[domain.Task](t, raw, err)
	verifier := &VerificationWorker{Integration: f.w}
	if err := verifier.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	cases := failureCases(t, f.s)
	if len(cases) != 1 || cases[0].SourceTask.ID != source.ID {
		t.Fatal("actual failed command did not enter diagnosis", cases)
	}
	plan := followupPlan(cases[0])
	plan.Tasks[0].Paths = []string{"src/feature.txt"}
	sf := &serviceFixture{s: f.s, owner: f.owner, node: f.node}
	got := runRecovery(t, sf, plan)
	if got.State != "waiting_followup" {
		t.Fatal("repair not derived", got)
	}
	// Deterministic developer/reviewer receipts exercise the real upload,
	// checks, fixed candidate and Git merge without starting any model session.
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	if err := (FailureRecoveries{Service: f.s}).queue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	landed := f.candidate(t)
	if landed.State != "landed" {
		t.Fatal("repair did not pass integration", landed)
	}
	if err := (FailureRecoveries{Service: f.s}).queue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	var after domain.Task
	for _, task := range snapshot.Tasks {
		if task.ID == source.ID {
			after = task
		}
	}
	if after.State != "verified" || after.Retries != 0 || after.MaxRetries != 0 || after.FailureRecheckID != got.ID || after.Verification.Commit != landed.Commit {
		t.Fatal("fresh executable acceptance did not close bounded loop", after)
	}
	oldFailed, newPassed := false, false
	for _, run := range snapshot.Runs {
		if run.TaskID != source.ID {
			continue
		}
		oldFailed = oldFailed || run.State == "failed" && run.Base == source.Verification.Commit && run.Quiescent
		newPassed = newPassed || run.State == "succeeded" && run.Base == landed.Commit && run.Quiescent
	}
	if !oldFailed || !newPassed {
		t.Fatal("old failure was erased or a different version satisfied acceptance")
	}
}
