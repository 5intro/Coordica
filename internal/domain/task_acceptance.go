package domain

const MaxTaskAcceptanceBytes = 16 << 10
const MaxTaskAcceptanceReasonBytes = 2048

// Revises a blocked module's contract, not the project or requirement baseline.
// The exact rejected candidate is retained only as input for a new repair Run.
type TaskAcceptanceRequest struct {
	ExpectedVersion     int64  `json:"expected_version"`
	ExpectedCandidateID string `json:"expected_candidate_id"`
	Acceptance          string `json:"acceptance"`
	Reason              string `json:"reason"`
}

func ValidateTaskAcceptanceRequest(in TaskAcceptanceRequest) error {
	if in.ExpectedVersion < 1 || !ChatText(in.Acceptance, MaxTaskAcceptanceBytes, true) ||
		!ChatText(in.Reason, MaxTaskAcceptanceReasonBytes, true) || !ChatIdentifier.MatchString(in.ExpectedCandidateID) {
		return Fail("invalid_input", "bounded acceptance, reason and exact rejected candidate required")
	}
	return nil
}
