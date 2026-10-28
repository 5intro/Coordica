package domain

import "time"

const FailureRecoveryPurpose = "failure_recovery"

// RecoveryHint is a reviewer's diagnosis request, never evidence or authority.
// Absence preserves the original bounded same-task repair behavior.
type RecoveryHint struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func ValidateRecoveryHint(h *RecoveryHint, verdict string) error {
	if h == nil {
		return nil
	}
	if (verdict != "blocked" && verdict != "changes_requested") || !FailureCategory(h.Category) || !ChatText(h.Reason, 4096, true) {
		return Fail("invalid_input", "negative review recovery hint requires a bounded category and reason")
	}
	return nil
}

func FailureCategory(v string) bool {
	switch v {
	case "execution_failure", "test_error", "product_defect", "evidence_missing", "contract_mismatch", "unknown":
		return true
	}
	return false
}

// FailureRecoveryPlan is read-only Leader output. The application validates
// any proposed action against the unchanged source contract and current goal.
type FailureRecoveryPlan struct {
	Category string         `json:"category"`
	Reason   string         `json:"reason"`
	Action   string         `json:"action"` // repair_current, retry_review_input, follow_up, needs_owner
	Tasks    []TaskProposal `json:"tasks"`
}

// FailureAssessment preserves the source contract separately from later
// diagnosis and follow-up work. It never grants an old result new authority.
type FailureAssessment struct {
	ID                          string               `json:"id"`
	Version                     int64                `json:"version"`
	SourceTask                  Task                 `json:"source_task"`
	SourceRunID                 string               `json:"source_run_id"`
	SourceRunVersion            int64                `json:"source_run_version"`
	SourceCandidateID           string               `json:"source_candidate_id"`
	SourceCandidateVersion      int64                `json:"source_candidate_version"`
	SourceCommit                string               `json:"source_commit"`
	PolicyVersion               int64                `json:"policy_version"`
	TestPolicyVersion           int64                `json:"test_policy_version"`
	State                       string               `json:"state"`
	RunID                       string               `json:"run_id"`
	Result                      *FailureRecoveryPlan `json:"result,omitempty"`
	FollowupTaskID              string               `json:"followup_task_id"`
	WaitingTaskVersion          int64                `json:"waiting_task_version"`
	RecheckAuthorizationVersion int64                `json:"recheck_authorization_version,omitempty"`
	FollowupCommit              string               `json:"followup_commit,omitempty"`
	PrecheckTaskID              string               `json:"precheck_task_id,omitempty"`
	Detail                      string               `json:"detail"`
	CreatedAt                   time.Time            `json:"created_at"`
	UpdatedAt                   time.Time            `json:"updated_at"`
}

// FailureContext is fixed historical input for related work, never a new grant.
type FailureContext struct {
	Category         string        `json:"category,omitempty"`
	FollowupCommit   string        `json:"followup_commit,omitempty"`
	AssessmentID     string        `json:"assessment_id"`
	SourceTaskID     string        `json:"source_task_id"`
	SourceRunID      string        `json:"source_run_id"`
	SourceCommit     string        `json:"source_commit"`
	SourceTitle      string        `json:"source_title"`
	SourceAcceptance string        `json:"source_acceptance"`
	Reason           string        `json:"reason"`
	Findings         []string      `json:"findings"`
	ExecutionDetail  string        `json:"execution_detail"`
	SourceChecks     []Check       `json:"source_checks"`
	CheckResults     []CheckResult `json:"check_results"`
}
