package app

import (
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func applyCandidateReview(tx *store.Tx, s *Service, t *domain.Task, r domain.Run, c *domain.Candidate, review domain.Review) error {
	review.RunID = r.ID
	if strings.TrimSpace(review.Summary) == "" || review.Findings == nil {
		return domain.Fail("invalid_input", "review summary and findings are required")
	}
	if review.Verdict != "approve" && review.Verdict != "changes_requested" && review.Verdict != "blocked" {
		return domain.Fail("invalid_input", "unknown review verdict")
	}
	if err := domain.ValidateRecoveryHint(review.Recovery, review.Verdict); err != nil {
		return err
	}
	if r.ReviewContractVersion == domain.ReviewContractVersion {
		if c.ReviewRules == nil || r.ReviewRulesDigest != c.ReviewRules.Digest {
			return domain.Fail("rules_changed", "review rules no longer match its assignment")
		}
		if err := domain.ValidateStructuredReview(review, r.ReviewRulesDigest); err != nil {
			return err
		}
	} else if c.ReviewRules != nil || review.RulesDigest != "" || len(review.Issues) > 0 || len(review.Resolutions) > 0 {
		return domain.Fail("invalid_result", "structured review requires an explicitly negotiated contract")
	}
	if err := (ReviewIssues{Service: s}).record(tx, *t, r, *c, review); err != nil {
		return err
	}
	// A later re-review may replace candidate.Review; retain every validated
	// receipt under its immutable source Run in the same transaction.
	if err := tx.Put("review_receipt", r.ID, review); err != nil {
		return err
	}
	c.Review = &review
	c.Version++
	if review.Verdict == "approve" {
		c.State = "validating"
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		if c.CheckPlan != nil && c.CheckPlan.PolicyVersion == p.TestPolicyVersion && domain.ChecksPassed(candidateChecks(p, *c), c.Checks) && c.CheckRunID != "" {
			c.State = "ready_to_land"
		}
		return nil
	}
	c.State, c.Detail = "rejected", review.Summary
	settings, err := automation(tx)
	if err != nil {
		return err
	}
	if review.Recovery != nil && t.GoalVersion != 0 && settings.Enabled {
		t.HoldReason, t.CurrentRunID = "failure_assessment", ""
	} else {
		queueRepair(t, c, review.Verdict == "changes_requested")
	}
	return nil
}
