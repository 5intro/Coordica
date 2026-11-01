package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestReviewPromptAndSchemaUseOneNegotiatedContract(t *testing.T) {
	for _, structured := range []bool{false, true} {
		a := domain.Assignment{Run: domain.Run{Kind: "review", Base: "fixed_candidate", ReviewRulesDigest: "fixed_rules"}}
		if structured {
			a.Run.ReviewContractVersion = domain.ReviewContractVersion
		}
		prompt := assignmentPrompt(a, "agent")
		legacy := strings.Contains(prompt, "findings (array of strings)")
		if legacy == structured {
			t.Fatal("prompt mixes legacy and structured instructions", prompt)
		}
		if structured {
			for _, field := range []string{"findings (must be [])", `rules_digest (must be "fixed_rules")`, "issues (array of structured findings)", "resolutions (array of structured issue resolutions)"} {
				if !strings.Contains(prompt, field) {
					t.Fatal("missing primary output field", field)
				}
			}
		}
		var schema struct {
			Properties map[string]map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(assignedReviewSchema(a), &schema); err != nil {
			t.Fatal(err)
		}
		_, limited := schema.Properties["findings"]["maxItems"]
		if limited != structured {
			t.Fatal("negotiated schema changed legacy behavior")
		}
	}
}
