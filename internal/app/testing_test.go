package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func testSuite(t *testing.T, s *Service, owner Actor, stage string) domain.TestSuite {
	t.Helper()
	b, e := s.SaveTestSuite(context.Background(), owner, ID("suite_"), domain.TestSuiteSave{Suite: domain.TestSuite{ID: "regression", Name: "Regression", Stage: stage, Enabled: true, Checks: []domain.Check{{Name: "clean", Command: "git", Args: []string{"status", "--porcelain"}, TimeoutSeconds: 10}}}})
	return itDecode[domain.TestSuite](t, b, e)
}

func requestVerification(t *testing.T, f *itFixture, suite domain.TestSuite) domain.Task {
	t.Helper()
	b, e := f.s.StartVerification(context.Background(), f.owner, ID("test_"), domain.VerificationRequest{SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: f.b.String(), BudgetSeconds: 300, MaxRunSeconds: 60, MaxRetries: 1})
	return itDecode[domain.Task](t, b, e)
}

func resumeITProject(t *testing.T, f *itFixture) {
	t.Helper()
	p, e := f.s.Project(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if p.Paused {
		if _, e = f.s.ProjectAction(context.Background(), f.owner, ID("resume_"), "resume", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
			t.Fatal(e)
		}
	}
}

func TestSuiteOwnerVersionReplayAndMergePolicyFences(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	p, _ := f.s.Project(ctx)
	if _, e := f.s.ProjectAction(ctx, f.owner, "initial-pause", "pause", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
		t.Fatal(e)
	}
	in := domain.TestSuiteSave{Suite: domain.TestSuite{ID: "smoke", Name: "Smoke", Stage: "merge", Enabled: true, Checks: []domain.Check{{Name: "check", Command: "git", TimeoutSeconds: 10}}}}
	if _, e := f.s.SaveTestSuite(ctx, f.node, "forbidden", in); e == nil {
		t.Fatal("node changed test policy")
	}
	b, e := f.s.SaveTestSuite(ctx, f.owner, "suite-save", in)
	v := decodeTest[domain.TestSuite](t, b, e)
	replay, e := f.s.SaveTestSuite(ctx, f.owner, "suite-save", in)
	if e != nil || string(replay) != string(b) {
		t.Fatal("suite replay changed", e)
	}
	if _, e = f.s.SaveTestSuite(ctx, f.owner, "stale", in); e == nil {
		t.Fatal("stale suite overwrote version")
	}
	p, _ = f.s.Project(ctx)
	if p.TestPolicyVersion != 1 {
		t.Fatal("merge policy not versioned", p)
	}
	if _, e = f.s.ProjectAction(ctx, f.owner, "unpause", "resume", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = v.Version
	if _, e = f.s.SaveTestSuite(ctx, f.owner, "live-policy", in); e == nil {
		t.Fatal("live merge policy changed")
	}
	a := f.claim(t, "working")
	p, _ = f.s.Project(ctx)
	if _, e = f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveTestSuite(ctx, f.owner, "unknown-tree-policy", in); e == nil {
		t.Fatal("unknown execution ignored by global quiescence barrier")
	}
	if a == nil {
		t.Fatal("missing fixture execution")
	}
}

func TestMergeCheckPlanConservativeSelection(t *testing.T) {
	p := domain.Project{TestPolicyVersion: 4, Checks: []domain.Check{{Name: "baseline", Command: "git", TimeoutSeconds: 1}}}
	suites := []domain.TestSuite{
		{ID: "ui", Stage: "merge", Enabled: true, MatchPaths: []string{"web/**"}, Checks: []domain.Check{{Name: "ui-test", Command: "node", TimeoutSeconds: 10}}},
		{ID: "api", Stage: "merge", Enabled: true, MatchPaths: []string{"internal/**"}, Checks: []domain.Check{{Name: "api-test", Command: "go", TimeoutSeconds: 10}}},
		{ID: "long", Stage: "release", Enabled: true, Checks: []domain.Check{{Name: "soak", Command: "go", TimeoutSeconds: 36000}}},
	}
	for _, tc := range []struct {
		name  string
		paths []string
		risk  bool
		count int
	}{
		{"known UI", []string{"web/src/App.tsx"}, false, 2},
		{"both", []string{"web/app.tsx", "internal/api.go"}, false, 3},
		{"unknown dependency", []string{"go.mod"}, false, 3},
		{"unknown scope", nil, false, 3},
		{"protected", []string{"web/app.tsx"}, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, e := mergeCheckPlan(p, append([]domain.TestSuite{}, suites...), tc.paths, tc.risk)
			if e != nil || len(plan.Checks) != tc.count || plan.Checks[0].Name != "baseline" || plan.Digest == "" || plan.PolicyVersion != 4 {
				t.Fatal(plan, e)
			}
			for _, check := range plan.Checks {
				if strings.Contains(check.Name, "long") {
					t.Fatal("release suite ran on every merge")
				}
			}
		})
	}
}

func TestVerificationRealCommandsReadinessAndSuiteVersionInvalidation(t *testing.T) {
	f := newITFixture(t)
	p, _ := f.s.Project(context.Background())
	if _, e := f.s.ProjectAction(context.Background(), f.owner, "initial-pause", "pause", domain.VersionRequest{ExpectedVersion: p.Version}); e != nil {
		t.Fatal(e)
	}
	suite := testSuite(t, f.s, f.owner, "release")
	request := requestVerification(t, f, suite)
	w := &VerificationWorker{Integration: f.w}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).Runs) != 0 {
		t.Fatal("paused verification started")
	}
	resumeITProject(t, f)
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	var task domain.Task
	for _, v := range snap.Tasks {
		if v.ID == request.ID {
			task = v
		}
	}
	if task.State != "verified" || task.CurrentRunID != "" || snap.Project.ReservedSeconds != 0 || len(snap.Candidates) != 0 || len(snap.Runs) != 1 {
		t.Fatal("verification did not finish without integration", task, snap.Runs)
	}
	r := snap.Runs[0]
	if r.Kind != "verification" || !r.Quiescent || r.TestReport == nil || r.TestReport.Commit != f.b.String() || !domain.ChecksPassed(suite.Checks, r.TestReport.Checks) {
		t.Fatal(r)
	}
	readiness, e := f.s.TestReadiness(context.Background(), f.owner, f.b.String())
	if e != nil || !readiness.Ready {
		t.Fatal(readiness, e)
	}
	canonical, e := f.s.TestReadiness(context.Background(), f.owner, strings.ToUpper(f.b.String()))
	if e != nil || !canonical.Ready || canonical.Commit != f.b.String() {
		t.Fatal("equivalent full object IDs did not share fixed evidence", canonical, e)
	}
	other, e := f.s.TestReadiness(context.Background(), f.owner, strings.Repeat("b", 40))
	if e != nil || other.Ready || len(other.Missing) != 1 {
		t.Fatal("result reused for another commit", other, e)
	}
	suite.Description = "Changed suite specification"
	if _, e = f.s.SaveTestSuite(context.Background(), f.owner, "new-suite-version", domain.TestSuiteSave{ExpectedVersion: suite.Version, Suite: suite}); e != nil {
		t.Fatal(e)
	}
	readiness, e = f.s.TestReadiness(context.Background(), f.owner, f.b.String())
	if e != nil || readiness.Ready || len(readiness.Missing) != 1 {
		t.Fatal("old suite evidence reused", readiness, e)
	}
	if _, e = f.s.TaskAction(context.Background(), f.owner, "cannot-resume-verified", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); e == nil {
		t.Fatal("verified task resumed")
	}
}

