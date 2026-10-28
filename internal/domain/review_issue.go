package domain

import "time"

const ReviewContractVersion = 1

// Model keys identify a failure across rounds; center-owned IDs identify its
// durable record. Evidence is an attributed claim, never an execution grant.
type ReviewFinding struct {
	Key      string `json:"key"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Trigger  string `json:"trigger"`
	Evidence string `json:"evidence"`
	Impact   string `json:"impact"`
}

type ReviewResolution struct {
	IssueID         string `json:"issue_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Disposition     string `json:"disposition"`
	Reason          string `json:"reason"`
}

type ReviewIssue struct {
	ID               string        `json:"id"`
	Version          int64         `json:"version"`
	TaskID           string        `json:"task_id"`
	Source           string        `json:"source"`
	Finding          ReviewFinding `json:"finding"`
	State            string        `json:"state"`
	FirstCandidate   string        `json:"first_candidate"`
	LastCandidate    string        `json:"last_candidate"`
	SourceRunID      string        `json:"source_run_id"`
	RulesDigest      string        `json:"rules_digest"`
	ResolutionCommit string        `json:"resolution_commit"`
	ResolutionReason string        `json:"resolution_reason"`
	ResolvedBy       string        `json:"resolved_by"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type ReviewIssueQuery struct {
	TaskID string `json:"task_id"`
	After  string `json:"after"`
	Limit  int    `json:"limit"`
}

type ReviewIssuePage struct {
	Items []ReviewIssue `json:"items"`
	Next  string        `json:"next"`
}

type ReviewIssueDecision struct {
	ExpectedVersion  int64  `json:"expected_version"`
	CandidateID      string `json:"candidate_id"`
	Commit           string `json:"commit"`
	Disposition      string `json:"disposition"`
	Reason           string `json:"reason"`
	RequestRecheck   bool   `json:"request_recheck,omitempty"`
	CandidateVersion int64  `json:"candidate_version,omitempty"`
}

func ValidateReviewFinding(f ReviewFinding) error {
	if !ChatIdentifier.MatchString(f.Key) || !ChatText(f.Title, 512, true) || !ChatText(f.Path, 1024, false) || f.Line < 0 ||
		!ChatText(f.Trigger, 1024, true) || !ChatText(f.Evidence, 2048, true) || !ChatText(f.Impact, 2048, true) {
		return Fail("invalid_result", "bounded issue key, location, trigger, evidence and impact required")
	}
	switch f.Severity {
	case "P0", "P1", "P2", "P3":
		return nil
	}
	return Fail("invalid_result", "issue severity must be P0..P3")
}

// Every older disposition must be revalidated on a new candidate. An Owner's
// accepted risk is an explicit exception on that commit, never a claimed fix.
func ReviewIssueBlocks(v ReviewIssue, commit string) bool {
	return v.State == "open" || v.ResolutionCommit != commit
}

func ValidateStructuredReview(v Review, digest string) error {
	if v.RulesDigest != digest || digest == "" || v.Issues == nil || v.Resolutions == nil || len(v.Findings) != 0 ||
		len(v.Issues) > 32 || len(v.Resolutions) > 64 || v.Verdict == "approve" && len(v.Issues) != 0 || v.Verdict == "changes_requested" && len(v.Issues) == 0 {
		return Fail("invalid_result", "review requires exact rules digest, structured issues/resolutions and empty legacy findings")
	}
	keys := map[string]bool{}
	for _, finding := range v.Issues {
		if keys[finding.Key] {
			return Fail("invalid_result", "duplicate issue key")
		}
		keys[finding.Key] = true
		if err := ValidateReviewFinding(finding); err != nil {
			return err
		}
	}
	ids := map[string]bool{}
	for _, resolution := range v.Resolutions {
		if ids[resolution.IssueID] || !ChatText(resolution.IssueID, 128, true) || resolution.ExpectedVersion < 1 ||
			(resolution.Disposition != "fixed" && resolution.Disposition != "false_positive") || !ChatText(resolution.Reason, 2048, true) {
			return Fail("invalid_result", "unique versioned issue resolutions with evidence are required")
		}
		ids[resolution.IssueID] = true
	}
	return nil
}
