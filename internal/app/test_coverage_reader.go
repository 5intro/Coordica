package app

import (
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

var errCoverageCycle = domain.Fail("requirement_changed", "补测证据存在循环或过深依赖，需要重新规划")

// One reader belongs to one immutable transactional view. Reuse completed
// projections and reject recursive supplement evidence instead of skipping it.
type coverageReader struct {
	tx     *store.Tx
	active map[string]bool
	cache  map[string]domain.AcceptanceCoverage
}

func newCoverageReader(tx *store.Tx) *coverageReader {
	return &coverageReader{tx: tx, active: map[string]bool{}, cache: map[string]domain.AcceptanceCoverage{}}
}

func (reader *coverageReader) taskGate(task domain.Task) error {
	for _, id := range task.RequirementIDs {
		var plan domain.TestSupplementPlan
		if err := reader.tx.Get("test_supplement", id, &plan); errors.Is(err, domain.ErrNotFound) {
			continue
		} else if err != nil {
			return err
		}
		if err := reader.supplementGate(plan); err != nil {
			return err
		}
	}
	return nil
}

func (reader *coverageReader) supplementGate(plan domain.TestSupplementPlan) error {
	if reader.active[plan.RequirementID] || len(reader.active) >= 64 {
		return errCoverageCycle
	}
	reader.active[plan.RequirementID] = true
	defer delete(reader.active, plan.RequirementID)
	var source domain.Requirement
	var acceptance domain.AcceptancePlan
	if err := reader.tx.Get("requirement", plan.SourceID, &source); err != nil {
		return err
	}
	if err := reader.tx.Get("acceptance_plan", plan.SourceID, &acceptance); err != nil {
		return err
	}
	if !domain.RequirementActive(source) || domain.RequirementRevisionNumber(source) != plan.SourceRevision ||
		acceptance.Version != plan.PlanVersion || acceptance.Revision != plan.SourceRevision {
		return domain.Fail("requirement_changed", "补测来源或验收计划已变化，需要重新规划")
	}
	var change domain.RequirementChange
	if err := reader.tx.Get("requirement_change", requirementRevisionID(source.ID, plan.SourceRevision), &change); err == nil {
		if change.State != "applied" && change.State != "superseded" {
			return domain.Fail("requirement_changed", "补测来源的需求变更尚未完成规划")
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	coverage, err := reader.read(plan.SourceID, plan.Commit)
	if err != nil {
		return err
	}
	if !coverage.Current || coverage.Digest != plan.Digest {
		return domain.Fail("requirement_changed", "补测覆盖证据已变化，需要重新规划")
	}
	return nil
}

type coverageSuitePin struct {
	SuiteID, Digest, TaskID, RunID string
	TaskVersion, RunVersion        int64
}

func coverageSuitePins(tx *store.Tx, cases []domain.TestCase, commit string) ([]coverageSuitePin, error) {
	pins := []coverageSuitePin{}
	seen := map[string]bool{}
	for _, v := range cases {
		if !v.Implemented || v.SuiteID == "" || seen[v.SuiteID] {
			continue
		}
		seen[v.SuiteID] = true
		var suite domain.TestSuite
		if err := tx.Get("test_suite", v.SuiteID, &suite); err != nil {
			return nil, err
		}
		task, run, err := store.LatestVerification(tx, suite.ID, commit)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		pins = append(pins, coverageSuitePin{SuiteID: suite.ID, Digest: suiteDigest(suite),
			TaskID: task.ID, TaskVersion: task.Version, RunID: run.ID, RunVersion: run.Version})
	}
	return pins, nil
}
