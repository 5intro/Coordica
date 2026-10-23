package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestCatalogUppercaseVerificationPinsCanonicalCommit(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	var suite domain.TestSuite
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) }); err != nil {
		t.Fatal(err)
	}
	request := domain.VerificationRequest{SuiteID: suite.ID, ExpectedVersion: suite.Version, Commit: strings.ToUpper(f.b.String()),
		BudgetSeconds: 300, MaxRunSeconds: 60, MaxRetries: 0}
	raw, err := f.s.StartVerification(context.Background(), f.owner, "uppercase-sha", request)
	task := itDecode[domain.Task](t, raw, err)
	if task.Verification.Commit != f.b.String() {
		t.Fatal("verification stored a noncanonical fixed commit", task.Verification.Commit)
	}
	replay, err := f.s.StartVerification(context.Background(), f.owner, "uppercase-sha", request)
	if err != nil || string(raw) != string(replay) {
		t.Fatal("normalization changed original request replay", err)
	}
	resumeITProject(t, f)
	if err := (&VerificationWorker{Integration: f.w}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready, err := f.s.TestReadiness(context.Background(), f.owner, f.b.String())
	if err != nil || !ready.Ready {
		t.Fatal("real uppercase verification did not complete", ready, err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if coverage.Criteria[0].Cases[0].State != "unreported" || coverage.Criteria[0].Cases[0].RunID == "" {
		t.Fatal("accepted fixed evidence was not found", coverage)
	}
}

func TestCatalogFailureOnlyBelongsToTheExecutedCheck(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"missing", "unrun"}, {"failed", "failed"}, {"passed-before-other-failure", "incomplete_suite"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newITFixture(t)
			c, plan, v := catalogDefinition(t, f)
			seedCatalogEvidence(t, f, v)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				var r domain.Run
				if err := tx.Get("run", "accepted_test", &r); err != nil {
					return err
				}
				r.State = "failed"
				if tc.mode == "missing" {
					r.TestReport.Checks = []domain.CheckResult{{Name: "earlier-check", ExitCode: 1}}
				} else if tc.mode == "failed" {
					r.TestReport.Checks[0].Passed, r.TestReport.Checks[0].ExitCode = false, 1
					r.TestReport.Checks[0].Output = strings.ReplaceAll(r.TestReport.Checks[0].Output, `"Action":"pass"`, `"Action":"fail"`)
				}
				return tx.Put("run", r.ID, r)
			}); err != nil {
				t.Fatal(err)
			}
			coverage := catalogCoverage(t, c, f, plan)
			item := coverage.Criteria[0].Cases[0]
			if coverage.Verified || item.State != tc.want {
				t.Fatal("case borrowed an unrelated suite failure", item, coverage.Verified)
			}
			gap := coverage.Gaps[0]
			if (gap.Action == "repair_test_or_code") != (tc.mode == "failed") || (gap.RunID != "") != (tc.mode == "failed") {
				t.Fatal("unexecuted case acquired repair identity", gap)
			}
		})
	}
}

func TestCatalogUppercaseSupplementSharesCanonicalIdentity(t *testing.T) {
	f := newITFixture(t)
	c, plan, _ := catalogDefinition(t, f)
	coverage := catalogCoverage(t, c, f, plan)
	in := domain.TestSupplementRequest{RequirementID: plan.RequirementID, Commit: strings.ToUpper(f.b.String()), ExpectedDigest: coverage.Digest}
	raw, err := c.Supplement(context.Background(), f.owner, "upper-supplement", in)
	first := itDecode[domain.TestSupplementPlan](t, raw, err)
	replay, err := c.Supplement(context.Background(), f.owner, "upper-supplement", in)
	if err != nil || string(replay) != string(raw) {
		t.Fatal("original uppercase request did not replay", err)
	}
	in.Commit = f.b.String()
	raw, err = c.Supplement(context.Background(), f.owner, "lower-supplement", in)
	second := itDecode[domain.TestSupplementPlan](t, raw, err)
	if first.Commit != f.b.String() || first.RequirementID != second.RequirementID {
		t.Fatal("equivalent fixed commits created different supplemental work", first, second)
	}
}
