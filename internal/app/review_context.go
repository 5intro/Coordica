package app

import (
	"errors"
	"maps"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Only negative reviews of stopped executions under the same authority are
// relevant. Keep the latest complete findings, never a truncated conversation.
func assignedReviewContext(tx *store.Tx, task domain.Task, run domain.Run) (*domain.ReviewContext, error) {
	if run.Kind != "review" {
		return nil, nil
	}
	issues, err := applicableReviewIssues(tx, task, run.PolicyVersion)
	if err != nil {
		return nil, err
	}
	candidates, err := store.List[domain.Candidate](tx, "candidate")
	if err != nil {
		return nil, err
	}
	var latest *domain.Candidate
	round := 1
	for _, c := range candidates {
		if c.ID == run.CandidateID || c.TaskID != task.ID || c.AuthorizationVersion != task.AuthorizationVersion ||
			c.PolicyVersion != run.PolicyVersion || c.BaselineVersion != run.BaselineVersion || c.BaselineDigest != run.BaselineDigest ||
			c.Review == nil || c.Review.Candidate != c.Commit || c.State != "rejected" ||
			(c.Review.Verdict != "blocked" && c.Review.Verdict != "changes_requested") {
			continue
		}
		var previous domain.Run
		if err := tx.Get("run", c.Review.RunID, &previous); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue // Legacy feedback without a stop receipt is not evidence.
			}
			return nil, err
		}
		if !previous.Quiescent || previous.FinishedAt == nil || previous.Kind != "review" || previous.State != "succeeded" ||
			previous.TaskID != task.ID || previous.CandidateID != c.ID || previous.Base != c.Commit ||
			previous.AuthorizationVersion != task.AuthorizationVersion || previous.GoalVersion != run.GoalVersion ||
			previous.PolicyVersion != c.PolicyVersion || previous.BaselineVersion != c.BaselineVersion || previous.BaselineDigest != c.BaselineDigest ||
			previous.GoalAuthorizationVersion != run.GoalAuthorizationVersion || !maps.Equal(previous.RequirementRevisions, run.RequirementRevisions) {
			continue
		}
		round++
		if latest == nil || c.CreatedAt.After(latest.CreatedAt) || c.CreatedAt.Equal(latest.CreatedAt) && c.ID > latest.ID {
			copy := c
			latest = &copy
		}
	}
	if latest == nil {
		if len(issues) > 0 {
			return &domain.ReviewContext{Round: round, Issues: issues, Findings: []string{}}, nil
		}
		return nil, nil
	}
	return &domain.ReviewContext{Issues: issues, Round: round, PreviousCandidate: latest.Commit, PreviousMain: latest.Main,
		Summary: latest.Review.Summary, Findings: latest.Review.Findings}, nil
}
