package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

// Defaults select work, not a global product allowlist. A task still names its
// authorized tool; each Run pins the receiving node's explicit model settings.
func validateDevelopmentChoice(tx *store.Tx, settings domain.AutomationSettings) error {
	legacy := domain.DefaultAutomationSettings()
	if settings.DevelopmentTool == legacy.DevelopmentTool && settings.DevelopmentModel == legacy.DevelopmentModel {
		return nil // Existing installations may configure controls before pairing.
	}
	nodes, err := store.List[domain.Node](tx, "node")
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Revoked {
			continue
		}
		for _, cap := range node.Capabilities {
			if cap.Tool == settings.DevelopmentTool && cap.Available && cap.Structured && cap.Cancel &&
				domain.ValidateExecutionOptions(cap, settings.DevelopmentModel, "") == nil {
				return nil
			}
		}
	}
	return domain.Fail("developer_unavailable", "selected development tool/model is not reported by a paired Runner")
}

func taskExecution(node domain.Node, tool string, settings domain.AutomationSettings) (string, string, bool) {
	for _, preference := range node.Tools {
		if preference.Tool == tool && (preference.Model != "" || preference.ReasoningEffort != "") {
			return nodeExecution(node, tool) // Invalid explicit choices never fall back.
		}
	}
	legacy := domain.DefaultAutomationSettings()
	if tool != settings.DevelopmentTool || tool == legacy.DevelopmentTool && settings.DevelopmentModel == legacy.DevelopmentModel {
		return nodeExecution(node, tool)
	}
	for _, cap := range node.Capabilities {
		if cap.Tool == tool {
			return settings.DevelopmentModel, "", domain.ValidateExecutionOptions(cap, settings.DevelopmentModel, "") == nil
		}
	}
	return "", "", false
}