func TestVerificationClaimAtomicRecoveryAndLateResult(t *testing.T) {
	f := newITFixture(t)
	suite := testSuite(t, f.s, f.owner, "project")
	request := requestVerification(t, f, suite)
	resumeITProject(t, f)
	w := &VerificationWorker{Integration: f.w}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var claims []*domain.Run
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, r, e := w.claim(context.Background())
			if e != nil {
				t.Error(e)
			}
			if r != nil {
				mu.Lock()
				claims = append(claims, r)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatal("non-atomic verification claim", len(claims))
	}
	old := *claims[0]
	if e := f.s.RecoverCenter(context.Background()); e != nil {
		t.Fatal(e)
	}
	lost := recoveryRun(t, f.snapshot(t), old.ID)
	if lost.State != "lost" || lost.Quiescent {
		t.Fatal("restart inferred stop proof", lost)
	}
	if _, r, e := w.claim(context.Background()); e != nil || r != nil {
		t.Fatal("new test started while old tree unknown", r, e)
	}
	if _, e := f.s.ConfirmStopped(context.Background(), f.owner, "fixture-stop", lost.ID, domain.StopConfirmation{ExpectedVersion: lost.Version, Evidence: "Test fixture claim spawned no process"}); e != nil {
		t.Fatal(e)
	}
	_, current, e := w.claim(context.Background())
	if e != nil || current == nil || current.Generation != old.Generation+1 {
		t.Fatal(current, e)
	}
	passed := []domain.CheckResult{{Name: "clean", Passed: true, ExitCode: 0}}
	if e = w.finish(context.Background(), old, passed, nil, true, false); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	for _, task := range snap.Tasks {
		if task.ID == request.ID && (task.CurrentRunID != current.ID || task.State != "executing" || task.Retries != 1) {
			t.Fatal("old test result affected replacement", task)
		}
	}
	if e = w.finish(context.Background(), *current, passed, nil, true, false); e != nil {
		t.Fatal(e)
	}
	if e = w.finish(context.Background(), *current, passed, nil, true, false); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).Project.ReservedSeconds != 0 {
		t.Fatal("duplicate completion changed accounting")
	}
}

