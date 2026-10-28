package runner

import "encoding/json"

import "coordica/internal/domain"

func assignedReviewSchema(a domain.Assignment) []byte {
	if a.Run.ReviewContractVersion != domain.ReviewContractVersion {
		return reviewOutputSchema(a.Run.Base, a.FailureRecoveryVersion)
	}
	var schema map[string]any
	_ = json.Unmarshal(reviewOutputSchema(a.Run.Base, a.FailureRecoveryVersion), &schema)
	props := schema["properties"].(map[string]any)
	props["rules_digest"] = map[string]any{"type": "string", "enum": []string{a.Run.ReviewRulesDigest}}
	fields := map[string]any{}
	for _, key := range []string{"key", "severity", "title", "path", "trigger", "evidence", "impact"} {
		fields[key] = map[string]any{"type": "string"}
	}
	fields["severity"] = map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}}
	fields["line"] = map[string]any{"type": "integer", "minimum": 0}
	props["issues"] = map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"key", "severity", "title", "path", "line", "trigger", "evidence", "impact"}, "properties": fields}}
	props["resolutions"] = map[string]any{"type": "array", "maxItems": 64, "items": map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"issue_id", "expected_version", "disposition", "reason"}, "properties": map[string]any{
			"issue_id": map[string]any{"type": "string"}, "expected_version": map[string]any{"type": "integer", "minimum": 1},
			"disposition": map[string]any{"type": "string", "enum": []string{"fixed", "false_positive"}}, "reason": map[string]any{"type": "string"}}}}
	schema["required"] = append(schema["required"].([]any), "rules_digest", "issues", "resolutions")
	b, _ := json.Marshal(schema)
	return b
}

func reviewOutputSchema(candidate string, version int) []byte {
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"candidate", "verdict", "summary", "findings", "run_id"},
		"properties": map[string]any{
			"candidate": map[string]any{"type": "string", "enum": []string{candidate}},
			"verdict":   map[string]any{"type": "string", "enum": []string{"approve", "changes_requested", "blocked"}},
			"summary":   map[string]any{"type": "string"},
			"findings":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"run_id":    map[string]any{"type": "string", "enum": []string{""}},
		},
	}
	if version == 1 {
		schema["required"] = append(schema["required"].([]string), "recovery")
		schema["properties"].(map[string]any)["recovery"] = map[string]any{"anyOf": []any{
			map[string]any{"type": "null"},
			map[string]any{"type": "object", "additionalProperties": false,
				"required": []string{"category", "reason"}, "properties": map[string]any{
					"category": map[string]any{"type": "string", "enum": []string{"execution_failure", "test_error", "product_defect", "evidence_missing", "contract_mismatch", "unknown"}},
					"reason":   map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
				}},
		}}
	}
	b, _ := json.Marshal(schema)
	return b
}
