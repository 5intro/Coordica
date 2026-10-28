package app

import (
	"errors"
	"maps"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// The Owner ledger retains every historical issue. Execution context and merge
// gates use only evidence from the same authority and a stopped source Run.
func applicableReviewIssues(tx *store.Tx, task domain.Task, policy int64) ([]domain.ReviewIssue, error) {
	issues, err := taskReviewIssues(tx, task.ID)
	if err != nil {
		return nil, err
	}
	result := []domain.ReviewIssue{}
	for _, issue := range issues {
		if issue.Source != "internal" {
			continue
		}
		var r domain.Run
		if err := tx.Get("run", issue.SourceRunID, &r); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				if issue.RulesDigest != "" {
					return nil, domain.Fail("invalid_context", "structured issue source Run is missing; review history requires repair")
				}
				continue
			}
			return nil, err
		}
		if r.Kind != "review" || r.State != "succeeded" || !r.Quiescent || r.FinishedAt == nil || r.TaskID != task.ID ||
			r.AuthorizationVersion != task.AuthorizationVersion || r.PolicyVersion != policy || r.GoalVersion != task.GoalVersion ||
			r.GoalAuthorizationVersion != task.GoalAuthorizationVersion || r.BaselineVersion != task.BaselineVersion || r.BaselineDigest != task.BaselineDigest ||
			!maps.Equal(r.RequirementRevisions, task.RequirementRevisions) {
			continue
		}
		var c domain.Candidate
		if err := tx.Get("candidate", r.CandidateID, &c); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				if issue.RulesDigest != "" {
					return nil, domain.Fail("invalid_context", "structured issue candidate is missing; review history requires repair")
				}
				continue
			}
			return nil, err
		}
		if c.TaskID == task.ID && c.Commit == r.Base && c.PolicyVersion == r.PolicyVersion && c.AuthorizationVersion == r.AuthorizationVersion &&
			c.BaselineVersion == r.BaselineVersion && c.BaselineDigest == r.BaselineDigest {
			result = append(result, issue)
		}
	}
	return result, nil
}
