package domain

import (
	"strings"
	"testing"
)

func TestTaskAdmissionRejectsDuplicateAndEmptyRequirementBindings(t *testing.T) {
	task := Task{Title: "Work", Description: "Change", Acceptance: "Check", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 10, MaxRunSeconds: 10}
	for _, ids := range [][]string{nil, {"req_a"}, {"req_a", "req_b"}} {
		task.RequirementIDs = ids
		if err := ValidateTask(task); err != nil {
			t.Fatal("valid bindings rejected", ids, err)
		}
	}
	for _, ids := range [][]string{{"req_a", "req_a"}, {"req_a", ""}, {" "}} {
		task.RequirementIDs = ids
		if err := ValidateTask(task); err == nil {
			t.Fatal("invalid bindings admitted", ids)
		}
	}
}

func TestAssignmentRequirementContractRejectsMissingSwappedAndStaleContent(t *testing.T) {
	valid := Assignment{Task: Task{RequirementIDs: []string{"req_a"}}, Run: Run{RequirementRevisions: map[string]int64{"req_a": 2}}, Requirements: []RequirementContext{{ID: "req_a", ContentRevision: 2, Title: "Title", Body: "Full acceptance"}}}
	if err := ValidateAssignmentRequirements(valid); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing", "unrelated", "stale", "duplicate", "body", "extra-binding"} {
		t.Run(kind, func(t *testing.T) {
			a := valid
			a.Requirements = append([]RequirementContext{}, valid.Requirements...)
			switch kind {
			case "missing":
				a.Requirements = nil
			case "unrelated":
				a.Requirements[0].ID = "req_b"
			case "stale":
				a.Requirements[0].ContentRevision = 1
			case "duplicate":
				a.Requirements = append(a.Requirements, a.Requirements[0])
			case "body":
				a.Requirements[0].Body = ""
			default:
				a.Run.RequirementRevisions = map[string]int64{"req_a": 2, "req_b": 1}
			}
			if err := ValidateAssignmentRequirements(a); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestAssignmentContextBoundsActualEncodedContractWithoutTruncation(t *testing.T) {
	a := Assignment{Task: Task{RequirementIDs: []string{"req"}}, Requirements: []RequirementContext{{ID: "req", ContentRevision: 1, Title: "Title", Body: strings.Repeat("<\x01", 30<<10)}}}
	if err := ValidateAssignmentRequirements(a); err != nil {
		t.Fatal("fixture body must be legal", err)
	}
	original := a.Requirements[0].Body
	if err := ValidateAssignmentContext(a); err == nil {
		t.Fatal("JSON expansion escaped context bound")
	}
	if a.Requirements[0].Body != original {
		t.Fatal("fixed body truncated")
	}
}
