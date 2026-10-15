package domain

import "strings"

// Test suites are Owner-maintained policy. A task pins a complete immutable
// suite version; an Agent's prose is never a substitute for process results.
type TestSuite struct {
	OutputPaths          []string `json:"output_paths,omitempty"`
	BlockMergesOnFailure bool     `json:"block_merges_on_failure,omitempty"`
	ID                   string   `json:"id"`
	Version              int64    `json:"version"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Stage                string   `json:"stage"` // merge, project, release
	Enabled              bool     `json:"enabled"`
	Categories           []string `json:"categories"`
	MatchPaths           []string `json:"match_paths"`
	Platforms            []string `json:"platforms"`
	RequirementIDs       []string `json:"requirement_ids"`
	Checks               []Check  `json:"checks"`
}

type TestSuiteSave struct {
	ExpectedVersion int64     `json:"expected_version"`
	Suite           TestSuite `json:"suite"`
}

type VerificationSpec struct {
	Suite  TestSuite `json:"suite"`
	Commit string    `json:"commit"`
	Digest string    `json:"digest"`
}

type VerificationRequest struct {
	SuiteID         string   `json:"suite_id"`
	ExpectedVersion int64    `json:"expected_version"`
	Commit          string   `json:"commit"`
	BudgetSeconds   int64    `json:"budget_seconds"`
	MaxRunSeconds   int64    `json:"max_run_seconds"`
	MaxRetries      int      `json:"max_retries"`
	RequirementIDs  []string `json:"requirement_ids"`
}

type TestReport struct {
	Commit       string        `json:"commit"`
	SuiteID      string        `json:"suite_id"`
	SuiteVersion int64         `json:"suite_version"`
	Digest       string        `json:"digest"`
	Platform     string        `json:"platform"`
	Checks       []CheckResult `json:"checks"`
}

// CheckPlan binds the exact commands selected for one fixed merge candidate.
type CheckPlan struct {
	OutputPaths   []string `json:"output_paths,omitempty"`
	PolicyVersion int64    `json:"policy_version"`
	Digest        string   `json:"digest"`
	Checks        []Check  `json:"checks"`
}

type TestReadiness struct {
	Commit    string   `json:"commit"`
	Ready     bool     `json:"ready"`
	Required  []string `json:"required"`
	Passed    []string `json:"passed"`
	Missing   []string `json:"missing"`
	Failed    []string `json:"failed"`
	Uncovered []string `json:"uncovered_requirement_ids"`
}

type RequirementLinkRequest struct {
	ExpectedVersion      int64  `json:"expected_version"`
	OtherExpectedVersion int64  `json:"other_expected_version"`
	OtherID              string `json:"other_id"`
}

func ValidateTestSuite(s TestSuite) error {
	if !ChatIdentifier.MatchString(s.ID) || !ChatText(s.Name, 512, true) || !ChatText(s.Description, 16384, false) || (s.Stage != "merge" && s.Stage != "project" && s.Stage != "release") || len(s.Checks) == 0 || len(s.Checks) > 32 || len(s.MatchPaths) > 128 || len(s.OutputPaths) > 128 || len(s.RequirementIDs) > 128 || len(s.Categories) > 32 || len(s.Platforms) > 3 {
		return Fail("invalid_input", "bounded suite ID, name, stage and 1..32 checks required")
	}
	names := map[string]bool{}
	for _, c := range s.Checks {
		if !ChatText(c.Name, 128, true) || names[c.Name] || !ChatText(c.Command, 4096, true) || len(c.Args) > 128 || c.TimeoutSeconds <= 0 || c.TimeoutSeconds > MaxRunDurationSeconds || (s.Stage == "merge" && c.TimeoutSeconds > 3600) {
			return Fail("invalid_input", "unique check names, executable and finite timeout required")
		}
		names[c.Name] = true
		for _, arg := range c.Args {
			if !ChatText(arg, 16384, false) {
				return Fail("invalid_input", "invalid check argument")
			}
		}
	}
	for _, id := range s.RequirementIDs {
		if !ChatIdentifier.MatchString(id) {
			return Fail("invalid_input", "invalid requirement identifier")
		}
	}
	for _, label := range s.Categories {
		if !ChatText(label, 64, true) {
			return Fail("invalid_input", "invalid test category")
		}
	}
	for _, platform := range s.Platforms {
		if platform != "darwin" && platform != "linux" && platform != "windows" {
			return Fail("invalid_input", "test platform must be darwin, linux or windows")
		}
	}
	return nil
}

func TaskFinished(t Task) bool {
	return t.State == "integrated" || t.State == "verified" || t.State == "cancelled"
}

func ChecksPassed(expected []Check, results []CheckResult) bool {
	if len(expected) == 0 || len(expected) != len(results) {
		return false
	}
	for i, c := range expected {
		if results[i].Name != c.Name || !results[i].Passed || results[i].ExitCode != 0 {
			return false
		}
	}
	return true
}

func TestPlatformAllowed(s TestSuite, platform string) bool {
	return len(s.Platforms) == 0 || strings.Contains("|"+strings.Join(s.Platforms, "|")+"|", "|"+platform+"|")
}
