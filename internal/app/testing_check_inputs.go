package app

import (
	"slices"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// A suite edit cannot extend an Owner-confirmed existence snapshot. Preserve
// known inputs (including attested absence), but require fresh analysis for a
// newly required path omitted from the public inventory. No Git I/O in this transaction.
func invalidateUnattestedSuiteInputs(s *Service, tx *store.Tx, actor Actor, project domain.Project, suite domain.TestSuite) error {
	if !suite.Enabled || suite.Stage != "merge" {
		return nil
	}
	baseline, err := initialization(tx)
	if err != nil || baseline.Version == 0 || baseline.Status == "draft" {
		return err
	}
	suites, err := store.List[domain.TestSuite](tx, "test_suite")
	if err != nil {
		return err
	}
	checks := append([]domain.Check{}, project.Checks...)
	for _, previous := range suites {
		if previous.Enabled && previous.Stage == "merge" {
			checks = append(checks, previous.Checks...)
		}
	}
	known := requiredCheckInputs(checks)
	for _, input := range requiredCheckInputs(suite.Checks) {
		present := slices.Contains(baseline.CheckInputPaths, input)
		for _, path := range baseline.AvailablePaths {
			present = present || path == input || strings.HasPrefix(path, input+"/")
		}
		if present || slices.Contains(known, input) {
			continue
		}
		baseline.Version++
		baseline.Status, baseline.Detail = "draft", "合并检查新增未核验输入；需重新分析与确认共享基线"
		baseline.Result, baseline.ApprovedBaseline = nil, nil
		baseline.Digest, baseline.ApprovedDigest, baseline.ApprovedVersion = "", "", 0
		baseline.CheckInputPaths = nil
		if err = tx.Put("initialization", "current", baseline); err != nil {
			return err
		}
		if err = s.invalidateBaselineTasks(tx); err != nil {
			return err
		}
		return s.audit(tx, actor, "tests.check_inputs.invalidated", suite.ID, input)
	}
	return nil
}
