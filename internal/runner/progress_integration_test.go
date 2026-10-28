package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/domain"
)

// Real Git, Runner, authenticated HTTP and application transactions. Execution
// intent is a local fixture, not a live-model or operating-system claim.
func TestProgressRunnerProducesFixedCandidateWithoutDeadline(t *testing.T) {
	f := newIntentFixtureWithPolicy(t, false, true)
	if !domain.ProgressManaged(f.record.Assignment.Run) {
		t.Fatal("missing progress-managed assignment")
	}
	f.r.execute = func(_ context.Context, request agent.Request, onEvent func(agent.Event)) (agent.Result, error) {
		onEvent(agent.Event{Type: "tool_execution_start"})
		if err := os.WriteFile(filepath.Join(request.WorkDir, "test/greeting.test.mjs"), []byte("// fixed implementation fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		onEvent(agent.Event{Type: "tool_execution_end"})
		return agent.Result{TreeStopped: true, OutputText: `{"version":1,"status":"completed","requested_paths":[],"reason":"","summary":"Actual scoped change"}`}, nil
	}
	f.perform(context.Background())
	snapshot := f.snapshot(t)
	if len(snapshot.Candidates) != 1 || f.artifactCalls != 1 || snapshot.Runs[0].State != "succeeded" ||
		snapshot.Tasks[0].ReservedSeconds != 0 || snapshot.Project.ReservedSeconds != 0 {
		t.Fatal("deadline-free handoff failed", snapshot.Runs, snapshot.Tasks)
	}
}
