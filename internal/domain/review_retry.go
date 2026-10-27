package domain

// An Owner retries material delivery, never approves or replaces the fixed code.
type ReviewInputRetryRequest struct {
	ExpectedVersion          int64  `json:"expected_version"`
	ExpectedCandidateID      string `json:"expected_candidate_id"`
	ExpectedCandidateVersion int64  `json:"expected_candidate_version"`
	ExpectedCommit           string `json:"expected_commit"`
	ReviewRunID              string `json:"review_run_id"`
	Reason                   string `json:"reason"`
}

func ValidateReviewInputRetry(in ReviewInputRetryRequest) error {
	if in.ExpectedVersion < 1 || in.ExpectedCandidateVersion < 1 || !ChatIdentifier.MatchString(in.ExpectedCandidateID) ||
		!ChatIdentifier.MatchString(in.ReviewRunID) || !ChatIdentifier.MatchString(in.ExpectedCommit) || !ChatText(in.Reason, 2048, true) {
		return Fail("invalid_input", "exact task/candidate/review identities and a bounded reason are required")
	}
	return nil
}
