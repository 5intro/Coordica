package main

import (
	"reflect"

	"coordica/internal/domain"
)

func applyGoalBudgetLimits(t reflect.Type, properties map[string]any) {
	if t != reflect.TypeOf(domain.GoalBudgetRequest{}) {
		return
	}
	for key, minimum := range map[string]int{"expected_version": 0, "expected_goal_version": 1, "expected_authorization_version": 1} {
		properties[key].(map[string]any)["minimum"] = minimum
	}
	properties["budget_seconds"].(map[string]any)["minimum"] = 1
	properties["budget_seconds"].(map[string]any)["maximum"] = domain.MaxTaskBudgetSeconds
	properties["reason"].(map[string]any)["minLength"] = 1
	properties["reason"].(map[string]any)["maxLength"] = 2048
}
