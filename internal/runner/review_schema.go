package runner

import "encoding/json"

func reviewOutputSchema(candidate string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"candidate", "verdict", "summary", "findings", "run_id"},
		"properties": map[string]any{
			"candidate": map[string]any{"type": "string", "enum": []string{candidate}},
			"verdict":   map[string]any{"type": "string", "enum": []string{"approve", "changes_requested", "blocked"}},
			"summary":   map[string]any{"type": "string"},
			"findings":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"run_id":    map[string]any{"type": "string", "enum": []string{""}},
		},
	})
	return b
}
