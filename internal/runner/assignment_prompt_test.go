package runner

import (
	"strings"
	"testing"
	"time"

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

func TestAssignmentDisclosesActualRunWindowSeparatelyFromTaskAllowance(t *testing.T) {
	for _, kind := range []string{"implement", "review"} {
		a := domain.Assignment{
			Run:  domain.Run{Kind: kind, GrantSeconds: 1300, Deadline: time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)},
			Task: domain.Task{BudgetSeconds: 3600, UsedSeconds: 1300, ReservedSeconds: 1300},
		}
		prompt := assignmentPrompt(a, "runner")
		for _, fact := range []string{"Run grant: 1300 seconds", "2026-10-26T08:00:00Z", "Task cumulative allowance: 3600 seconds", "used at assignment: 1300", "later review, checks and repairs"} {
			if !strings.Contains(prompt, fact) {
				t.Fatalf("%s omitted execution-window fact %q", kind, fact)
			}
		}
		if strings.Contains(prompt, "Run grant: 3600") || a.Task.UsedSeconds != 1300 {
			t.Fatal("task allowance became the Run window or accounting was mutated")
		}
	}
}

func TestReviewContextDistinguishesEvidenceAuthorFromReviewer(t *testing.T) {
	a := domain.Assignment{
		Run: domain.Run{ID: "run_independent_review", Kind: "review", Generation: 3, AuthorizationVersion: 2},
		Candidate: &domain.Candidate{ID: "candidate_fixed", SourceRunID: "run_measured_implementation",
			Main: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40)},
	}
	prompt := assignmentPrompt(a, "runner")
	for _, fact := range []string{
		"Coordica Run run_independent_review; generation 3; authorization 2",
		`Fixed input candidate ID: "candidate_fixed"; candidate handoff producer Run: "run_measured_implementation"`,
		"it need not be the software implementation author", "Task description:", "Task acceptance:",
		"an author cannot embed those future identities", "Do not demand a self-referential commit or candidate ID",
		"do not require those IDs or generations to equal a later review or repair Run", "request missing evidence", "run_id (empty)",
	} {
		if !strings.Contains(prompt, fact) {
			t.Fatalf("review conflates author/reviewer provenance or loses the result contract: missing %q", fact)
		}
	}
	a.Candidate.SourceRunID = ""
	if !strings.Contains(assignmentPrompt(a, "runner"), "An empty source identity is unknown") {
		t.Fatal("legacy missing provenance became an inferred author")
	}
	a.Run.Kind = "implement"
	a.Candidate.SourceRunID = "run_measured_implementation"
	if prompt := assignmentPrompt(a, "runner"); !strings.Contains(prompt, "run_measured_implementation") ||
		!strings.Contains(prompt, "Preserve historical measurement authors") || strings.Contains(prompt, "You are an independent reviewer") {
		t.Fatal("repair lost historical authorship or acquired reviewer authority")
	}
}
