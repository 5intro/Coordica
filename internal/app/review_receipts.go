package app

import (
	"errors"
	"reflect"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Receipts and their source identity are immutable, even if the same candidate
// ID later receives a rebuilt commit. Both are written in the completion tx.
func saveReviewReceipt(tx *store.Tx, run domain.Run, review domain.Review) error {
	var previous domain.Review
	err := tx.Get("review_receipt", run.ID, &previous)
	if err == nil && !reflect.DeepEqual(previous, review) {
		return domain.Fail("invalid_context", "cannot replace immutable review evidence")
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if errors.Is(err, domain.ErrNotFound) {
		if err := tx.Put("review_receipt", run.ID, review); err != nil {
			return err
		}
	}
	var candidateID string
	err = tx.Get("review_source_candidate", run.ID, &candidateID)
	if err == nil {
		if candidateID != run.CandidateID {
			return domain.Fail("invalid_context", "review source candidate changed")
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return tx.Put("review_source_candidate", run.ID, run.CandidateID)
}

func reviewIssueSource(tx *store.Tx, issue domain.ReviewIssue, run domain.Run) error {
	var receipt domain.Review
	if err := tx.Get("review_receipt", run.ID, &receipt); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Fail("invalid_context", "review source receipt is missing")
		}
		return err
	}
	if receipt.RunID != run.ID || receipt.Candidate != run.Base || receipt.Candidate != issue.LastCandidate ||
		receipt.RulesDigest != run.ReviewRulesDigest || !receiptMentionsIssue(receipt, issue) {
		return domain.Fail("invalid_context", "issue does not match its immutable source review")
	}
	var candidateID string
	err := tx.Get("review_source_candidate", run.ID, &candidateID)
	if errors.Is(err, domain.ErrNotFound) {
		// Older receipts predate the explicit identity record. Their immutable
		// review pins the commit; the surviving candidate only binds ID/authority.
		// Its current commit/rules are deliberately not historical evidence.
		var c domain.Candidate
		if err := tx.Get("candidate", run.CandidateID, &c); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.Fail("invalid_context", "legacy review source identity is unavailable")
			}
			return err
		}
		if c.TaskID != run.TaskID || c.AuthorizationVersion != run.AuthorizationVersion || c.PolicyVersion != run.PolicyVersion ||
			c.BaselineVersion != run.BaselineVersion || c.BaselineDigest != run.BaselineDigest {
			return domain.Fail("invalid_context", "legacy review source authority differs")
		}
		candidateID, err = c.ID, nil
	}
	if err != nil {
		return err
	}
	if candidateID == "" || candidateID != run.CandidateID {
		return domain.Fail("invalid_context", "review source identity differs from its Run")
	}
	return nil
}

func receiptMentionsIssue(v domain.Review, issue domain.ReviewIssue) bool {
	for _, finding := range v.Issues {
		if finding.Key == issue.Finding.Key {
			return true
		}
	}
	for _, resolution := range v.Resolutions {
		if resolution.IssueID == issue.ID {
			return true
		}
	}
	for _, finding := range v.Findings {
		if issue.Finding.Key == "legacy_"+Hash(finding)[:24] {
			return true
		}
	}
	return false
}
