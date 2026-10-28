package domain

// ReviewContext is historical feedback on the same task authority, not an
// approval or an instruction to ignore the complete current candidate.
type ReviewContext struct {
	Round             int      `json:"round"`
	PreviousCandidate string   `json:"previous_candidate"`
	PreviousMain      string   `json:"previous_main"`
	Summary           string   `json:"summary"`
	Findings          []string `json:"findings"`
}
