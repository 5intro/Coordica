package runner

import "encoding/json"

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
