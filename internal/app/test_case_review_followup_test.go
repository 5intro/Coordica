package app

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestCatalogSharedCommandFailureRequiresTargetFailure(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"passed", "incomplete_suite"}, {"missing", "unrun"}, {"failed", "failed"},
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
				check := &r.TestReport.Checks[0]
				check.Passed, check.ExitCode = false, 1
				if tc.mode == "missing" {
					check.Output = strings.ReplaceAll(check.Output, "TestPay", "TestOther")
				} else if tc.mode == "failed" {
					check.Output = strings.ReplaceAll(check.Output, `"Action":"pass","Package":"p","Test":"TestPay"`,
						`"Action":"fail","Package":"p","Test":"TestPay"`)
				}
				check.Output = strings.ReplaceAll(check.Output, `"Action":"pass","Package":"p"}`,
					`"Action":"fail","Package":"p"}`)
				return tx.Put("run", r.ID, r)
			}); err != nil {
				t.Fatal(err)
			}
			coverage := catalogCoverage(t, c, f, plan)
			if coverage.Verified || coverage.Criteria[0].Cases[0].State != tc.want {
				t.Fatal("shared check failure was attributed to an innocent target", coverage)
			}
			gap := coverage.Gaps[0]
			if (gap.RunID != "") != (tc.mode == "failed") || (gap.Action == "repair_test_or_code") != (tc.mode == "failed") {
				t.Fatal("only target failure may supply a repair Run", gap)
			}
		})
	}
}

func TestCatalogDuplicateSinglePlatformIsVerifiable(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	var suite domain.TestSuite
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) }); err != nil {
		t.Fatal(err)
	}
	suite.Platforms = []string{runtime.GOOS, runtime.GOOS}
	if _, err := f.s.SaveTestSuite(context.Background(), f.owner, "duplicate-platform",
		domain.TestSuiteSave{ExpectedVersion: suite.Version, Suite: suite}); err != nil {
		t.Fatal(err)
	}
	seedCatalogEvidence(t, f, v)
	if coverage := catalogCoverage(t, c, f, plan); !coverage.Verified {
		t.Fatal("duplicate platform introduced an impossible coverage gap", coverage)
	}
}

func TestCatalogRemovedCheckRequiresBindingReview(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	var suite domain.TestSuite
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("test_suite", v.SuiteID, &suite) }); err != nil {
		t.Fatal(err)
	}
	suite.Checks[0].Name = "renamed"
	if _, err := f.s.SaveTestSuite(context.Background(), f.owner, "rename-check",
		domain.TestSuiteSave{ExpectedVersion: suite.Version, Suite: suite}); err != nil {
		t.Fatal(err)
	}
	seedCatalogEvidence(t, f, v)
	coverage := catalogCoverage(t, c, f, plan)
	if coverage.Verified || coverage.Criteria[0].Cases[0].State != "stale" || coverage.Gaps[0].Action != "review_test_binding" {
		t.Fatal("removed check was reported as runnable", coverage)
	}
}
