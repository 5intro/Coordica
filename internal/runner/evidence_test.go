package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

func TestPrivateEvidenceLimitRetainsFinalResultAndMarksOmittedEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "事件 with spaces.jsonl")
	e, err := newEvidence(path, "fixture-node-secret")
	if err != nil {
		t.Fatal(err)
	}
	chunk, _ := json.Marshal(map[string]string{"text": strings.Repeat("structured text fixture-node-secret ", 6000)})
	for i := 0; i < 30; i++ {
		e.append(agent.Event{Type: "agent_message", Data: chunk}, false)
	}
	e.value("runner.result", map[string]string{"output_text": "final fixture-node-secret"}, true)
	if err := e.close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxEvidenceBytes || strings.Contains(string(raw), "fixture-node-secret") {
		t.Fatal("log limit or redaction failed")
	}
	if !strings.Contains(string(raw), "runner.log_truncated") || !strings.Contains(string(raw), "runner.result") || !strings.Contains(string(raw), "final [redacted]") {
		t.Fatal("truncation marker or final output missing")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatal("partial protocol event stored")
		}
	}
	if stat, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0) {
		t.Fatal("private evidence permissions", err)
	}
}

func TestAssignmentPromptIncludesAuthorizedGoalConstraints(t *testing.T) {
	a := domain.Assignment{Run: domain.Run{ID: "run", Kind: "implement"}, Task: domain.Task{Title: "Task", Description: "Build", Acceptance: "Task check", AllowedPaths: []string{"src/**"}}, GoalContext: &domain.GoalContext{Version: 2, AuthorizationVersion: 3, Objective: "Owner objective", NonGoals: "Keep storage local", Acceptance: "Owner acceptance", Architecture: "Go and SQLite", DirectoryOutline: "src/: implementation", AllowedRoots: []string{"src"}}}
	prompt := assignmentPrompt(a, "runner")
	for _, text := range []string{"Owner objective", "Keep storage local", "Owner acceptance", "Go and SQLite", "src/: implementation", "Every change must satisfy task scope and goal roots"} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("assigned execution lost constraint %q", text)
		}
	}
}
