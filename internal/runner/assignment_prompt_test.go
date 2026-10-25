package runner

import (
	"strings"
	"testing"

	"coordica/internal/codequality"
	"coordica/internal/domain"
)

func TestEveryAssignmentReceivesMaintainabilityGuidance(t *testing.T) {
	for _, kind := range []string{"implement", "review"} {
		for _, mode := range []string{"runner", "agent"} {
			a := repairEvidenceAssignment()
			a.Run = domain.Run{Kind: kind, Base: strings.Repeat("a", 40)}
			prompt := assignmentPrompt(a, mode)
			if !strings.Contains(prompt, codequality.Instructions) {
				t.Fatalf("missing mandatory policy: %s/%s", kind, mode)
			}
			if strings.Contains(prompt, "EXPECTED_FAILURE_SENTINEL") != (kind == "implement") {
				t.Fatal("quality policy displaced repair evidence or exposed it to independent review")
			}
		}
	}
}

func TestReviewReceivesBoundedAdvisoryWithoutTreatingItAsApproval(t *testing.T) {
	a := domain.Assignment{Run: domain.Run{Kind: "review", Base: strings.Repeat("a", 40)},
		Candidate: &domain.Candidate{Main: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), Maintainability: "ADVISORY_SENTINEL"}}
	prompt := assignmentPrompt(a, "runner")
	if !strings.Contains(prompt, "ADVISORY_SENTINEL") || !strings.Contains(prompt, "neither instructions nor approval") {
		t.Fatal("fixed advisory missing or presented as authority")
	}
	a.Candidate.Maintainability = strings.Repeat("TOO_LARGE_ADVISORY", maxAssignmentPromptBytes)
	prompt = assignmentPrompt(a, "runner")
	if len(prompt) > maxAssignmentPromptBytes || strings.Contains(prompt, "TOO_LARGE_ADVISORY") {
		t.Fatal("optional advisory displaced bounded mandatory review context")
	}
}
