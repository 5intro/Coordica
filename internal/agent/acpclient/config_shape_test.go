package acpclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigOptionShapeUnionAndBoundedDeclarations(t *testing.T) {
	for _, scenario := range []string{"grouped", "flat", "empty", "empty-group", "unknown-category", "missing-category", "missing-id", "null-current", "null-options", "mixed", "duplicate-id", "duplicate-choice", "nested-null", "missing-name", "wrong-type", "too-many", "too-large"} {
		t.Run(scenario, func(t *testing.T) {
			options := fixtureConfigOptions("small")
			o := options[0].(map[string]any)
			group := o["options"].([]any)[0].(map[string]any)
			switch scenario {
			case "flat":
				o["options"] = group["options"]
			case "empty":
				o["options"] = []any{}
			case "empty-group":
				group["options"] = []any{}
			case "unknown-category":
				o["category"] = "unknown-provider-category"
			case "missing-category":
				delete(o, "category")
			case "missing-id":
				delete(o, "id")
			case "null-current":
				o["currentValue"] = nil
			case "null-options":
				o["options"] = nil
			case "mixed":
				o["options"] = append(o["options"].([]any), map[string]any{"value": "other", "name": "Other"})
			case "duplicate-id":
				options = append(options, o)
			case "duplicate-choice":
				group["options"] = append(group["options"].([]any), map[string]any{"value": "small", "name": "Other"})
			case "nested-null":
				group["options"] = nil
			case "missing-name":
				delete(group["options"].([]any)[0].(map[string]any), "name")
			case "wrong-type":
				o["type"] = "input"
			case "too-many":
				for i := 0; i < 65; i++ {
					options = append(options, o)
				}
			case "too-large":
				o["name"] = strings.Repeat("x", 1025)
			}
			raw, _ := json.Marshal(options)
			parsed, err := parseConfigOptions(raw)
			valid := scenario == "grouped" || scenario == "flat" || scenario == "empty" || scenario == "empty-group" || scenario == "unknown-category" || scenario == "missing-category"
			if (err == nil) != valid || (valid && len(parsed) != 1) {
				t.Fatal(scenario, parsed, err)
			}
		})
	}
}