func TestVerificationBudgetAndRetryStopAtLimits(t *testing.T) {
	f := newITFixture(t)
	suite := testSuite(t, f.s, f.owner, "project")
	request := requestVerification(t, f, suite)
	resumeITProject(t, f)
	w := &VerificationWorker{Integration: f.w}
	for attempt := 0; attempt < 2; attempt++ {
		_, r, e := w.claim(context.Background())
		if e != nil || r == nil {
			t.Fatal(r, e)
		}
		if e = f.s.RecoverCenter(context.Background()); e != nil {
			t.Fatal(e)
		}
		lost := recoveryRun(t, f.snapshot(t), r.ID)
		if _, e = f.s.ConfirmStopped(context.Background(), f.owner, ID("stop_"), lost.ID, domain.StopConfirmation{ExpectedVersion: lost.Version, Evidence: "fixture did not start a process"}); e != nil {
			t.Fatal(e)
		}
	}
	if _, r, e := w.claim(context.Background()); e != nil || r != nil {
		t.Fatal("retry limit bypassed", r, e)
	}
	for _, task := range f.snapshot(t).Tasks {
		if task.ID == request.ID && (task.HoldReason != "human_decision" || task.Retries != 1 || task.UsedSeconds != 120) {
			t.Fatal(task)
		}
	}
	if _, e := f.s.StartVerification(context.Background(), f.owner, "unbounded", domain.VerificationRequest{SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: f.b.String()}); e == nil {
		t.Fatal("unbounded verification admitted")
	}
}

