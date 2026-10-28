package runner

import (
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestReviewConvergesOnEvidenceWithoutInheritingApproval(t *testing.T) {
	a := domain.Assignment{Run: domain.Run{Kind: "review", Base: strings.Repeat("a", 40)},
		ReviewContext: &domain.ReviewContext{Round: 3, PreviousCandidate: strings.Repeat("b", 40), Findings: []string{"OLD_FINDING_SENTINEL"}}}
	prompt := assignmentPrompt(a, "runner")
	for _, want := range []string{"REVIEW.md", "file/line or missing artifact", "concrete trigger", "Drop style-only nits",
		"do not repeat a resolved finding", "complete current diff", "Never inherit an old approval", "OLD_FINDING_SENTINEL",
		"Repeated review must converge", "raw artifacts", "Do not invent new criteria"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing convergence/safety rule %q", want)
		}
	}
}
