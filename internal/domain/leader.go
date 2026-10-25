package domain

import "time"

// LeaderSelection is a model choice, never execution or scope authority.
type LeaderSelection struct {
	Tool            string `json:"tool"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type LeaderConfiguration struct {
	Version      int64           `json:"version"`
	Selection    LeaderSelection `json:"selection"`
	Capabilities []Capability    `json:"capabilities"`
	QueriedAt    time.Time       `json:"queried_at"`
}

type LeaderConfigureRequest struct {
	ExpectedVersion int64           `json:"expected_version"`
	Selection       LeaderSelection `json:"selection"`
}

func DefaultLeaderSelection() LeaderSelection {
	return LeaderSelection{Tool: "codex", Model: "gpt-6.1-sol"}
}

func ValidateLeaderSelection(selection LeaderSelection) error {
	if ValidateToolID(selection.Tool) != nil || !validModelIdentifier(selection.Model) ||
		selection.ReasoningEffort != "" && !effortIdentifier.MatchString(selection.ReasoningEffort) {
		return Fail("invalid_input", "invalid Leader tool, model or reasoning effort")
	}
	return nil
}
