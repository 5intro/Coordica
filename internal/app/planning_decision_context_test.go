package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/store"
)

func TestPlanningContextCarriesOwnerDecisionReceiptWithoutGrantingAuthority(t *testing.T) {
	f, document, result := planningFixture(t, true)
	ctx := context.Background()
	result.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "architecture", Evidence: "Existing rule needs confirmation", Actions: []string{"retain"}}}
	scheduleTest(t, f, document, "first")
	worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	decision := f.snapshot(t).Decisions[0]
	evidence := strings.Repeat("EXACT_OWNER_RECEIPT中", 1000)
	_, err := f.s.ResolveDecision(ctx, f.owner, "resolve", decision.ID, domain.DecisionResolveRequest{
		ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "retain", Evidence: evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.snapshot(t).PlanningDocument.Status == "admitted" {
		t.Fatal("decision receipt silently granted goal authority")
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		other := decision
		other.ID, other.DocumentVersion, other.Evidence = "other-goal", document.Version+1, "UNRELATED_GOAL"
		if err := tx.Put("decision", other.ID, other); err != nil {
			return err
		}
		other = decision
		other.ID, other.AuthorizationVersion, other.Evidence = "other-authorization", document.AuthorizationVersion+1, "UNRELATED_AUTH"
		return tx.Put("decision", other.ID, other)
	}); err != nil {
		t.Fatal(err)
	}
	scheduleTest(t, f, document, "second")
	worker.Executor = planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
		_, raw, _ := strings.Cut(prompt, " Input: ")
		var input struct {
			Decisions []struct{ ID, Status string } `json:"decision_index"`
		}
		if json.Unmarshal([]byte(raw), &input) != nil || len(input.Decisions) != 1 ||
			input.Decisions[0].ID != decision.ID || input.Decisions[0].Status != "resolved" {
			t.Fatal("current Owner decision receipt missing from the planning index")
		}
		path := filepath.Join(dir, "context", "decisions.jsonl")
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "retain: "+evidence) ||
			strings.Contains(string(data), "UNRELATED_") || strings.Contains(prompt, "EXACT_OWNER_RECEIPT") {
			t.Fatal("receipt was lost, truncated, leaked across goals, or injected into the prompt", err)
		}
		if err := localfile.CheckPrivate(path); err != nil {
			t.Fatal(err)
		}
		result.Tasks, result.MajorDecisions = []domain.TaskProposal{}, []domain.MajorDecisionProposal{}
		result.Ready = false
		data, _ = json.Marshal(result)
		return PlanningOutcome{Output: string(data), TreeStopped: true}, nil
	})
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
}
