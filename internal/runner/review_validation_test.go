package runner

import (
	"encoding/json"
	"slices"
	"testing"

	"coordica/internal/domain"
)

func TestReviewContractSchemaAndAssignedIssueValidation(t *testing.T) {
	a := domain.Assignment{Task: domain.Task{ID: "task_one"}, Run: domain.Run{Base: "candidate", ReviewContractVersion: 1, ReviewRulesDigest: "digest"},
		ReviewContext: &domain.ReviewContext{Issues: []domain.ReviewIssue{{ID: "issue_one", TaskID: "task_one", Version: 3, Source: "internal", State: "open", Finding: domain.ReviewFinding{Key: "retry"}}}}}
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(assignedReviewSchema(a), &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rules_digest", "issues", "resolutions"} {
		if !slices.Contains(schema.Required, name) || schema.Properties[name] == nil {
			t.Fatal("structured fields not required", schema)
		}
	}
	v := domain.Review{Verdict: "approve", RulesDigest: "digest", Findings: []string{}, Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{}}
	if validateAssignedReview(a, v) == nil {
		t.Fatal("missing resolution entered durable spool")
	}
	v.Resolutions = []domain.ReviewResolution{{IssueID: "issue_one", ExpectedVersion: 3, Disposition: "fixed", Reason: "retry regression confirms exactly one write"}}
	if err := validateAssignedReview(a, v); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"stale", "foreign", "external", "reopened"} {
		t.Run(mode, func(t *testing.T) {
			copy := a
			copy.ReviewContext = &domain.ReviewContext{Issues: slices.Clone(a.ReviewContext.Issues)}
			switch mode {
			case "stale":
				copy.ReviewContext.Issues[0].Version++
			case "foreign":
				copy.ReviewContext.Issues[0].TaskID = "task_other"
			case "external":
				copy.ReviewContext.Issues[0].Source = "external/codex"
			case "reopened":
				v.Verdict = "changes_requested"
				v.Issues = []domain.ReviewFinding{{Key: "retry", Severity: "P1", Title: "duplicate", Trigger: "retry", Evidence: "two rows", Impact: "data loss"}}
			}
			if validateAssignedReview(copy, v) == nil {
				t.Fatal("invalid resolution accepted")
			}
		})
	}
}
