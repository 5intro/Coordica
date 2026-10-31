package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func suiteDigest(s domain.TestSuite) string {
	b, _ := json.Marshal(s)
	return Hash(string(b))
}

func validateTestRunLink(tx *store.Tx, id string) error {
	if id == "" {
		return nil
	}
	var run domain.Run
	if !domain.ChatIdentifier.MatchString(id) || tx.Get("run", id, &run) != nil || run.Kind != "verification" || run.State != "failed" || run.TestReport == nil {
		return domain.Fail("invalid_input", "repair must reference a failed verification Run")
	}
	return nil
}

func (s *Service) SaveTestSuite(ctx context.Context, a Actor, key string, in domain.TestSuiteSave) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if e := domain.ValidateTestSuite(in.Suite); e != nil {
		return nil, e
	}
	if _, e := gitops.NewScope(in.Suite.MatchPaths, nil); e != nil {
		return nil, domain.Fail("invalid_input", e.Error())
	}
	if _, e := gitops.NewScope(in.Suite.OutputPaths, nil); e != nil {
		return nil, domain.Fail("invalid_input", e.Error())
	}
	return s.mutate(ctx, a, key, "tests.suite.save", in, func(tx *store.Tx) (any, error) {
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		var old domain.TestSuite
		if e = tx.Get("test_suite", in.Suite.ID, &old); e != nil && !errors.Is(e, domain.ErrNotFound) {
			return nil, e
		}
		if e = version(old.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if old.Stage == "merge" || in.Suite.Stage == "merge" {
			if !p.Paused {
				return nil, domain.Fail("conflict", "pause project before changing merge test policy")
			}
			if e = requireQuiescence(tx, ""); e != nil {
				return nil, e
			}
			p.TestPolicyVersion++
			p.Version++
			if e = tx.Put("project", p.ID, p); e != nil {
				return nil, e
			}
			candidates, e := store.List[domain.Candidate](tx, "candidate")
			if e != nil {
				return nil, e
			}
			for _, c := range candidates {
				if c.State == "landed" || c.State == "rejected" || c.State == "invalidated" {
					continue
				}
				if c.State == "landing" || c.State == "reconciling" {
					return nil, domain.Fail("busy", "publication must be reconciled before changing merge policy")
				}
				c.State, c.Commit, c.Main, c.ApprovedCommit = "accepted", "", "", ""
				c.Review, c.Checks, c.CheckPlan, c.CheckRunID = nil, nil, nil, ""
				c.Version++
				if e = tx.Put("candidate", c.ID, c); e != nil {
					return nil, e
				}
			}
		}
		for _, id := range in.Suite.RequirementIDs {
			var requirement domain.Requirement
			if e = tx.Get("requirement", id, &requirement); e != nil {
				return nil, domain.Fail("invalid_input", "unknown suite requirement")
			}
		}
		v := in.Suite
		v.Version = old.Version + 1
		if e = tx.Put("test_suite", v.ID, v); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "tests.suite.saved", v.ID, fmt.Sprintf("version=%d; stage=%s", v.Version, v.Stage))
	})
}

// StartVerification admits an explicitly requested finite program task. It
// uses the normal authorization, reservations, cancellation and recovery rules.
func (s *Service) StartVerification(ctx context.Context, a Actor, key string, in domain.VerificationRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if in.MaxRunSeconds < 1 || in.MaxRunSeconds > domain.MaxRunDurationSeconds {
		return nil, domain.Fail("invalid_input", "verification requires a finite program-operation timeout")
	}
	if _, e := gitops.ParseOID(in.Commit); e != nil {
		return nil, domain.Fail("invalid_input", "full fixed commit required")
	}
	return s.mutateWithCheck(ctx, a, key, "tests.start", in, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		var suite domain.TestSuite
		if e := tx.Get("test_suite", in.SuiteID, &suite); e != nil {
			return nil, e
		}
		if e := version(suite.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if !suite.Enabled || !domain.TestPlatformAllowed(suite, runtime.GOOS) {
			return nil, domain.Fail("conflict", "suite disabled or unavailable on this center platform")
		}
		t, e := verificationTask(tx, s, in, suite)
		if e != nil {
			return nil, e
		}
		if e = tx.Put("task", t.ID, t); e != nil {
			return nil, e
		}
		if e = store.PinTestCatalog(tx, t.ID, suite.ID); e != nil {
			return nil, e
		}
		return t, s.audit(tx, a, "tests.requested", t.ID, suite.ID+":"+in.Commit)
	})
}

