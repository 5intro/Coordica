package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func seedCatalogEvidence(t *testing.T, f *itFixture, v domain.TestCase) domain.Task {
	t.Helper()
	var suite domain.TestSuite
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		return tx.Get("test_suite", v.SuiteID, &suite)
	}); err != nil {
		t.Fatal(err)
	}
	task := requestVerification(t, f, suite)
	task.State, task.Generation, task.AuthorizationVersion = "verified", 1, 1
	output := `{"Action":"run","Package":"p","Test":"TestPay"}` + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestPay"}` + "\n" + `{"Action":"pass","Package":"p"}` + "\n"
	run := domain.Run{ID: "accepted_test", TaskID: task.ID, State: "succeeded", Generation: 1, AuthorizationVersion: 1,
		NodeID: "center", Tool: "program", Kind: "verification", Base: f.b.String(), Quiescent: true,
		TestReport: &domain.TestReport{Commit: f.b.String(), SuiteID: suite.ID, SuiteVersion: suite.Version,
			Digest: suiteDigest(suite), Platform: "darwin", Checks: []domain.CheckResult{{Name: v.CheckName, Passed: true, Output: output}}}}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		return tx.Put("run", run.ID, run)
	}); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestCatalogFixedEvidenceFences(t *testing.T) {
	for _, mode := range []string{"pass", "cancelled", "unquiet", "late_generation", "stale_grant", "foreign_node", "wrong_commit",
		"wrong_suite", "case_changed", "requirement_changed", "clause_changed", "unreported", "clipped", "manual", "new_attempt"} {
		t.Run(mode, func(t *testing.T) {
			f := newITFixture(t)
			c, plan, v := catalogDefinition(t, f)
			task := seedCatalogEvidence(t, f, v)
			if mode == "new_attempt" {
				var suite domain.TestSuite
				_ = f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) })
				requestVerification(t, f, suite)
			} else {
				mutateCatalogEvidence(t, f, task, plan, v, mode)
			}
			coverage := catalogCoverage(t, c, f, plan)
			if coverage.Verified != (mode == "pass") {
				t.Fatalf("%s: false evidence: %+v", mode, coverage)
			}
		})
	}
}

func mutateCatalogEvidence(t *testing.T, f *itFixture, task domain.Task, plan domain.AcceptancePlan, v domain.TestCase, mode string) {
	t.Helper()
	err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var r domain.Run
		if err := tx.Get("run", "accepted_test", &r); err != nil {
			return err
		}
		switch mode {
		case "cancelled":
			r.State = "stopped"
		case "unquiet":
			r.Quiescent = false
		case "late_generation":
			r.Generation++
		case "stale_grant":
			r.AuthorizationVersion++
		case "foreign_node":
			r.NodeID = "runner"
		case "wrong_commit":
			r.TestReport.Commit = strings.Repeat("b", 40)
		case "wrong_suite":
			r.TestReport.SuiteVersion++
		case "unreported":
			r.TestReport.Checks[0].Output = "all passed"
		case "clipped":
			r.TestReport.Checks[0].Output += strings.Repeat(" ", 256<<10)
		case "case_changed":
			v.Version++
			return tx.Put("test_case", v.ID, v)
		case "requirement_changed":
			var req domain.Requirement
			if err := tx.Get("requirement", plan.RequirementID, &req); err != nil {
				return err
			}
			req.ContentRevision++
			return tx.Put("requirement", req.ID, req)
		case "clause_changed":
			plan.Version++
			return tx.Put("acceptance_plan", plan.RequirementID, plan)
		case "manual":
			plan.Criteria[0].Method = "manual"
			return tx.Put("acceptance_plan", plan.RequirementID, plan)
		}
		return tx.Put("run", r.ID, r)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCatalogActualCommandSuccessDoesNotInventIndividualPass(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	var suite domain.TestSuite
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) }); err != nil {
		t.Fatal(err)
	}
	requestVerification(t, f, suite)
	w := &VerificationWorker{Integration: f.w}
	if err := w.Tick(context.Background()); err != nil || len(f.snapshot(t).Runs) != 0 {
		t.Fatal("paused execution started", err)
	}
	resumeITProject(t, f)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if coverage.Verified || coverage.Criteria[0].Cases[0].State != "unreported" {
		t.Fatalf("command-only report inflated case coverage: %+v", coverage)
	}
}

func TestCatalogFailedEvidenceSuppliesExistingRepairRun(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	seedCatalogEvidence(t, f, v)
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var r domain.Run
		if err := tx.Get("run", "accepted_test", &r); err != nil {
			return err
		}
		r.State = "failed"
		r.TestReport.Checks[0].Passed, r.TestReport.Checks[0].ExitCode = false, 1
		r.TestReport.Checks[0].Output = strings.ReplaceAll(r.TestReport.Checks[0].Output, `"Action":"pass"`, `"Action":"fail"`)
		return tx.Put("run", r.ID, r)
	}); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if len(coverage.Gaps) != 1 || coverage.Gaps[0].RunID != "accepted_test" || coverage.Verified {
		t.Fatal("failed verification lost its repair identity", coverage.Gaps)
	}
}