func TestVerificationCancellationKillsProcessAndKeepsEvidence(t *testing.T) {
	f := newITFixture(t)
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(t.TempDir(), "started")
	b, e := f.s.SaveTestSuite(context.Background(), f.owner, "long-test", domain.TestSuiteSave{Suite: domain.TestSuite{ID: "longevity", Name: "Long running", Stage: "project", Enabled: true, Checks: []domain.Check{{Name: "wait", Command: binary, Args: []string{"-test.run=^TestVerificationSubprocess$", "--", "coordica-longevity", marker}, TimeoutSeconds: 60}}}})
	suite := itDecode[domain.TestSuite](t, b, e)
	request := requestVerification(t, f, suite)
	resumeITProject(t, f)
	w := &VerificationWorker{Integration: f.w}
	done := make(chan error, 1)
	go func() { done <- w.Tick(context.Background()) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, e = os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real subprocess did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := f.snapshot(t)
	for _, task := range snap.Tasks {
		if task.ID == request.ID {
			if _, e = f.s.TaskAction(context.Background(), f.owner, "cancel-test", task.ID, "cancel", domain.VersionRequest{ExpectedVersion: task.Version}); e != nil {
				t.Fatal(e)
			}
		}
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("verification did not stop")
	}
	snap = f.snapshot(t)
	if len(snap.Runs) != 1 || snap.Runs[0].State != "stopped" || !snap.Runs[0].Quiescent || snap.Project.ReservedSeconds != 0 || snap.Runs[0].TestReport == nil {
		t.Fatal(snap.Runs)
	}
	if ready, e := f.s.TestReadiness(context.Background(), f.owner, f.b.String()); e != nil || ready.Ready || len(ready.Failed) != 0 {
		t.Fatal("cancelled test was shown as passed/failed", ready, e)
	}
}

func TestVerificationSubprocess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "coordica-generate-test-output" {
			if e := os.MkdirAll("cache", 0700); e != nil {
				os.Exit(2)
			}
			if e := os.WriteFile(filepath.Join("cache", "generated.txt"), []byte("test evidence"), 0600); e != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
		if arg == "coordica-longevity" && i+1 < len(os.Args) {
			if e := os.WriteFile(os.Args[i+1], []byte("started"), 0600); e != nil {
				os.Exit(2)
			}
			time.Sleep(time.Minute)
			os.Exit(0)
		}
	}
}

func TestVerificationRetainsOnlyDeclaredGeneratedOutputs(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "undeclared", true: "declared"}[allow], func(t *testing.T) {
			f := newITFixture(t)
			binary, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			suite := domain.TestSuite{ID: "outputs", Name: "Generated output", Stage: "project", Enabled: true, Checks: []domain.Check{{Name: "generate", Command: binary, Args: []string{"-test.run=^TestVerificationSubprocess$", "--", "coordica-generate-test-output"}, TimeoutSeconds: 10}}}
			if allow {
				suite.OutputPaths = []string{"cache/**"}
			}
			b, e := f.s.SaveTestSuite(context.Background(), f.owner, "outputs", domain.TestSuiteSave{Suite: suite})
			suite = itDecode[domain.TestSuite](t, b, e)
			request := requestVerification(t, f, suite)
			w := &VerificationWorker{Integration: f.w}
			if e = w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			for _, task := range f.snapshot(t).Tasks {
				if task.ID != request.ID {
					continue
				}
				if (task.State == "verified") != allow {
					t.Fatal("output policy not enforced", task)
				}
			}
		})
	}
}

func TestRequirementsLinkIsAtomicSymmetricAndVersioned(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	b, e := f.s.CreateRequirement(ctx, f.owner, "left", domain.Requirement{Title: "Left", Body: "Original left"})
	left := decodeTest[domain.Requirement](t, b, e)
	b, e = f.s.CreateRequirement(ctx, f.owner, "right", domain.Requirement{Title: "Right", Body: "Original right"})
	right := decodeTest[domain.Requirement](t, b, e)
	in := domain.RequirementLinkRequest{ExpectedVersion: 1, OtherExpectedVersion: 2, OtherID: right.ID}
	if _, e = f.s.LinkRequirements(ctx, f.owner, "bad-link", left.ID, in); e == nil {
		t.Fatal("stale link accepted")
	}
	in.OtherExpectedVersion = 1
	b, e = f.s.LinkRequirements(ctx, f.owner, "link", left.ID, in)
	linked := decodeTest[[]domain.Requirement](t, b, e)
	if linked[0].Version != 2 || linked[1].Version != 2 || linked[0].RelatedIDs[0] != right.ID || linked[1].RelatedIDs[0] != left.ID || linked[0].Body != left.Body {
		t.Fatal(linked)
	}
	replay, e := f.s.LinkRequirements(ctx, f.owner, "link", left.ID, in)
	if e != nil || string(replay) != string(b) {
		t.Fatal("link replay changed", e)
	}
	if _, e = f.s.LinkRequirements(ctx, f.node, "node-link", left.ID, in); e == nil {
		t.Fatal("node wrote requirement links")
	}
}

