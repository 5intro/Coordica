package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestFailureRecoveryLargeEvidenceIsCompletePrivateAndReclaimed(t *testing.T) {
	f, source := failureFixture(t)
	large := strings.Repeat("failure evidence · ", 20_000) + "END-OF-FIXED-EVIDENCE"
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var run domain.Run
		if err := tx.Get("run", source.SourceRunID, &run); err != nil {
			return err
		}
		run.Detail = large
		run.Version++
		source.SourceRunVersion = run.Version
		if err := tx.Put("run", run.ID, run); err != nil {
			return err
		}
		return tx.Put("failure_assessment", source.ID, source)
	}); err != nil {
		t.Fatal(err)
	}
	var path string
	executor := planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
		if len(prompt) > leaderPromptLimit || !strings.Contains(prompt, "failure-context.json") {
			t.Fatal("large source was truncated or inline", len(prompt))
		}
		path = filepath.Join(dir, "failure-context.json")
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), large) || !strings.Contains(prompt, Hash(string(raw))) {
			t.Fatal("incomplete/unbound source", err)
		}
		out, _ := json.Marshal(domain.FailureRecoveryPlan{Category: "unknown", Reason: "No safe automatic change established", Action: "needs_owner", Tasks: []domain.TaskProposal{}})
		return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2, Output: string(out)}, nil
	})
	if err := (&PlanningWorker{Service: f.s, Executor: executor}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("read-only diagnosis did not execute")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("stopped diagnosis directory retained", err)
	}
}
