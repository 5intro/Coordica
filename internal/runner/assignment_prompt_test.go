package runner

import (
	"strings"
	"testing"

	"coordica/internal/codequality"
	"coordica/internal/domain"
)

func TestEveryAssignmentReceivesStructuralAcceptance(t *testing.T) {
	for _, kind := range []string{"implement", "review"} {
		for _, mode := range []string{"runner", "agent"} {
			a := repairEvidenceAssignment()
			a.Run = domain.Run{Kind: kind, Base: strings.Repeat("a", 40)}
			prompt := assignmentPrompt(a, mode)
			if !strings.Contains(prompt, codequality.Instructions) {
				t.Fatalf("missing mandatory policy: %s/%s", kind, mode)
			}
			if kind == "review" && !strings.Contains(prompt, "Request changes on a violation") {
				t.Fatal("review may approve structure violations")
			}
			if strings.Contains(prompt, "EXPECTED_FAILURE_SENTINEL") != (kind == "implement") {
				t.Fatal("quality policy displaced repair evidence or exposed it to independent review")
			}
		}
	}
}
