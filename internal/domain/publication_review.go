package domain

// PullRequestReviewState derives feedback from the currently accepted sources.
// It grants no merge authority; other checks and fixed-version gates still apply.
func PullRequestReviewState(reviews []RemoteReview, external []ExternalReview, head string) string {
	state := "none"
	for _, review := range reviews {
		if review.State == "CHANGES_REQUESTED" {
			return "changes_requested"
		}
		if review.State == "APPROVED" && review.Commit == head {
			state = "approved"
		}
	}
	for _, report := range external {
		if report.State == "failure" {
			return "changes_requested"
		}
	}
	return state
}
