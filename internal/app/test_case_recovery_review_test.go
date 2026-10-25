package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestCatalogSuccessfulRunCannotSupplyFailedRepairIdentity(t *testing.T) {
	f := newITFixture(t)
	c, plan, v := catalogDefinition(t, f)
	seedCatalogEvidence(t, f, v)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var run domain.Run
		if err := tx.Get("run", "accepted_test", &run); err != nil {
			return err
		}
		// A wrapper exited zero while complete Go JSON reported a target failure.
		run.TestReport.Checks[0].Output = strings.ReplaceAll(run.TestReport.Checks[0].Output, `"Action":"pass"`, `"Action":"fail"`)
		return tx.Put("run", run.ID, run)
	}); err != nil {
		t.Fatal(err)
	}
	coverage := catalogCoverage(t, c, f, plan)
	if coverage.Verified || len(coverage.Gaps) != 1 || coverage.Criteria[0].Cases[0].State != "failed" {
		t.Fatal("structured failure became a pass or lost the actual target", coverage)
	}
	gap := coverage.Gaps[0]
	if gap.RunID != "" || gap.Action != "repair_test_or_code" {
		t.Fatal("successful verification Run was prescribed as failed-Run repair evidence", gap)
	}
	raw, err := c.Supplement(ctx, f.owner, "wrapper-gap", domain.TestSupplementRequest{
		RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest,
	})
	supplement := itDecode[domain.TestSupplementPlan](t, raw, err)
	detail, err := f.s.RequirementDetail(ctx, f.owner, supplement.RequirementID, 0)
	if err != nil || strings.Contains(detail.Requirement.Body, "accepted_test") {
		t.Fatal("supplement invented a failed Run or lost its source", detail, err)
	}
	raw, err = f.s.CreateTask(ctx, f.owner, "wrapper-repair", domain.Task{
		Title: "Repair structured failure", Description: "Fix the reported payment assertion", Acceptance: "Pay once",
		Tool: "codex", AllowedPaths: []string{"src/**"}, RequirementIDs: []string{supplement.RequirementID},
		TestRunID: gap.RunID, BudgetSeconds: 120, MaxRunSeconds: 60,
	})
	task := itDecode[domain.Task](t, raw, err)
	if task.TestRunID != "" || !f.snapshot(t).Project.Paused {
		t.Fatal("ordinary repair changed attribution or execution authority", task)
	}
}

func advanceCatalogPlan(t *testing.T, f *itFixture, c TestCatalog, plan domain.AcceptancePlan, mode string) domain.AcceptancePlan {
	t.Helper()
	ctx := context.Background()
	r := catalogCoverage(t, c, f, plan).Requirement
	if mode == "requirement" {
		raw, err := f.s.EditRequirement(ctx, f.owner, "revise-catalog-source", r.ID, "revise", domain.RequirementEdit{
			ExpectedVersion: r.Version, Title: r.Title, Body: r.Body + " including delayed retries", Reason: "Clarify acceptance",
		})
		change := itDecode[domain.RequirementChange](t, raw, err)
		detail, err := f.s.RequirementDetail(ctx, f.owner, change.RequirementID, 0)
		if err != nil {
			t.Fatal(err)
		}
		r = detail.Requirement
		plan.Revision = domain.RequirementRevisionNumber(r)
	}
	if mode == "different_criterion" {
		plan.Criteria = []domain.AcceptanceCriterion{{ID: "refund", Title: "Refund once", Method: "automated"}}
	}
	raw, err := c.SaveAcceptance(ctx, f.owner, "next-catalog-plan", domain.AcceptanceSave{
		ExpectedRequirementVersion: r.Version, ExpectedVersion: plan.Version, Plan: plan,
	})
	return itDecode[domain.AcceptancePlan](t, raw, err)
}

func TestCatalogOlderCaseBindingsRequireReviewInsteadOfDuplicateTests(t *testing.T) {
	for _, mode := range []string{"plan", "requirement", "different_criterion", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newITFixture(t)
			c, plan, v := catalogDefinition(t, f)
			seedCatalogEvidence(t, f, v)
			if mode == "disabled" {
				v.Enabled = false
				if _, err := c.Save(context.Background(), f.owner, "disable-old-case", domain.TestCaseSave{
					ExpectedVersion: v.Version, Case: v,
				}); err != nil {
					t.Fatal(err)
				}
			}
			plan = advanceCatalogPlan(t, f, c, plan, mode)
			coverage := catalogCoverage(t, c, f, plan)
			if !coverage.Current || coverage.Verified || len(coverage.Gaps) != 1 {
				t.Fatal("old evidence counted as current acceptance", coverage)
			}
			gap := coverage.Gaps[0]
			if mode == "different_criterion" || mode == "disabled" {
				if gap.Reason != "missing_case" || gap.Action != "write_test" || gap.CaseID != "" {
					t.Fatal("unrelated or disabled test became current coverage", gap)
				}
				return
			}
			if gap.Action != "review_test_binding" || gap.Reason != "stale" || gap.CaseID != v.ID || gap.RunID != "" {
				t.Fatal("existing test was lost instead of offered for binding review", gap)
			}
			if cases := coverage.Criteria[0].Cases; len(cases) != 1 || cases[0].Case.ID != v.ID || cases[0].State != "stale" {
				t.Fatal("coverage discarded the existing case", cases)
			}
			raw, err := c.Supplement(context.Background(), f.owner, "review-binding", domain.TestSupplementRequest{
				RequirementID: plan.RequirementID, Commit: f.b.String(), ExpectedDigest: coverage.Digest,
			})
			p := itDecode[domain.TestSupplementPlan](t, raw, err)
			detail, err := f.s.RequirementDetail(context.Background(), f.owner, p.RequirementID, 0)
			if err != nil || !strings.Contains(detail.Requirement.Body, v.Purpose) ||
				!strings.Contains(detail.Requirement.Body, v.ExternalID) || strings.Contains(detail.Requirement.Body, "write_test") {
				t.Fatal("supplement omitted reusable test details or asked for duplicate authoring", detail, err)
			}
		})
	}
}
