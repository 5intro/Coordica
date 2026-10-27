package domain

// TaskToolRequest selects execution machinery, not new task content or scope.
// Only an Owner may change an unissued task through the application use case.
type TaskToolRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Tool            string `json:"tool"`
	Reason          string `json:"reason"`
}
