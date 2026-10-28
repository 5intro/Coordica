package domain

import (
	"testing"
	"time"
)

func TestOnlyVersionedDevelopmentMayOmitDeadline(t *testing.T) {
	now := time.Now()
	run := Run{ProgressVersion: ProgressVersion, Kind: "implement"}
	if !ProgressManaged(run) || !RunTimeValid(run, now) {
		t.Fatal("progress contract rejected")
	}
	for _, change := range []func(*Run){
		func(r *Run) { r.Kind = "review" },
		func(r *Run) { r.ProgressVersion = 0 },
		func(r *Run) { r.ProgressVersion = 2 },
		func(r *Run) { r.GrantSeconds = 30 },
		func(r *Run) { r.Deadline = now.Add(-time.Second) },
	} {
		invalid := run
		change(&invalid)
		if RunTimeValid(invalid, now) {
			t.Fatal("invalid omission granted unbounded execution", invalid)
		}
	}
}

func TestProgressDefaultsAndBoundedCounters(t *testing.T) {
	settings := DefaultAutomationSettings()
	if settings.EnforceTimeLimits || settings.ProgressCheckSeconds != 1800 || settings.PlanningBudgetSeconds != 0 {
		t.Fatal("unexpected default hard limit", settings)
	}
	for _, v := range []ProgressObservation{{}, {Sequence: -1}, {Sequence: 1, ToolCompleted: -1}, {Sequence: 1, Messages: 1 << 53}} {
		if ValidateProgress(v) == nil {
			t.Fatal("unbounded progress accepted", v)
		}
	}
	if err := ValidateTask(Task{Title: "Task", Description: "Work", Acceptance: "Checks pass", Tool: "codex", AllowedPaths: []string{"src/**"}}); err != nil {
		t.Fatal("optional time limits required", err)
	}
}
