package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestConsultationEncodedBoundAndHonestOmissions(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	a, _ := chatFixture(t, f)
	// Legal controls and HTML escapes expand sixfold during JSON encoding.
	large := strings.Repeat("<\x01", 8<<10)
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		for i := 0; i < 25; i++ {
			id := ID("req_")
			if e := tx.Put("requirement", id, domain.Requirement{ID: id, Title: large, Body: large, AssistantSummary: large}); e != nil {
				return e
			}
			id = ID("task_")
			if e := tx.Put("task", id, domain.Task{ID: id, Title: large, Description: large, Acceptance: large}); e != nil {
				return e
			}
		}
		return tx.Put("leader_message", "private", domain.LeaderMessage{ID: "private", Text: "PRIVATE_PLANNING_HISTORY"})
	}); e != nil {
		t.Fatal(e)
	}
	q := consultTest(t, f, a, 0, "normal-short", "consultation")
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		if len(prompt) > consultationPromptLimit || !strings.Contains(prompt, q.OriginalText) || strings.Contains(prompt, "PRIVATE_PLANNING_HISTORY") {
			t.Error("oversized/private/missing prompt")
		}
		var input struct {
			Omitted map[string]int `json:"omitted"`
		}
		if e := json.Unmarshal([]byte(strings.TrimPrefix(prompt, consultationInstruction)), &input); e != nil {
			t.Fatal(e)
		}
		if input.Omitted["tasks"] == 0 || input.Omitted["requirements"] == 0 {
			t.Error("omissions not disclosed")
		}
		return PlanningOutcome{TreeStopped: true, Output: `{"reply":"short consultation remains runnable"}`}, nil
	})}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if got := getConversation(t, f, q.ID); got.Status != "succeeded" {
		t.Fatal("avoidable retry/failed short consult", got)
	}
}
func TestMachinePathRedactionPreservesRepositoryPathsRoutesAndURLs(t *testing.T) {
	for _, v := range []string{"src/api/routes.go", "/api/v1/tasks", "https://example.com/api", "https://example.com/Users/demo", "change web/src/app.tsx and /api/new", "space src/api.go"} {
		if redacted(v) != v {
			t.Fatal("useful text damaged", v, redacted(v))
		}
	}
	for _, v := range []string{`/Users/alice/.config/token`, `here /home/alice/project/file`, `C:\Users\Alice\settings.json`, `D:/secret/file`, `\\server\share\settings`, `/private/tmp/file`, `/var/lib/coordica/settings`} {
		safe := redacted(v)
		if !strings.Contains(safe, "[local path redacted]") {
			t.Fatal("machine path exposed", v, safe)
		}
	}
	safe := redacted(strings.Repeat("中", 7000))
	if !utf8.ValidString(safe) || len(safe) > 16<<10 {
		t.Fatal("invalid truncated UTF-8")
	}
}
func TestLateDurableStoppedReceiptOnlyReleasesBarrierExactlyOnce(t *testing.T) {
	for _, purpose := range []string{"consultation", "evaluation"} {
		t.Run(purpose, func(t *testing.T) {
			var f *serviceFixture
			var q domain.ConversationRequest
			if purpose == "consultation" {
				f = newServiceFixture(t)
				a, _ := chatFixture(t, f)
				q = consultTest(t, f, a, 0, "late", "consultation")
			} else {
				var d domain.PlanningDocument
				f, d, _ = planningFixture(t, true)
				scheduleTest(t, f, d, "late")
			}
			ctx := context.Background()
			w := &PlanningWorker{Service: f.s}
			r, _, _, e := w.reserve(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = w.record(ctx, r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: false, ElapsedSeconds: 1}); e != nil {
				t.Fatal(e)
			}
			// Consultation currently aggregates the Run barrier without creating
			// a Decision. Also cover historical consultation stop decisions.
			unknown := f.snapshot(t).PlanningRuns[0]
			matchingID := ""
			for _, decision := range f.snapshot(t).Decisions {
				if decision.SourceID == r.ID && decision.Kind == "unknown_live" {
					matchingID = decision.ID
				}
			}
			if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
				if matchingID == "" {
					matchingID = "decision_consult_stop"
					if e := tx.Put("decision", matchingID, domain.Decision{ID: matchingID, Version: 1, SourceKind: "planning_run", SourceID: r.ID, SourceVersion: unknown.Version, Kind: "unknown_live", Status: "open", Evidence: "original stop uncertainty"}); e != nil {
						return e
					}
				}
				for _, decision := range []domain.Decision{
					{ID: "decision_other_run", SourceKind: "planning_run", SourceID: "plan_other", SourceVersion: unknown.Version, Kind: "unknown_live"},
					{ID: "decision_other_version", SourceKind: "planning_run", SourceID: r.ID, SourceVersion: unknown.Version + 1, Kind: "unknown_live"},
					{ID: "decision_other_kind", SourceKind: "planning_run", SourceID: r.ID, SourceVersion: unknown.Version, Kind: "budget"},
					{ID: "decision_other_source", SourceKind: "task", SourceID: r.ID, SourceVersion: unknown.Version, Kind: "unknown_live"},
				} {
					decision.Version, decision.Status = 1, "open"
					if e := tx.Put("decision", decision.ID, decision); e != nil {
						return e
					}
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			before := f.snapshot(t)
			unbound := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true}
			if e = w.record(ctx, r, unbound); e == nil {
				t.Fatal("unbound late stop accepted")
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("unbound stop changed state")
			}
			if before.Project.UsedSeconds != r.GrantSeconds || before.Project.ReservedSeconds != 0 {
				t.Fatal("unknown tree was refunded")
			}
			out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, FinishedAt: r.StartedAt.Add(time.Second), TreeStopped: true, ElapsedSeconds: 1, Output: `{"reply":"never adopt late output"}`}
			bindPlanningOutcome(&out, r)
			if e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700); e != nil {
				t.Fatal(e)
			}
			for _, mismatch := range []string{"run", "purpose", "account", "request", "generation", "document", "authorization"} {
				bad := out
				switch mismatch {
				case "run":
					bad.RunID = "other"
				case "purpose":
					bad.Purpose = "other"
				case "account":
					bad.AccountingTarget = "other"
				case "request":
					bad.ConversationID = "other"
				case "generation":
					bad.Generation++
				case "document":
					bad.DocumentVersion++
				case "authorization":
					bad.AuthorizationVersion++
				}
				raw, _ := json.Marshal(bad)
				if e = os.WriteFile(w.receipt(r.ID), raw, 0600); e != nil {
					t.Fatal(e)
				}
				if e = w.Recover(ctx); e != nil {
					t.Fatal(e)
				}
				after := f.snapshot(t)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("mismatched receipt changed durable barrier", mismatch)
				}
			}
			raw, _ := json.Marshal(out)
			if e = os.WriteFile(w.receipt(r.ID), raw, 0600); e != nil {
				t.Fatal(e)
			}
			if e = w.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			if after.PlanningRuns[0].State != "stopped" || !after.PlanningRuns[0].TreeStopped || after.Project.UsedSeconds != before.Project.UsedSeconds || after.Project.ReservedSeconds != 0 || len(after.Tasks) != len(before.Tasks) {
				t.Fatal("late result adopted/refunded or barrier stuck")
			}
			for _, decision := range after.Decisions {
				if decision.ID == matchingID {
					if decision.Status != "resolved" || !strings.Contains(decision.Resolution, "same-Run") {
						t.Fatal("original decision left open", decision)
					}
				} else if decision.Status != "open" {
					t.Fatal("unrelated decision changed", decision)
				}
			}
			for _, block := range after.Blocks {
				if block.ObjectID == matchingID || block.ObjectID == r.ID {
					t.Fatal("false stop barrier remains", block)
				}
			}
			foundAudit := false
			for _, event := range after.Events {
				if event.Kind == "decision.stop_reconciled" && event.ObjectID == matchingID {
					foundAudit = true
				}
			}
			if !foundAudit {
				t.Fatal("no reconciliation audit")
			}
			if purpose == "consultation" && getConversation(t, f, q.ID).Reply != "" {
				t.Fatal("adopted late advice")
			}
			if e = w.Recover(ctx); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(after, f.snapshot(t)) {
				t.Fatal("duplicate receipt changed accounting/state")
			}
		})
	}
}
