package app

import (
	"context"
	"encoding/json"
	"errors"
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
