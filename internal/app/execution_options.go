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
	model, effort := "", ""
	for _, preference := range n.Tools {
		if preference.Tool == tool {
			var err error
			model, effort, err = configuredExecution(n.Capabilities, preference)
			if err != nil {
				return "", "", false
			}
			break
		}
	}
	for _, capability := range n.Capabilities {
		if capability.Tool == tool && model == "" && (tool == "codex" || capability.Protocol == "codex-jsonl") {
			model = "gpt-6.1-sol"
			if capability.ModelDiscovery == "reported" && domain.ValidateExecutionOptions(capability, model, effort) != nil {
				return "", "", false
			}
		}
	}
	return model, effort, true
}
