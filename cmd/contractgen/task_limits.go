package main

import (
	"reflect"

	"coordica/internal/domain"
)

// Both public and native proposal contracts describe the same admission rules.
func applyTaskLimits(t reflect.Type, properties map[string]any) {
	if t == reflect.TypeOf(domain.ReviewInputRetryRequest{}) {
		for _, name := range []string{"expected_version", "expected_candidate_version"} {
			properties[name].(map[string]any)["minimum"] = 1
		}
		for name, maximum := range map[string]int{"expected_candidate_id": 128, "expected_commit": 64, "review_run_id": 128, "reason": 2048} {
			properties[name].(map[string]any)["minLength"] = 1
			properties[name].(map[string]any)["maxLength"] = maximum
			if name != "reason" {
				properties[name].(map[string]any)["pattern"] = domain.ChatIdentifier.String()
			}
		}
		return
	}
	if t == reflect.TypeOf(domain.TaskAcceptanceRequest{}) {
		properties["expected_version"].(map[string]any)["minimum"] = 1
		bounds := map[string]int{
			"acceptance": domain.MaxTaskAcceptanceBytes, "reason": domain.MaxTaskAcceptanceReasonBytes, "expected_candidate_id": 128,
		}
		for field, max := range bounds {
			properties[field].(map[string]any)["minLength"] = 1
			properties[field].(map[string]any)["maxLength"] = max
		}
		properties["expected_candidate_id"].(map[string]any)["pattern"] = domain.ChatIdentifier.String()
		return
	}
	if t != reflect.TypeOf(domain.Task{}) && t != reflect.TypeOf(domain.TaskProposal{}) {
		return
	}
	for field, maximum := range map[string]int64{
		"budget_seconds": domain.MaxTaskBudgetSeconds, "max_run_seconds": domain.MaxRunDurationSeconds,
		"max_repairs": domain.MaxTaskRepairs, "max_retries": domain.MaxTaskRetries,
	} {
		minimum := int64(0)
		if field == "budget_seconds" || field == "max_run_seconds" {
			minimum = 1
		}
		properties[field].(map[string]any)["minimum"] = minimum
		properties[field].(map[string]any)["maximum"] = maximum
	}
	properties["max_run_seconds"].(map[string]any)["description"] = "Finite single Run limit; must be <= budget_seconds."
}