func (s *Service) LinkRequirements(ctx context.Context, a Actor, key, id string, in domain.RequirementLinkRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if id == in.OtherID || !domain.ChatIdentifier.MatchString(id) || !domain.ChatIdentifier.MatchString(in.OtherID) {
		return nil, domain.Fail("invalid_input", "two distinct requirement IDs required")
	}
	return s.mutate(ctx, a, key, "requirements.link."+id, in, func(tx *store.Tx) (any, error) {
		var left, right domain.Requirement
		if e := tx.Get("requirement", id, &left); e != nil {
			return nil, e
		}
		if e := tx.Get("requirement", in.OtherID, &right); e != nil {
			return nil, e
		}
		if e := version(left.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if e := version(right.Version, in.OtherExpectedVersion); e != nil {
			return nil, e
		}
		for _, pair := range [][2]*domain.Requirement{{&left, &right}, {&right, &left}} {
			found := false
			for _, related := range pair[0].RelatedIDs {
				found = found || related == pair[1].ID
			}
			if !found {
				if len(pair[0].RelatedIDs) >= 128 {
					return nil, domain.Fail("invalid_input", "too many requirement links")
				}
				pair[0].RelatedIDs = append(pair[0].RelatedIDs, pair[1].ID)
				sort.Strings(pair[0].RelatedIDs)
				pair[0].Version++
				if e := tx.Put("requirement", pair[0].ID, pair[0]); e != nil {
					return nil, e
				}
			}
		}
		return []domain.Requirement{left, right}, s.audit(tx, a, "requirements.linked", id, in.OtherID)
	})
}

func (s *Service) TestReadiness(ctx context.Context, a Actor, commit string) (domain.TestReadiness, error) {
	out := domain.TestReadiness{Commit: commit, Required: []string{}, Passed: []string{}, Missing: []string{}, Failed: []string{}, Uncovered: []string{}}
	if e := owner(a); e != nil {
		return out, e
	}
	fixed, e := gitops.ParseOID(commit)
	if e != nil {
		return out, domain.Fail("invalid_input", "full fixed commit required")
	}
	commit = string(fixed)
	out.Commit = commit
	e = s.DB.View(ctx, func(tx *store.Tx) error {
		suites, e := store.List[domain.TestSuite](tx, "test_suite")
		if e != nil {
			return e
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return e
		}
		covered := map[string]bool{}
		for _, suite := range suites {
			if !suite.Enabled || suite.Stage == "merge" {
				continue
			}
			platforms := suite.Platforms
			if len(platforms) == 0 {
				platforms = []string{runtime.GOOS}
			}
			for _, platform := range platforms {
				label := suite.ID + "@" + platform
				out.Required = append(out.Required, label)
				var latest *domain.Run
				for i := range runs {
					r := &runs[i]
					if r.Kind != "verification" || r.Base != commit || r.TestReport == nil || r.TestReport.SuiteID != suite.ID || r.TestReport.SuiteVersion != suite.Version || r.TestReport.Digest != suiteDigest(suite) || r.TestReport.Platform != platform {
						continue
					}
					if latest == nil || r.StartedAt.After(latest.StartedAt) || (r.StartedAt.Equal(latest.StartedAt) && r.ID > latest.ID) {
						latest = r
					}
				}
				if latest == nil {
					out.Missing = append(out.Missing, label)
					continue
				}
				if latest.State != "succeeded" && latest.State != "failed" {
					out.Missing = append(out.Missing, label)
					continue
				}
				accepted, currentContract := false, false
				for _, t := range tasks {
					if t.ID == latest.TaskID && requirementTaskGate(tx, t) == nil {
						currentContract = true
					}
					if t.ID == latest.TaskID && t.State == "verified" && t.Generation == latest.Generation && t.Verification != nil && t.Verification.Digest == latest.TestReport.Digest && requirementTaskGate(tx, t) == nil {
						accepted = true
					}
				}
				if !currentContract {
					out.Missing = append(out.Missing, label)
					continue
				}
				if accepted && latest.State == "succeeded" && latest.Quiescent && domain.ChecksPassed(suite.Checks, latest.TestReport.Checks) {
					out.Passed = append(out.Passed, label)
					for _, id := range suite.RequirementIDs {
						covered[id] = true
					}
				} else {
					out.Failed = append(out.Failed, label)
				}
			}
		}
		requirements, e := store.List[domain.Requirement](tx, "requirement")
		if e != nil {
			return e
		}
		for _, requirement := range requirements {
			if domain.RequirementActive(requirement) && !covered[requirement.ID] {
				out.Uncovered = append(out.Uncovered, requirement.ID)
			}
		}
		out.Ready = len(out.Required) > 0 && len(out.Missing) == 0 && len(out.Failed) == 0
		return nil
	})
	return out, e
}

// The mandatory baseline checks always run. Unknown impact and protected
// changes conservatively select all merge suites; path filters only add checks.
func mergeCheckPlan(p domain.Project, suites []domain.TestSuite, paths []string, highRisk bool) (domain.CheckPlan, error) {
	plan := domain.CheckPlan{PolicyVersion: p.TestPolicyVersion, Checks: append([]domain.Check{}, p.Checks...)}
	selected := map[string]bool{}
	known := map[string]bool{}
	for _, suite := range suites {
		if !suite.Enabled || suite.Stage != "merge" {
			continue
		}
		if len(suite.MatchPaths) == 0 {
			selected[suite.ID] = true
			continue
		}
		scope, e := gitops.NewScope(suite.MatchPaths, nil)
		if e != nil {
			return plan, e
		}
		for _, path := range paths {
			if scope.Check([]string{path}).Allowed {
				selected[suite.ID], known[path] = true, true
			}
		}
	}
	unknown := len(paths) == 0
	for _, path := range paths {
		unknown = unknown || !known[path]
	}
	sort.Slice(suites, func(i, j int) bool { return suites[i].ID < suites[j].ID })
	for _, suite := range suites {
		if !suite.Enabled || suite.Stage != "merge" || (!highRisk && !unknown && !selected[suite.ID]) {
			continue
		}
		if !domain.TestPlatformAllowed(suite, runtime.GOOS) {
			return plan, domain.Fail("test_platform", "required merge suite unavailable on this center platform")
		}
		for _, check := range suite.Checks {
			check.Name = suite.ID + "/" + check.Name
			plan.Checks = append(plan.Checks, check)
		}
		plan.OutputPaths = append(plan.OutputPaths, suite.OutputPaths...)
	}
	plan.OutputPaths = append(plan.OutputPaths, domain.CheckOutputs(plan.Checks)...)
	b, e := json.Marshal(plan)
	plan.Digest = Hash(string(b))
	return plan, e
}

func candidateChecks(p domain.Project, c domain.Candidate) []domain.Check {
	if c.CheckPlan != nil {
		return c.CheckPlan.Checks
	}
	return p.Checks // old persisted candidates must still satisfy baseline checks
}

// A configured critical failure remains a merge fence until that suite passes
// on the actual main/candidate version. A green run on unrelated code cannot
// erase the failure; in-flight and cancelled runs do not erase it either.
func blockingTestFailures(tx *store.Tx, commits ...string) ([]domain.Run, error) {
	suites, e := store.List[domain.TestSuite](tx, "test_suite")
	if e != nil {
		return nil, e
	}
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return nil, e
	}
	tasks, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return nil, e
	}
	accepted := map[string]bool{}
	for _, t := range tasks {
		if t.State == "verified" {
			accepted[t.ID] = true
		}
	}
	var blocked []domain.Run
	for _, suite := range suites {
		if !suite.Enabled || !suite.BlockMergesOnFailure || suite.Stage == "merge" {
			continue
		}
		failures := map[string]domain.Run{}
		for _, run := range runs {
			if run.Kind != "verification" || run.TestReport == nil || run.TestReport.SuiteID != suite.ID || run.TestReport.Digest != suiteDigest(suite) || !run.Quiescent {
				continue
			}
			if run.State == "failed" {
				failures[run.TestReport.Platform] = run
			}
			if run.State == "succeeded" && accepted[run.TaskID] && domain.ChecksPassed(suite.Checks, run.TestReport.Checks) {
				for _, commit := range commits {
					if run.Base == commit {
						delete(failures, run.TestReport.Platform)
					}
				}
			}
		}
		platforms := []string{}
		for platform := range failures {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		for _, platform := range platforms {
			blocked = append(blocked, failures[platform])
		}
	}
	return blocked, nil
}
