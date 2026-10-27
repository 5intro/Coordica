package main

import (
	"encoding/json"
	"testing"

	"coordica/internal/domain"
)

func TestTaskProposalNativeLimitsMatchAdmission(t *testing.T) {
	for name, raw := range map[string][]byte{"planning": leaderSchema(), "requirement": requirementSchema()} {
		t.Run(name, func(t *testing.T) {
			var schema struct {
				Defs map[string]struct {
					Properties map[string]struct {
						Minimum *int64 `json:"minimum"`
						Maximum *int64 `json:"maximum"`
					} `json:"properties"`
				} `json:"$defs"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			fields := map[string][]int64{
				"max_repairs": {-1, 0, 2, 3}, "max_retries": {-1, 0, 2, 3},
				"budget_seconds":  {0, 1, domain.MaxTaskBudgetSeconds, domain.MaxTaskBudgetSeconds + 1},
				"max_run_seconds": {0, 1, domain.MaxRunDurationSeconds, domain.MaxRunDurationSeconds + 1},
			}
			for field, values := range fields {
				bounds := schema.Defs["TaskProposal"].Properties[field]
				if bounds.Minimum == nil || bounds.Maximum == nil {
					t.Errorf("native %s has no declared admission bounds", field)
					continue
				}
				for _, value := range values {
					task := domain.Task{Title: "Example", Description: "Objective", Acceptance: "Verified",
						Tool: "dsh", AllowedPaths: []string{"src/**"}, BudgetSeconds: domain.MaxTaskBudgetSeconds, MaxRunSeconds: 1}
					switch field {
					case "max_repairs":
						task.MaxRepairs = int(value)
					case "max_retries":
						task.MaxRetries = int(value)
					case "budget_seconds":
						task.BudgetSeconds = value
					case "max_run_seconds":
						task.MaxRunSeconds = value
					}
					admitted := domain.ValidateTask(task) == nil
					declared := value >= *bounds.Minimum && value <= *bounds.Maximum
					if admitted != declared {
						t.Errorf("%s=%d: domain admission=%v, native schema=%v", field, value, admitted, declared)
					}
				}
			}
		})
	}
}
