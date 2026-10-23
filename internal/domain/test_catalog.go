package domain

// Catalog definitions describe behavior, not executable authority. Only an
// Owner-maintained suite supplies commands. Case versions are pinned at enqueue.
type CriterionRef struct {
	RequirementID string `json:"requirement_id"`
	Revision      int64  `json:"revision"`
	PlanVersion   int64  `json:"plan_version"`
	CriterionID   string `json:"criterion_id"`
}

type TestCase struct {
	ID           string         `json:"id"`
	Version      int64          `json:"version"`
	Name         string         `json:"name"`
	Purpose      string         `json:"purpose"`
	Categories   []string       `json:"categories"`
	Steps        []string       `json:"steps"`
	Assertions   []string       `json:"assertions"`
	Enabled      bool           `json:"enabled"`
	Implemented  bool           `json:"implemented"`
	SuiteID      string         `json:"suite_id"`
	CheckName    string         `json:"check_name"`
	ReportFormat string         `json:"report_format"`
	ExternalID   string         `json:"external_id"`
	Coverage     []CriterionRef `json:"coverage"`
}

type TestCaseSave struct {
	ExpectedVersion int64    `json:"expected_version"`
	Case            TestCase `json:"case"`
}

type TestCaseQuery struct {
	After         string `json:"after"`
	Category      string `json:"category"`
	RequirementID string `json:"requirement_id"`
	Limit         int    `json:"limit"`
}

type TestCasePage struct {
	Items []TestCase `json:"items"`
	Next  string     `json:"next"`
}

type AcceptanceCriterion struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Method string `json:"method"` // automated or manual; neither implies acceptance
}

type AcceptancePlan struct {
	RequirementID string                `json:"requirement_id"`
	Revision      int64                 `json:"revision"`
	Version       int64                 `json:"version"`
	Criteria      []AcceptanceCriterion `json:"criteria"`
}

type AcceptanceSave struct {
	ExpectedRequirementVersion int64          `json:"expected_requirement_version"`
	ExpectedVersion            int64          `json:"expected_version"`
	Plan                       AcceptancePlan `json:"plan"`
}

type TestCaseSummary struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Name    string `json:"name"`
}

type CaseEvidence struct {
	Case     TestCaseSummary `json:"case"`
	State    string          `json:"state"`
	RunID    string          `json:"run_id"`
	Platform string          `json:"platform"`
}

type CriterionCoverage struct {
	Criterion AcceptanceCriterion `json:"criterion"`
	State     string              `json:"state"`
	Cases     []CaseEvidence      `json:"cases"`
}

type AcceptanceCoverage struct {
	Requirement Requirement         `json:"requirement"`
	Plan        AcceptancePlan      `json:"plan"`
	Commit      string              `json:"commit"`
	Digest      string              `json:"digest"`
	Current     bool                `json:"current"`
	Verified    bool                `json:"verified"`
	Criteria    []CriterionCoverage `json:"criteria"`
	Gaps        []TestGap           `json:"gaps"`
}

type TestGap struct {
	CriterionID string `json:"criterion_id"`
	CaseID      string `json:"case_id"`
	RunID       string `json:"run_id,omitempty"`
	Reason      string `json:"reason"`
	Action      string `json:"action"`
}

type TestSupplementRequest struct {
	RequirementID  string `json:"requirement_id"`
	Commit         string `json:"commit"`
	ExpectedDigest string `json:"expected_digest"`
}

// Stored under the generated requirement ID: duplicate submissions of the
// same coverage digest return the same requirement, even with a fresh key.
type TestSupplementPlan struct {
	RequirementID  string    `json:"requirement_id"`
	SourceID       string    `json:"source_id"`
	SourceRevision int64     `json:"source_revision"`
	PlanVersion    int64     `json:"plan_version"`
	Commit         string    `json:"commit"`
	Digest         string    `json:"digest"`
	Gaps           []TestGap `json:"gaps"`
}

type TestCatalogPin struct {
	Versions map[string]int64 `json:"versions"`
}
