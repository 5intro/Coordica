package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"coordica/internal/agent"
	"coordica/internal/domain"
	"coordica/internal/store"
)

// Opt-in acceptance: a real native CLI must retrieve prose available only in
// its private JSONL material. No center, daemon or project execution is started.
func TestInstalledCodexReadsLayeredPlanningMaterial(t *testing.T) {
	executable := os.Getenv("COORDICA_TEST_CONTEXT_CODEX")
	if executable == "" {
		t.Skip("native Codex material acceptance is opt-in")
	}
	f, d, _ := planningFixture(t, true)
	evidence := "material-evidence-" + ID("")
	task := f.task
	task.Description = evidence
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error { return tx.Put("task", task.ID, task) }); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "cmd", "coordica", "leader.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(t.TempDir(), "leader.schema.json")
	if err := os.WriteFile(schemaPath, schema, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := f.s.SchedulePlanning(context.Background(), f.owner, "native-material", domain.PlanningMessageRequest{ExpectedVersion: d.Version, Text: "Read the full task record for " + task.ID + " from the context/tasks.jsonl material. Return reply exactly equal to that record's description, ready=false, and all other arrays empty. Do not implement, execute project code, create tasks, or change files."})
	_ = decodeTest[domain.PlanningRun](t, raw, err)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	var nativeErr error
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(ctx context.Context, dir, prompt string) (PlanningOutcome, error) {
		started := time.Now()
		result, err := agent.Execute(ctx, agent.Request{Tool: "codex", Executable: executable, Model: "gpt-6.1-sol", Role: "leader", WorkDir: dir, Prompt: prompt, OutputSchemaPath: schemaPath}, func(event agent.Event) {
			var info struct {
				Message string
				Item    struct{ Type string }
			}
			_ = json.Unmarshal(event.Data, &info)
			if info.Item.Type != "reasoning" {
				t.Logf("native event=%s item=%s", event.Type, info.Item.Type)
			}
			if event.Type == "error" {
				t.Logf("native connection diagnostic: %s", info.Message)
			}
		})
		nativeErr = err
		out := PlanningOutcome{Output: result.OutputText, TreeStopped: result.TreeStopped, ElapsedSeconds: int64(time.Since(started)/time.Second) + 1}
		if err != nil {
			out.Error = err.Error()
		}
		return out, err
	})}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	if nativeErr != nil {
		t.Fatal("native CLI failed", nativeErr)
	}
	if snapshot.PlanningDocument.LeaderResult == nil || snapshot.PlanningDocument.LeaderResult.Reply != evidence {
		t.Fatal("native CLI did not retrieve exact deferred evidence")
	}
	if len(snapshot.Tasks) != 1 {
		t.Fatal("read-only acceptance created work")
	}
}
