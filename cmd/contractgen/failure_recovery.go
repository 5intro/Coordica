package main

import (
	"coordica/internal/domain"
	"encoding/json"
)

func failureRecoverySchema() []byte {
	raw := strictNativeSchema("FailureRecoveryPlan", domain.FailureRecoveryPlan{}, domain.TaskProposal{})
	var schema map[string]any
	must(json.Unmarshal(raw, &schema))
	defs := schema["$defs"].(map[string]any)
	props := defs["FailureRecoveryPlan"].(map[string]any)["properties"].(map[string]any)
	props["category"].(map[string]any)["enum"] = []string{"execution_failure", "test_error", "product_defect", "evidence_missing", "contract_mismatch", "unknown"}
	props["reason"].(map[string]any)["minLength"] = 1
	props["reason"].(map[string]any)["maxLength"] = 4096
	props["action"].(map[string]any)["enum"] = []string{"repair_current", "retry_review_input", "retry_validation", "follow_up", "needs_owner"}
	props["tasks"].(map[string]any)["maxItems"] = 1
	schema["properties"] = props
	raw, err := json.MarshalIndent(schema, "", "  ")
	must(err)
	return append(raw, '\n')
}
