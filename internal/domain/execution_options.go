package domain

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelOption is a tool's reported catalog, not execution or authentication proof.
// Model identifiers and effort values are opaque protocol data, never raw argv.
type ModelOption struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	ReasoningEfforts       []string `json:"reasoning_efforts"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort,omitempty"`
}

func validModelIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && !strings.HasPrefix(value, "-") && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

var effortIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]-]{0,63}$`)

func ValidateModelOptions(models []ModelOption) error {
	if len(models) > 64 {
		return Fail("invalid_input", "too many reported models")
	}
	seen := map[string]bool{}
	for _, model := range models {
		if !validModelIdentifier(model.ID) || seen[model.ID] || len(model.Name) > 256 || len(model.ReasoningEfforts) > 16 {
			return Fail("invalid_input", "invalid reported model")
		}
		seen[model.ID] = true
		efforts := map[string]bool{}
		for _, effort := range model.ReasoningEfforts {
			if !effortIdentifier.MatchString(effort) || efforts[effort] {
				return Fail("invalid_input", "invalid reported reasoning effort")
			}
			efforts[effort] = true
		}
		if model.DefaultReasoningEffort != "" && !efforts[model.DefaultReasoningEffort] {
			return Fail("invalid_input", "default effort is not reported as supported")
		}
	}
	return nil
}

func ValidateExecutionOptions(capability Capability, model, effort string) error {
	if model == "" && effort == "" {
		return nil
	}
	if capability.ModelDiscovery != "reported" || !capability.Available {
		return Fail("invalid_input", "tool has no current model catalog")
	}
	for _, option := range capability.Models {
		if option.ID != model {
			continue
		}
		if effort == "" {
			return nil
		}
		for _, supported := range option.ReasoningEfforts {
			if supported == effort {
				return nil
			}
		}
		return Fail("invalid_input", "reasoning effort is not supported by the selected model")
	}
	return Fail("invalid_input", "model is not reported by this tool")
}
