package app

import (
	"context"
	"fmt"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

type checkInputSource interface {
	CheckInputPaths(context.Context, string, string, []string) ([]string, error)
}

func readCheckInputs(ctx context.Context, service *Service, project domain.Project, sha string) ([]string, error) {
	var suites []domain.TestSuite
	if err := service.DB.View(ctx, func(tx *store.Tx) (err error) {
		suites, err = store.List[domain.TestSuite](tx, "test_suite")
		return
	}); err != nil {
		return nil, err
	}
	checks := append([]domain.Check{}, project.Checks...)
	for _, suite := range suites {
		if suite.Enabled && suite.Stage == "merge" {
			checks = append(checks, suite.Checks...)
		}
	}
	var inputs []string
	seen := map[string]bool{}
	for _, check := range checks {
		for _, input := range check.RequiredPaths {
			if !seen[input] {
				inputs, seen[input] = append(inputs, input), true
			}
		}
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	reader, ok := service.analysisSource().(checkInputSource)
	if !ok {
		return nil, domain.Fail("check_inputs", "source cannot attest required check inputs at the fixed revision")
	}
	return reader.CheckInputPaths(ctx, analysisRepository(project), sha, inputs)
}

// This checks explicit contracts rather than parsing natural-language acceptance
// or guessing entrypoints from executable names. It runs before any batch writes.
func validatePlanningCheckInputs(checks []domain.Check, baseline domain.Initialization,
	proposals map[string]domain.TaskProposal, existing []domain.Task, suites ...domain.TestSuite,
) error {
	available := append(append([]string{}, baseline.CheckInputPaths...), baseline.AvailablePaths...)
	for key := range proposals {
		plan, err := mergeCheckPlan(domain.Project{Checks: checks}, append([]domain.TestSuite{}, suites...),
			proposals[key].Paths, unprovenPlanningPatterns(proposals[key].Paths, suites))
		if err != nil {
			return err
		}
		for _, check := range plan.Checks {
			for _, input := range check.RequiredPaths {
				present := false
				for _, path := range available {
					present = present || path == input || strings.HasPrefix(path, input+"/")
				}
				for _, task := range existing {
					if task.State == "integrated" {
						present = present || planningScopeCovers(task.AllowedPaths, input)
					}
				}
				if !present && !planningAncestorsProvide(key, input, proposals, map[string]bool{}) {
					return domain.Fail("planning_inputs", fmt.Sprintf(
						"task %q cannot satisfy check %q: input %q is absent from the baseline, its write scope and completed dependencies", key, check.Name, input))
				}
			}
		}
	}
	return nil
}

// Runtime selects all merge suites for unknown changed paths. Reuse that rule;
// a task glob is not a concrete changed file or proof of pattern containment.
func unprovenPlanningPatterns(paths []string, suites []domain.TestSuite) bool {
	for _, path := range paths {
		if !strings.ContainsAny(path, "*?[") && !strings.HasSuffix(path, "/") {
			continue
		}
		covered := false
		for _, suite := range suites {
			if !suite.Enabled || suite.Stage != "merge" {
				continue
			}
			for _, pattern := range suite.MatchPaths {
				covered = covered || pattern == path || pattern == "**"
			}
		}
		if !covered {
			return true
		}
	}
	return false
}

func planningScopeCovers(paths []string, input string) bool {
	scope, err := gitops.NewScope(paths, nil)
	if err != nil {
		return false
	}
	if scope.Check([]string{input}).Allowed {
		return true
	}
	for _, path := range paths {
		if strings.HasPrefix(path, input+"/") {
			return true // Creating a permitted descendant also creates its parent directory.
		}
	}
	return false
}

func planningAncestorsProvide(key, input string, proposals map[string]domain.TaskProposal, seen map[string]bool) bool {
	if seen[key] {
		return false
	}
	seen[key] = true
	q := proposals[key]
	if planningScopeCovers(q.Paths, input) {
		return true
	}
	for _, dependency := range q.Dependencies {
		if planningAncestorsProvide(dependency, input, proposals, seen) {
			return true
		}
	}
	return false
}
