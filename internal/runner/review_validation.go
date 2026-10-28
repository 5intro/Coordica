package runner

import (
	"coordica/internal/domain"
)

// Catch invalid model output before durable delivery. The center separately
// repeats these checks against current transactional state, including races.
func validateAssignedReview(a domain.Assignment, v domain.Review) error {
	if err := domain.ValidateStructuredReview(v, a.Run.ReviewRulesDigest); err != nil {
		return err
	}
	known := map[string]domain.ReviewIssue{}
	if a.ReviewContext != nil {
		for _, issue := range a.ReviewContext.Issues {
			known[issue.ID] = issue
		}
	}
	resolved := map[string]bool{}
	for _, resolution := range v.Resolutions {
		issue, ok := known[resolution.IssueID]
		if !ok || issue.Source != "internal" || issue.TaskID != a.Task.ID || issue.Version != resolution.ExpectedVersion {
			return domain.Fail("invalid_result", "resolution must name an assigned internal issue and version")
		}
		for _, finding := range v.Issues {
			if issue.Finding.Key == finding.Key {
				return domain.Fail("invalid_result", "same issue cannot be reported and resolved")
			}
		}
		resolved[issue.ID] = true
	}
	if v.Verdict == "approve" {
		for id, issue := range known {
			if issue.Source == "internal" && domain.ReviewIssueBlocks(issue, a.Run.Base) && !resolved[id] {
				return domain.Fail("review_unresolved", "approval must explicitly resolve assigned findings on this commit")
			}
		}
	}
	return nil
}
