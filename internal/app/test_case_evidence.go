package app

import (
	"errors"
	"slices"

	"coordica/internal/domain"
	"coordica/internal/store"
	"coordica/internal/testreport"
)

func criterionCoverage(tx *store.Tx, plan domain.AcceptancePlan, criterion domain.AcceptanceCriterion,
	cases []domain.TestCase, commit string, current bool,
	cache map[string]domain.CaseEvidence,
) (domain.CriterionCoverage, error) {
	out := domain.CriterionCoverage{Criterion: criterion, State: "missing_case", Cases: []domain.CaseEvidence{}}
	ref := domain.CriterionRef{RequirementID: plan.RequirementID, Revision: plan.Revision,
		PlanVersion: plan.Version, CriterionID: criterion.ID}
	for _, v := range cases {
		if !v.Enabled || !slices.Contains(v.Coverage, ref) {
			continue
		}
		evidence, found := cache[v.ID]
		if !found {
			var err error
			evidence, err = caseEvidence(tx, v, commit)
			if err != nil {
				return out, err
			}
			cache[v.ID] = evidence
		}
		out.Cases = append(out.Cases, evidence)
	}
	if len(out.Cases) > 0 {
		out.State = "passed"
		for _, item := range out.Cases {
			if item.State != "passed" {
				out.State = "test_gap"
			}
		}
	}
	if criterion.Method == "manual" {
		out.State = "manual_required"
	}
	if !current {
		out.State = "stale"
	}
	return out, nil
}

func caseEvidence(tx *store.Tx, v domain.TestCase, commit string) (domain.CaseEvidence, error) {
	out := domain.CaseEvidence{Case: domain.TestCaseSummary{ID: v.ID, Version: v.Version, Name: v.Name}, State: "unrun"}
	if !v.Implemented {
		out.State = "unimplemented"
		return out, nil
	}
	var suite domain.TestSuite
	if err := tx.Get("test_suite", v.SuiteID, &suite); err != nil {
		return out, err
	}
	if !suite.Enabled {
		out.State = "suite_disabled"
		return out, nil
	}
	if !slices.ContainsFunc(suite.Checks, func(check domain.Check) bool { return check.Name == v.CheckName }) {
		out.State = "stale"
		return out, nil
	}
	task, run, err := store.LatestVerification(tx, suite.ID, commit)
	if errors.Is(err, domain.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.RunID = run.ID
	if run.TestReport != nil {
		out.Platform = run.TestReport.Platform
	}
	var pin domain.TestCatalogPin
	if err := tx.Get("test_catalog_pin", task.ID, &pin); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	if pin.Versions[v.ID] != v.Version || task.Verification == nil || task.Verification.Digest != suiteDigest(suite) ||
		requirementTaskGate(tx, task) != nil || !reportMatchesSuite(run, suite, commit) {
		out.State = "stale"
		return out, nil
	}
	if task.Generation != run.Generation || task.AuthorizationVersion != run.AuthorizationVersion ||
		!run.Quiescent || (run.State != "succeeded" && run.State != "failed") {
		return out, nil
	}
	out.State = reportedCaseState(v, task, run, suite)
	return out, nil
}

func reportedCaseState(v domain.TestCase, task domain.Task, run domain.Run, suite domain.TestSuite) string {
	index := slices.IndexFunc(run.TestReport.Checks, func(c domain.CheckResult) bool { return c.Name == v.CheckName })
	if index < 0 {
		return "unrun"
	}
	check := run.TestReport.Checks[index]
	state := "unreported"
	if v.ReportFormat == "go-json" {
		state = testreport.GoCase(check.Output, v.ExternalID)
	}
	if run.State == "failed" {
		if state == "passed" {
			return "incomplete_suite"
		}
		return state
	}
	if task.State != "verified" || !domain.ChecksPassed(suite.Checks, run.TestReport.Checks) {
		return "unrun"
	}
	if len(slices.Compact(slices.Sorted(slices.Values(suite.Platforms)))) > 1 {
		return "platform_gap"
	}
	return state
}

func reportMatchesSuite(r domain.Run, suite domain.TestSuite, commit string) bool {
	v := r.TestReport
	return r.NodeID == "center" && r.Tool == "program" && r.Kind == "verification" && r.Base == commit && v != nil &&
		v.Commit == commit && v.SuiteID == suite.ID && v.SuiteVersion == suite.Version && v.Digest == suiteDigest(suite) &&
		domain.TestPlatformAllowed(suite, v.Platform) && slices.Contains([]string{"darwin", "linux", "windows"}, v.Platform)
}

func criterionGaps(item domain.CriterionCoverage) []domain.TestGap {
	if item.State == "passed" {
		return nil
	}
	if item.State != "test_gap" {
		action := "write_test"
		if item.State == "manual_required" {
			action = "owner_acceptance"
		}
		if item.State == "stale" {
			action = "define_acceptance"
		}
		return []domain.TestGap{{CriterionID: item.Criterion.ID, Reason: item.State, Action: action}}
	}
	gaps := []domain.TestGap{}
	for _, evidence := range item.Cases {
		if evidence.State == "passed" {
			continue
		}
		action := "run_test"
		if evidence.State == "unimplemented" || evidence.State == "unreported" {
			action = "write_test"
		} else if evidence.State == "failed" {
			action = "repair_test_or_code"
		} else if evidence.State == "stale" || evidence.State == "suite_disabled" {
			action = "review_test_binding"
		}
		gap := domain.TestGap{CriterionID: item.Criterion.ID, CaseID: evidence.Case.ID, Reason: evidence.State, Action: action}
		if action == "repair_test_or_code" {
			gap.RunID = evidence.RunID
		}
		gaps = append(gaps, gap)
	}
	return gaps
}