func TestPremergeChecksBeforeReviewAndNoDuplicateAfterReview(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	c := f.candidate(t)
	if c.State != "reviewing" || c.CheckRunID == "" || c.CheckPlan == nil || !domain.ChecksPassed(c.CheckPlan.Checks, c.Checks) {
		t.Fatal("unchecked candidate admitted to review", c)
	}
	before := len(f.snapshot(t).Runs)
	f.reviewAndValidate(t)
	if len(f.snapshot(t).Runs) != before+1 {
		t.Fatal("review repeated candidate checks")
	}
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.candidate(t).State != "landed" || len(f.snapshot(t).Runs) != before+1 {
		t.Fatal("candidate was not landed directly after review")
	}
}

func TestFailedPrecheckCannotEnterReview(t *testing.T) {
	f := newITFixture(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.Checks = []domain.Check{{Name: "failure", Command: "coordica-nonexistent-test-command", TimeoutSeconds: 1}}
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	seedCurrentPolicyBaseline(t, f.s)
	f.implement(t, "src/feature.txt")
	if c := f.candidate(t); c.State != "rejected" || c.CheckRunID != "" {
		t.Fatal("failed precheck reached review", c)
	}
}

func TestVerificationSnapshotSurvivesBackupClosure(t *testing.T) {
	f := newITFixture(t)
	if _, e := f.w.Base(context.Background()); e != nil {
		t.Fatal(e)
	}
	suite := testSuite(t, f.s, f.owner, "project")
	requestVerification(t, f, suite)
	path, e := f.w.Backup(context.Background(), f.owner)
	if e != nil {
		t.Fatal(e)
	}
	records, e := readBackupRecords(context.Background(), filepath.Join(path, "coordica.db"), false)
	if e != nil {
		t.Fatal(e)
	}
	oids, e := requiredBackupOIDs(records)
	if e != nil {
		t.Fatal(e)
	}
	if len(oids) != 1 || oids[0] != f.b {
		raw, _ := json.Marshal(oids)
		t.Fatal("queued test version omitted from backup", string(raw))
	}
}

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
	b, e = f.s.CreateTask(context.Background(), f.owner, "repair", domain.Task{Title: "Fix regression", Description: "Repair failing scenario", Acceptance: "Regression passes", Tool: "codex", AllowedPaths: []string{"src/**"}, TestRunID: failed.ID, BudgetSeconds: 120, MaxRunSeconds: 60})
	repair := itDecode[domain.Task](t, b, e)
	if repair.TestRunID != failed.ID {
		t.Fatal("repair lost failure attribution")
	}
	passed := []domain.CheckResult{{Name: "clean", Passed: true, ExitCode: 0}}
	b, e = f.s.StartVerification(context.Background(), f.owner, "unrelated-green", domain.VerificationRequest{SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: strings.Repeat("b", 40), BudgetSeconds: 300, MaxRunSeconds: 60})
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
			{ID: "failed_windows", TaskID: "win", Kind: "verification", State: "failed", Base: main, Quiescent: true, TestReport: &domain.TestReport{SuiteID: suite.ID, Digest: suiteDigest(suite), Platform: "windows"}},
			{ID: "passed_mac", TaskID: "mac", Kind: "verification", State: "succeeded", Base: main, Quiescent: true, TestReport: &domain.TestReport{SuiteID: suite.ID, Digest: suiteDigest(suite), Platform: "darwin", Checks: []domain.CheckResult{{Name: "clean", Passed: true, ExitCode: 0}}}},
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
