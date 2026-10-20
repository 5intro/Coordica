package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/store"
)

func TestPlanningContextDefersProseAndKeepsContractsAndExactMaterials(t *testing.T) {
	f, d, result := planningFixture(t, true)
	ctx := context.Background()
	detail := strings.Repeat("LONG_TASK_DETAIL中", 12000)
	oldOutput := strings.Repeat("OLD_LEADER_OUTPUT", 20000)
	task := f.task
	task.Description, task.Acceptance = detail, "EXACT_TASK_ACCEPTANCE"
	task.UsedSeconds, task.ReservedSeconds, task.Repairs, task.Retries = 7, 9, 1, 1
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		if err := tx.Put("task", task.ID, task); err != nil {
			return err
		}
		d.LeaderResult = &domain.LeaderResult{Reply: oldOutput}
		d.LeaderSession = "PRIVATE_NATIVE_SESSION"
		if err := tx.Put("planning_document", "current", d); err != nil {
			return err
		}
		return tx.Put("leader_message", "other-version", domain.LeaderMessage{ID: "other-version", DocumentVersion: d.Version + 1, Text: "OTHER_VERSION_HISTORY"})
	}); err != nil {
		t.Fatal(err)
	}
	run := scheduleTest(t, f, d, "layered")
	observedDir := ""
	result.Tasks = []domain.TaskProposal{}
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, dir, prompt string) (PlanningOutcome, error) {
		observedDir = dir
		if len(prompt) > leaderPromptLimit || strings.Contains(prompt, "LONG_TASK_DETAIL") || strings.Contains(prompt, "OLD_LEADER_OUTPUT") || strings.Contains(prompt, "PRIVATE_NATIVE_SESSION") || strings.Contains(prompt, "OTHER_VERSION_HISTORY") {
			t.Fatal("unbounded or unrelated context injected")
		}
		_, raw, ok := strings.Cut(prompt, " Input: ")
		var input planningFacts
		if !ok || json.Unmarshal([]byte(raw), &input) != nil {
			t.Fatal("invalid facts")
		}
		if input.Baseline.Version != run.BaselineVersion || input.Baseline.Digest != run.BaselineDigest || input.Baseline.Baseline == nil || input.Baseline.Baseline.Conventions != "Fixture interfaces" || input.Document.NonGoals != d.NonGoals || !reflect.DeepEqual(input.Document.AllowedRoots, d.AllowedRoots) {
			t.Fatal("mandatory shared constraints were lost")
		}
		found := false
		for _, index := range input.TaskIndex {
			if index.ID == task.ID {
				found = true
				if index.Version != task.Version || index.AuthorizationVersion != task.AuthorizationVersion || index.UsedSeconds != 7 || index.ReservedSeconds != 9 || index.Repairs != 1 || index.Retries != 1 || !reflect.DeepEqual(index.AllowedPaths, task.AllowedPaths) {
					t.Fatal("index reset accounting, versions or paths")
				}
			}
		}
		if !found || len(input.Materials.Files) != 4 || input.Materials.RunID != run.ID || input.Materials.Generation != run.Generation {
			t.Fatal("incomplete manifest/index")
		}
		for _, file := range input.Materials.Files {
			path := filepath.Join(dir, filepath.FromSlash(file.Path))
			data, err := os.ReadFile(path)
			if err != nil || Hash(string(data)) != file.SHA256 || len(data) != file.Bytes || strings.Count(string(data), "\n") != file.Records {
				t.Fatal("material not exactly described", file, err)
			}
			if err := localfile.CheckPrivate(path); err != nil {
				t.Fatal(err)
			}
			if file.Path == "context/tasks.jsonl" && (!strings.Contains(string(data), detail) || !strings.Contains(string(data), task.Acceptance)) {
				t.Fatal("deferred task contract truncated")
			}
			if file.Path == "context/history.jsonl" && strings.Contains(string(data), "OTHER_VERSION_HISTORY") {
				t.Fatal("different goal history crossed boundary")
			}
		}
		out, _ := json.Marshal(result)
		return PlanningOutcome{TreeStopped: true, Output: string(out)}, nil
	})}
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if observedDir == "" {
		t.Fatal("executor was not reached")
	}
	if _, err := os.Stat(observedDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stopped Leader left context files behind", err)
	}
}

