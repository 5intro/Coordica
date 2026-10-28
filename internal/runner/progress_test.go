package runner

import (
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

func TestProgressObserverIntervalAndNoSyntheticActivity(t *testing.T) {
	start := time.Now()
	run := domain.Run{Kind: "implement", ProgressVersion: domain.ProgressVersion, ProgressCheckSeconds: 60}
	observer := newProgressObserver(run, start)
	if observer.snapshot(start.Add(59*time.Second)) != nil {
		t.Fatal("early observation")
	}
	quiet := observer.snapshot(start.Add(time.Minute))
	if quiet.Sequence != 1 || quiet.Messages != 0 || quiet.ToolStarted != 0 {
		t.Fatal("heartbeat invented progress", quiet)
	}
	observer.record(agent.Event{Type: "tool_execution_start"})
	observer.record(agent.Event{Type: "tool_execution_end"})
	observed := observer.snapshot(start.Add(2 * time.Minute))
	if observed.Sequence != 2 || observed.ToolStarted != 1 || observed.ToolCompleted != 1 {
		t.Fatal(observed)
	}
	if newProgressObserver(domain.Run{Kind: "review"}, start) != nil {
		t.Fatal("finite review replaced")
	}
}

func TestProgressPermissionWindowHasNoLifetimeButRetainsLeaseAndSilence(t *testing.T) {
	start := time.Now()
	window := permissionWindow{lease: start.Add(45 * time.Second), lastSuccess: start}
	if !window.until().Equal(start.Add(30 * time.Second)) {
		t.Fatal("silence fence lost", window.until())
	}
	window.renew(start.Add(3*time.Hour), start.Add(2*time.Hour))
	if !window.until().Equal(start.Add(2*time.Hour + 30*time.Second)) {
		t.Fatal("zero deadline killed renewal")
	}
	window.deadline = start.Add(time.Hour)
	if !window.until().Equal(window.deadline) {
		t.Fatal("finite operation deadline weakened")
	}
}

func TestProgressAssignmentDoesNotTellAgentToRaceAnAllowance(t *testing.T) {
	a := domain.Assignment{Run: domain.Run{Kind: "implement", ProgressVersion: 1, ProgressCheckSeconds: 1800},
		Task: domain.Task{BudgetSeconds: 14400, MaxRunSeconds: 3600}}
	prompt := assignmentPrompt(a, "runner")
	if !strings.Contains(prompt, "no fixed duration") || !strings.Contains(prompt, "1800 seconds") ||
		strings.Contains(prompt, "Run grant:") || strings.Contains(prompt, "Task cumulative allowance:") {
		t.Fatal("progress-managed assignment disclosed obsolete deadline")
	}
}
