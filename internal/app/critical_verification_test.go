package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestCriticalTestFailureBlocksMergeUntilMatchingVersionPasses(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	f.reviewAndValidate(t)
	suite := testSuite(t, f.s, f.owner, "project")
	suite.BlockMergesOnFailure = true
	b, e := f.s.SaveTestSuite(context.Background(), f.owner, "critical-suite", domain.TestSuiteSave{ExpectedVersion: suite.Version, Suite: suite})
	suite = itDecode[domain.TestSuite](t, b, e)
	w := &VerificationWorker{Integration: f.w}
	requestVerification(t, f, suite)
	_, failed, e := w.claim(context.Background())
	if e != nil || failed == nil {
		t.Fatal(failed, e)
	}
	if e = w.finish(context.Background(), *failed, []domain.CheckResult{{Name: "clean", ExitCode: 1}}, errors.New("regression"), true, false); e != nil {
		t.Fatal(e)
	}
	if e = f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.candidate(t).State != "ready_to_land" {
		t.Fatal("critical failure did not stop publication")
	}
	// A repair is ordinary scoped code work with explicit failure attribution.
	b, e = f.s.CreateTask(context.Background(), f.owner, "repair", domain.Task{
		Title: "Fix regression", Description: "Repair failing scenario", Acceptance: "Regression passes",
		Tool: "codex", AllowedPaths: []string{"src/**"}, TestRunID: failed.ID, BudgetSeconds: 120, MaxRunSeconds: 60,
	})
	repair := itDecode[domain.Task](t, b, e)
	if repair.TestRunID != failed.ID {
		t.Fatal("repair lost failure attribution")
	}
	passed := []domain.CheckResult{{Name: "clean", Passed: true, ExitCode: 0}}
	b, e = f.s.StartVerification(context.Background(), f.owner, "unrelated-green", domain.VerificationRequest{
		SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: strings.Repeat("b", 40),
		BudgetSeconds: 300, MaxRunSeconds: 60,
	})
	if e != nil {
		t.Fatal(e, string(b))
	}
	_, unrelated, e := w.claim(context.Background())
	if e != nil || unrelated == nil {
		t.Fatal(unrelated, e)
	}
	if e = w.finish(context.Background(), *unrelated, passed, nil, true, false); e != nil {
		t.Fatal(e)
	}
	if e = f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.candidate(t).State != "ready_to_land" {
		t.Fatal("unrelated green test cleared critical failure")
	}
	requestVerification(t, f, suite)
	_, matching, e := w.claim(context.Background())
	if e != nil || matching == nil {
		t.Fatal(matching, e)
	}
	if e = w.finish(context.Background(), *matching, passed, nil, true, false); e != nil {
		t.Fatal(e)
	}
	if e = f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.candidate(t).State != "landed" {
		t.Fatal("matching successful verification did not release merge")
	}
}

func TestCriticalFailuresKeepPlatformAndCandidateIdentity(t *testing.T) {
	f := newServiceFixture(t)
	suite := testSuite(t, f.s, f.owner, "project")
	suite.BlockMergesOnFailure = true
	b, e := f.s.SaveTestSuite(context.Background(), f.owner, "critical", domain.TestSuiteSave{ExpectedVersion: suite.Version, Suite: suite})
	suite = decodeTest[domain.TestSuite](t, b, e)
	main := strings.Repeat("a", 40)
	if e = f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		records := []domain.Run{
			{ID: "failed_windows", TaskID: "win", Kind: "verification", State: "failed", Base: main, Quiescent: true,
				TestReport: &domain.TestReport{SuiteID: suite.ID, Digest: suiteDigest(suite), Platform: "windows"}},
			{ID: "passed_mac", TaskID: "mac", Kind: "verification", State: "succeeded", Base: main, Quiescent: true,
				TestReport: &domain.TestReport{SuiteID: suite.ID, Digest: suiteDigest(suite), Platform: "darwin",
					Checks: []domain.CheckResult{{Name: "clean", Passed: true, ExitCode: 0}}}},
		}
		for _, r := range records {
			if e := tx.Put("run", r.ID, r); e != nil {
				return e
			}
		}
		if e := tx.Put("task", "mac", domain.Task{ID: "mac", State: "verified"}); e != nil {
			return e
		}
		blocked, e := blockingTestFailures(tx, main)
		if e != nil || len(blocked) != 1 || blocked[0].ID != "failed_windows" {
			t.Fatal("mac green erased Windows failure", blocked, e)
		}
		pass := records[1]
		pass.ID, pass.TaskID, pass.TestReport.Platform = "passed_windows", "win-fixed", "windows"
		if e := tx.Put("run", pass.ID, pass); e != nil {
			return e
		}
		if e := tx.Put("task", pass.TaskID, domain.Task{ID: pass.TaskID, State: "verified"}); e != nil {
			return e
		}
		blocked, e = blockingTestFailures(tx, main)
		if e != nil || len(blocked) != 0 {
			t.Fatal("matching platform remained blocked", blocked, e)
		}
		blocked, e = blockingTestFailures(tx, strings.Repeat("b", 40))
		if e != nil || len(blocked) != 1 {
			t.Fatal("another candidate used unrelated green result", blocked, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