func TestLeaderHistoryCountsEncodedBytesAndPreservesUTF8AndLatestRequest(t *testing.T) {
	messages := []domain.LeaderMessage{}
	for i := 0; i < 20; i++ {
		messages = append(messages, domain.LeaderMessage{ID: ID("history_"), DocumentVersion: 2, Text: strings.Repeat("中<\x01", 6000)})
	}
	messages = append(messages, domain.LeaderMessage{ID: "latest", DocumentVersion: 2, Text: "Keep this request complete 中"})
	original := messages[0].Text
	got, info, err := selectLeaderHistory(messages)
	if err != nil || len(got) == 0 || info.Omitted == 0 || len(info.Truncated) == 0 || got[len(got)-1].Text != messages[len(messages)-1].Text {
		t.Fatal(got, info, err)
	}
	raw, _ := json.Marshal(got)
	if len(raw) > leaderHistoryLimit || messages[0].Text != original || info.Omitted != len(messages)-len(got) {
		t.Fatal("encoded history bound, source mutation or omission accounting")
	}
	for _, m := range got {
		if !utf8.ValidString(m.Text) {
			t.Fatal("split UTF-8 boundary")
		}
	}
	huge := domain.LeaderMessage{ID: "latest", Text: strings.Repeat("<\x01", 32<<10)}
	if out, _, err := selectLeaderHistory([]domain.LeaderMessage{huge}); err == nil || out != nil {
		t.Fatal("oversized current request silently clipped")
	}
}

func TestPlanningContextCannotTrimMandatoryContractsToFit(t *testing.T) {
	facts := planningFacts{Document: domain.PlanningDocument{Version: 1, NonGoals: strings.Repeat("MANDATORY", leaderPromptLimit/8)}}
	_, err := buildPlanningContext(t.TempDir(), domain.PlanningRun{}, facts, []domain.LeaderMessage{{DocumentVersion: 1, Text: "small request"}})
	var problem *domain.Error
	if !errors.As(err, &problem) || problem.Code != "context_limit" {
		t.Fatal("oversized rules were accepted or clipped", err)
	}
}

func TestPlanningMaterialRejectsStaleBindingsBeforeWriting(t *testing.T) {
	for _, kind := range []string{"goal", "baseline", "cancel", "requirement", "paused"} {
		t.Run(kind, func(t *testing.T) {
			f, d, _ := planningFixture(t, true)
			scheduleTest(t, f, d, "stale-material")
			w := &PlanningWorker{Service: f.s}
			r, doc, messages, err := w.reserve(context.Background())
			if err != nil || r.ID == "" {
				t.Fatal(err)
			}
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				switch kind {
				case "paused":
					p, e := getProject(tx)
					if e != nil {
						return e
					}
					p.Paused = true
					return tx.Put("project", p.ID, p)
				case "goal":
					doc.Version++
					return tx.Put("planning_document", "current", doc)
				case "baseline":
					v, e := initialization(tx)
					if e != nil {
						return e
					}
					v.ApprovedDigest = "changed"
					return tx.Put("initialization", "current", v)
				case "cancel":
					r.State = "cancel_requested"
					return tx.Put("planning_run", r.ID, r)
				default:
					var req domain.Requirement
					if e := tx.Get("requirement", d.RequirementIDs[0], &req); e != nil {
						return e
					}
					req.ContentRevision = 2
					return tx.Put("requirement", req.ID, req)
				}
			}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if _, err := w.planningPrompt(context.Background(), r, d, messages, dir); err == nil {
				t.Fatal("stale material exposed")
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Fatal("I/O happened before snapshot validation", entries, err)
			}
		})
	}
}

func TestLeaderMaterialBoundsDoNotLeaveTruncatedSuccess(t *testing.T) {
	data, err := encodeLeaderRows([]domain.LeaderMessage{{Text: strings.Repeat("x", leaderMaterialLimit)}})
	if err == nil || data != nil {
		t.Fatal("partial oversized material accepted")
	}
}

