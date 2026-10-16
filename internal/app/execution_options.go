package app

import "coordica/internal/domain"

func configuredExecution(capabilities []domain.Capability, preference domain.ToolScheduling) (string, string, error) {
	var capability domain.Capability
	for _, reported := range capabilities {
		if reported.Tool == preference.Tool {
			capability = reported
			break
		}
	}
	if err := domain.ValidateExecutionOptions(capability, preference.Model, preference.ReasoningEffort); err != nil {
		return "", "", err
	}
	return preference.Model, preference.ReasoningEffort, nil
}

// Preferences are copied inside the claim transaction, using this claim's
// report. Later configuration edits cannot alter an already assigned Run.
func nodeExecution(n domain.Node, tool string) (string, string, bool) {
	for _, preference := range n.Tools {
		if preference.Tool == tool {
			model, effort, err := configuredExecution(n.Capabilities, preference)
			return model, effort, err == nil
		}
	}
	return "", "", true
}
