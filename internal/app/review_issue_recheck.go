package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

// An old rejected candidate remains actionable only before any replacement
// execution. This check never cancels a running repair or restores old scope.
func issueCandidateCurrent(tx *store.Tx, t domain.Task, c domain.Candidate) error {
	p, err := getProject(tx)
	if err != nil {
		return err
	}
	if c.AuthorizationVersion != t.AuthorizationVersion || c.PolicyVersion != p.PolicyVersion ||
		c.BaselineVersion != t.BaselineVersion || c.BaselineDigest != t.BaselineDigest {
		return domain.Fail("authorization_changed", "issue disposition must use the current candidate authority")
	}
	if t.CurrentCandidateID == c.ID {
		return nil
	}
	if t.CurrentCandidateID != "" || t.CurrentRunID != "" || (t.State != "ready" && t.State != "awaiting_integration") {
		return domain.Fail("candidate_changed", "a replacement execution or candidate superseded this review")
	}
	if err := rejectedRepairSource(tx, t, p, c); err != nil {
		return err
	}
	var r domain.Run
	if err := tx.Get("run", c.Review.RunID, &r); err != nil {
		return err
	}
	if r.Generation != t.Generation || r.CandidateID != c.ID || r.Base != c.Commit || !r.Quiescent || r.State != "succeeded" {
		return domain.Fail("candidate_changed", "review no longer belongs to the latest stopped execution")
	}
	return requireQuiescence(tx, t.ID)
}

func (c ReviewIssues) recheck(tx *store.Tx, a Actor, t domain.Task, candidate domain.Candidate, in domain.ReviewIssueDecision) error {
	if err := version(candidate.Version, in.CandidateVersion); err != nil {
		return err
	}
	if candidate.State != "rejected" || t.CurrentRunID != "" || t.ReservedSeconds != 0 || t.ApprovedVersion == 0 ||
		(t.HoldReason != "" && t.HoldReason != "paused" && t.HoldReason != "human_decision") {
		return domain.Fail("conflict", "only a stopped rejected review can be reconsidered")
	}
	if err := requireQuiescence(tx, t.ID); err != nil {
		return err
	}
	p, err := getProject(tx)
	if err != nil {
		return err
	}
	if err := rejectedRepairSource(tx, t, p, candidate); err != nil {
		return err
	}
	if err := baselineGate(tx, nil, false); err != nil {
		return err
	}
	if err := goalGate(tx, &t, false); err != nil {
		return err
	}
	if t.Retries >= t.MaxRetries || taskTimeSpent(tx, t) {
		return domain.Fail("budget", "finite review retry allowance exhausted")
	}
	if err := reviewLedgerReady(tx, t.ID, candidate.Commit); err != nil {
		return err
	}
	if err := tx.Put("review_receipt", candidate.Review.RunID, *candidate.Review); err != nil {
		return err
	}
	// The immutable review_receipt retains the former verdict. Approval is
	// intentionally not synthesized and no retry/repair counter is refunded.
	candidate.State, candidate.Detail, candidate.Review = "reviewing", "Owner requested reconsideration of issue dispositions", nil
	candidate.Version++
	t.State, t.CurrentCandidateID, t.HoldReason = "awaiting_integration", candidate.ID, "paused"
	t.Retries++
	t.Version++
	if err := tx.Put("candidate", candidate.ID, candidate); err != nil {
		return err
	}
	if err := tx.Put("task", t.ID, t); err != nil {
		return err
	}
	return c.Service.audit(tx, a, "review.issue_recheck", candidate.ID, in.Reason)
}