func TestRequirementContextDefersCompleteFixedImpactSnapshots(t *testing.T) {
	f, req, task := requirementFixture(t)
	ctx := context.Background()
	task.Description = strings.Repeat("FIXED_IMPACT_PROSE中", 20000)
	task.Acceptance = "FIXED_IMPACT_ACCEPTANCE"
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("task", task.ID, task) }); err != nil {
		t.Fatal(err)
	}
	change := reviseRequirement(t, f, req, "revise")
	if len(change.Impacts) != 1 {
		t.Fatal("missing impact fixture", change)
	}
	// The persisted impact is a historical contract, distinct from current work.
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Task
		if err := tx.Get("task", task.ID, &current); err != nil {
			return err
		}
		current.Description = "CURRENT_TASK_PROSE"
		return tx.Put("task", current.ID, current)
	}); err != nil {
		t.Fatal(err)
	}
	w := &PlanningWorker{Service: f.s}
	if err := w.queueRequirementChanges(ctx); err != nil {
		t.Fatal(err)
	}
	r, _, _, err := w.reserve(ctx)
	if err != nil || r.RequirementChangeID != change.ID {
		t.Fatal("change not reserved", r, err)
	}
	dir := t.TempDir()
	prompt, err := w.requirementPrompt(ctx, r, dir)
	if err != nil || len(prompt) > leaderPromptLimit || strings.Contains(prompt, "FIXED_IMPACT_PROSE") || strings.Contains(prompt, "CURRENT_TASK_PROSE") {
		t.Fatal("impact prose remained inline", err)
	}
	_, raw, ok := strings.Cut(prompt, " Input: ")
	var input struct {
		Change    requirementChangeContext `json:"change"`
		Materials leaderMaterials          `json:"materials"`
	}
	if !ok || json.Unmarshal([]byte(raw), &input) != nil {
		t.Fatal("invalid context")
	}
	index := input.Change.Impacts[0]
	want := change.Impacts[0]
	if len(input.Change.Impacts) != len(change.Impacts) || index.Task.ID != want.Task.ID || index.Task.Version != want.Task.Version || index.Task.BudgetSeconds != want.Task.BudgetSeconds || !reflect.DeepEqual(index.Task.AllowedPaths, want.Task.AllowedPaths) || index.Shared != want.Shared || index.NeedsStop != want.NeedsStop || index.Integrated != want.Integrated || index.PublicationBusy != want.PublicationBusy || index.Direct != want.Direct {
		t.Fatal("fixed impact contracts/flags lost", index)
	}
	found := false
	for _, file := range input.Materials.Files {
		if file.Path != "context/requirement-impacts.jsonl" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		var impact domain.RequirementImpact
		if err != nil || json.Unmarshal(bytes.TrimSpace(data), &impact) != nil || !reflect.DeepEqual(impact, want) || Hash(string(data)) != file.SHA256 || file.Bytes != len(data) || file.Records != 1 {
			t.Fatal("fixed snapshot changed or truncated", err)
		}
		found = true
	}
	if !found {
		t.Fatal("missing deferred impact snapshot")
	}
}

func TestLeaderMaterialsReclaimedOnlyAfterBoundStopProof(t *testing.T) {
	for _, proof := range []string{"owner", "late_receipt"} {
		t.Run(proof, func(t *testing.T) {
			f, d, _ := planningFixture(t, true)
			scheduleTest(t, f, d, "uncertain-materials")
			ctx := context.Background()
			var dir string
			w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, work, _ string) (PlanningOutcome, error) {
				dir = work
				return PlanningOutcome{TreeStopped: false}, nil
			})}
			if err := w.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			r := before.PlanningRuns[0]
			if r.State != "unknown_live" || r.WorkingDirectory != dir {
				t.Fatal("missing uncertain snapshot", r)
			}
			path := filepath.Join(dir, "context", "manifest.json")
			if _, err := os.Stat(path); err != nil {
				t.Fatal("uncertain process lost its files", err)
			}
			bad := PlanningOutcome{Generation: r.Generation + 1, DocumentVersion: r.DocumentVersion, TreeStopped: true}
			bindPlanningOutcome(&bad, r)
			if err := w.record(ctx, r, bad); err == nil {
				t.Fatal("stale proof accepted")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("stale proof deleted files", err)
			}
			if proof == "owner" {
				decision := before.Decisions[0]
				in := domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "confirm_stopped", Evidence: "operator verified the entire original tree stopped"}
				for i := 0; i < 2; i++ {
					if _, err := f.s.ResolveDecision(ctx, f.owner, "stop-materials", decision.ID, in); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true}
				bindPlanningOutcome(&out, r)
				data, _ := json.Marshal(out)
				if err := os.WriteFile(w.receipt(r.ID), data, 0600); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					if err := w.Recover(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			after := f.snapshot(t)
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stopped process left materials", err)
			}
			if got := after.PlanningRuns[0]; got.WorkingDirectory != "" || !got.TreeStopped || got.State != "stopped" || got.GrantSeconds != r.GrantSeconds || after.Project.UsedSeconds != before.Project.UsedSeconds {
				t.Fatal("cleanup changed accounting or retained path", got)
			}
		})
	}
}

func TestStoppedLeaderCleanupRetriesAndRejectsForeignPaths(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	r := scheduleTest(t, f, d, "cleanup-retry")
	dir, err := os.MkdirTemp("", "coordica-planning-"+r.ID+"-"+fmt.Sprint(r.Generation)+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	foreign := t.TempDir()
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		r.TreeStopped, r.State, r.WorkingDirectory = true, "stopped", foreign
		return tx.Put("planning_run", r.ID, r)
	}); err != nil {
		t.Fatal(err)
	}
	w := &PlanningWorker{Service: f.s}
	if err := w.Recover(context.Background()); err == nil {
		t.Fatal("foreign cleanup path accepted")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("unowned directory removed")
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error { r.WorkingDirectory = dir; return tx.Put("planning_run", r.ID, r) }); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after removal but before clearing the persisted path.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := w.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.snapshot(t).PlanningRuns[0]; got.WorkingDirectory != "" {
		t.Fatal("restart did not finish cleanup")
	}
}
