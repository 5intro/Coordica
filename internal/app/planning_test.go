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
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

type planningExecFunc func(context.Context, string, string) (PlanningOutcome, error)

func (f planningExecFunc) Execute(ctx context.Context, dir, prompt string, _ func(int) error) (PlanningOutcome, error) {
	return f(ctx, dir, prompt)
}
func planningFixture(t *testing.T, enabled bool) (*serviceFixture, domain.PlanningDocument, domain.LeaderResult) {
	t.Helper()
	f := newServiceFixture(t)
	ctx := context.Background()
	b, e := f.s.CreateRequirement(ctx, f.owner, "b-req", domain.Requirement{Title: "Source", Body: "Implement source"})
	req := decodeTest[domain.Requirement](t, b, e)
	d := domain.PlanningDocument{Objective: "Build source", NonGoals: "No publication changes", Acceptance: "Tests pass", RequirementIDs: []string{req.ID}, Architecture: "Go modules", DirectoryOutline: "src/: application", AllowedRoots: []string{"src"}, BudgetSeconds: 10000}
	b, e = f.s.SavePlanning(ctx, f.owner, "b-save", domain.PlanningSaveRequest{Document: d})
	d = decodeTest[domain.PlanningDocument](t, b, e)
	b, e = f.s.AuthorizePlanning(ctx, f.owner, "b-auth", domain.VersionRequest{ExpectedVersion: d.Version})
	d = decodeTest[domain.PlanningDocument](t, b, e)
	settings := f.snapshot(t).Automation
	settings.Enabled = enabled
	if _, e = f.s.ConfigureAutomation(ctx, f.owner, "b-auto", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
		t.Fatal(e)
	}
	result := domain.LeaderResult{Reply: "Ready", Ready: true, MissingItems: []string{}, Clarifications: []string{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}, Tasks: []domain.TaskProposal{{Key: "a", Title: "A", Objective: "Build", Acceptance: "Check", RequirementIDs: []string{req.ID}, Paths: []string{"src/**"}, Dependencies: []string{}, HotResources: []string{}, BudgetSeconds: 100, MaxRunSeconds: 30, MaxRepairs: 1, MaxRetries: 1}}}
	return f, d, result
}
func scheduleTest(t *testing.T, f *serviceFixture, d domain.PlanningDocument, key string) domain.PlanningRun {
	t.Helper()
	b, e := f.s.SchedulePlanning(context.Background(), f.owner, key, domain.PlanningMessageRequest{ExpectedVersion: d.Version, Text: "Evaluate"})
	return decodeTest[domain.PlanningRun](t, b, e)
}
func resultExecutor(v domain.LeaderResult) PlanningExecutor {
	return planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		b, _ := json.Marshal(v)
		return PlanningOutcome{Output: string(b), TreeStopped: true, SessionID: "test-executor", ElapsedSeconds: 2}, nil
	})
}
func TestPlanningMissingInputsFenceDirectApprovalsAndClaim(t *testing.T) {
	for _, field := range []string{"requirements", "architecture", "directory"} {
		t.Run(field, func(t *testing.T) {
			f, d, _ := planningFixture(t, true)
			switch field {
			case "requirements":
				d.RequirementIDs = nil
			case "architecture":
				d.Architecture = ""
			case "directory":
				d.DirectoryOutline = ""
			}
			b, e := f.s.SavePlanning(context.Background(), f.owner, "incomplete", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
			d = decodeTest[domain.PlanningDocument](t, b, e)
			if _, e = f.s.AuthorizePlanning(context.Background(), f.owner, "no-auth", domain.VersionRequest{ExpectedVersion: d.Version}); e == nil {
				t.Fatal("incomplete goal authorized")
			}
			if got := f.claim(t, "no-implement"); got != nil {
				t.Fatal("legacy approved task bypassed planning")
			}
			b, e = f.s.CreateTask(context.Background(), f.owner, "bypass-task", domain.Task{Title: "Bypass", Description: "Build", Acceptance: "Check", Tool: "codex", AllowedPaths: []string{"src/**"}, BudgetSeconds: 30, MaxRunSeconds: 10})
			task := decodeTest[domain.Task](t, b, e)
			if _, e = f.s.TaskAction(context.Background(), f.owner, "bypass-approve", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version}); e == nil {
				t.Fatal("direct task approval bypassed planning")
			}
		})
	}
}
func TestPlanningAdmissionDerivationAndDuplicateReplay(t *testing.T) {
	f, d, result := planningFixture(t, true)
	r := scheduleTest(t, f, d, "evaluate")
	same := scheduleTest(t, f, d, "evaluate")
	if same.ID != r.ID {
		t.Fatal("duplicate evaluation created run")
	}
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if snap.PlanningDocument.Status != "admitted" || len(snap.Tasks) != 2 || snap.Tasks[1].State != "ready" || len(snap.LeaderMessages) != 2 {
		t.Fatalf("admission failed: %+v", snap)
	}
	if e := w.apply(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).Tasks) != 2 {
		t.Fatal("duplicate completion created tasks")
	}
	assignment := f.claim(t, "implement-b")
	if assignment == nil || assignment.Task.ID != snap.Tasks[1].ID {
		t.Fatal("admitted task did not claim")
	}
	before := f.snapshot(t)
	settings := before.Automation
	settings.Enabled = false
	if _, e := f.s.ConfigureAutomation(context.Background(), f.owner, "disable", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
		t.Fatal(e)
	}
	response, e := f.s.Heartbeat(context.Background(), f.node, assignment.Run.ID, domain.HeartbeatRequest{Generation: assignment.Run.Generation})
	if e != nil || response.Cancel {
		t.Fatalf("settings off revoked authorized run: %+v %v", response, e)
	}
	after := f.snapshot(t)
	if after.Project.PolicyVersion != before.Project.PolicyVersion {
		t.Fatal("automation changed policy")
	}
	if after.PlanningDocument.ReservedSeconds != assignment.Run.GrantSeconds {
		t.Fatal("goal reservation missing")
	}
	f.now = f.now.Add(time.Second)
	_, e = f.s.Complete(context.Background(), f.node, "stop", assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "stopped", Quiescent: true, ElapsedSeconds: 1})
	if e != nil {
		t.Fatal(e)
	}
	after = f.snapshot(t)
	if after.PlanningDocument.ReservedSeconds != 0 || after.PlanningDocument.UsedSeconds <= 2 {
		t.Fatal("goal implementation settlement missing")
	}
}
func TestPlanningWholeBatchRollback(t *testing.T) {
	for _, scenario := range []string{"cycle", "missing_ref", "outside", "protected", "budget", "major_invalid"} {
		t.Run(scenario, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			second := result.Tasks[0]
			second.Key = "b"
			result.Tasks = append(result.Tasks, second)
			switch scenario {
			case "cycle":
				result.Tasks[0].Dependencies = []string{"b"}
				result.Tasks[1].Dependencies = []string{"a"}
			case "missing_ref":
				result.Tasks[1].Dependencies = []string{"missing"}
			case "outside":
				result.Tasks[1].Paths = []string{"outside/**"}
			case "protected":
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					p, e := getProject(tx)
					if e != nil {
						return e
					}
					p.ProtectedPaths = append(p.ProtectedPaths, "src/private/**")
					return tx.Put("project", p.ID, p)
				}); e != nil {
					t.Fatal(e)
				}
			case "budget":
				result.Tasks[1].BudgetSeconds = 10000
			case "major_invalid":
				result.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "architecture", Actions: []string{}}}
			}
			scheduleTest(t, f, d, "evaluate")
			w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
			if e := w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if len(snap.Tasks) != 1 || snap.PlanningDocument.Status == "admitted" || len(snap.Decisions) != 1 || snap.PlanningRuns[0].State != "blocked" {
				t.Fatalf("partial batch or missing decision: %+v", snap)
			}
		})
	}
}
func TestPlanningSettingsOffRaceAndGoalChange(t *testing.T) {
	for _, change := range []string{"settings", "goal"} {
		t.Run(change, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			r := scheduleTest(t, f, d, "evaluate")
			w := &PlanningWorker{Service: f.s}
			run, _, _, e := w.reserve(context.Background())
			if e != nil || run.ID != r.ID {
				t.Fatal(e)
			}
			switch change {
			case "settings":
				v := f.snapshot(t).Automation
				v.Enabled = false
				if _, e = f.s.ConfigureAutomation(context.Background(), f.owner, "disable-race", domain.AutomationRequest{ExpectedVersion: v.Version, Settings: v}); e != nil {
					t.Fatal(e)
				}
			case "goal":
				d.Objective = "Changed objective"
				if _, e = f.s.SavePlanning(context.Background(), f.owner, "goal-race", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d}); e != nil {
					t.Fatal(e)
				}
			}
			text, _ := json.Marshal(result)
			if e = w.record(context.Background(), run, PlanningOutcome{Generation: run.Generation, DocumentVersion: run.DocumentVersion, TreeStopped: true, Output: string(text), ElapsedSeconds: 1}); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if change == "settings" {
				if len(snap.Tasks) != 2 || snap.Tasks[1].State != "draft" {
					t.Fatal("settings off race approved tasks")
				}
			} else if len(snap.Tasks) != 1 || snap.PlanningRuns[0].State != "stopped" {
				t.Fatal("old document result created tasks")
			}
		})
	}
}
func TestPlanningRestartRequiresStopEvidenceAndChargesFullGrant(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	scheduleTest(t, f, d, "evaluate")
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if snap.PlanningRuns[0].State != "unknown_live" || snap.PlanningRuns[0].TreeStopped || snap.PlanningDocument.ReservedSeconds != 0 || snap.PlanningDocument.UsedSeconds != r.GrantSeconds || snap.Project.UsedSeconds != r.GrantSeconds {
		t.Fatal("restart invented stop evidence or refunded unknown work")
	}
	if e = w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).Project.UsedSeconds != r.GrantSeconds {
		t.Fatal("restart double charged")
	}
	if _, e = f.s.SchedulePlanning(context.Background(), f.owner, "blocked", domain.PlanningMessageRequest{ExpectedVersion: d.Version}); e == nil {
		t.Fatal("unknown live process did not block")
	}
	decision := snap.Decisions[0]
	_, e = f.s.ResolveDecision(context.Background(), f.owner, "confirm", decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "confirm_stopped", Evidence: "operator checked process tree"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SchedulePlanning(context.Background(), f.owner, "new", domain.PlanningMessageRequest{ExpectedVersion: d.Version}); e != nil {
		t.Fatal(e)
	}
}
func TestPlanningDurableReceiptAndPendingReplay(t *testing.T) {
	for _, stage := range []string{"receipt", "pending"} {
		t.Run(stage, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			scheduleTest(t, f, d, "evaluate")
			w := &PlanningWorker{Service: f.s}
			r, _, _, e := w.reserve(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			text, _ := json.Marshal(result)
			out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Output: string(text), ElapsedSeconds: 2}
			if stage == "receipt" {
				if e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700); e != nil {
					t.Fatal(e)
				}
				bindPlanningOutcome(&out, r)
				b, _ := json.Marshal(out)
				if e = os.WriteFile(w.receipt(r.ID), b, 0600); e != nil {
					t.Fatal(e)
				}
			} else {
				if e = w.record(context.Background(), r, out); e != nil {
					t.Fatal(e)
				}
				if e = w.Recover(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			if e = w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if len(snap.Tasks) != 2 || snap.PlanningRuns[0].State != "succeeded" || !snap.PlanningRuns[0].TreeStopped || snap.Project.UsedSeconds != 2 {
				t.Fatal("durable receipt did not replay exactly once")
			}
		})
	}
}
func TestPlanningCancellationSupervisesExecutor(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	scheduleTest(t, f, d, "evaluate")
	entered := make(chan struct{})
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(ctx context.Context, dir, prompt string) (PlanningOutcome, error) {
		if strings.HasPrefix(dir, f.s.Dir+string(os.PathSeparator)) || dir == f.s.Dir {
			t.Error("not isolated")
		}
		close(entered)
		<-ctx.Done()
		return PlanningOutcome{TreeStopped: true}, ctx.Err()
	})}
	done := make(chan error, 1)
	go func() { done <- w.Tick(context.Background()) }()
	<-entered
	if _, e := f.s.CancelPlanning(context.Background(), f.owner, "cancel-plan", domain.VersionRequest{ExpectedVersion: d.Version}); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("executor was not cancelled")
	}
	snap := f.snapshot(t)
	if snap.PlanningRuns[0].State != "stopped" || !snap.PlanningRuns[0].TreeStopped || snap.Project.ReservedSeconds != 0 {
		t.Fatal("cancel failed to settle")
	}
}
func TestPlanningRetriesBoundedAndBudgeted(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	scheduleTest(t, f, d, "evaluate")
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2}, errors.New("quiet failure")
	})}
	for i := 0; i < 4; i++ {
		if e := w.Tick(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	snap := f.snapshot(t)
	if len(snap.PlanningRuns) != 3 || snap.Project.UsedSeconds != 6 || snap.Project.ReservedSeconds != 0 || len(snap.Decisions) != 1 {
		t.Fatalf("retry unbounded or free: %+v", snap.PlanningRuns)
	}
}
func TestPlanningStrictDecodeAndLiteralContainment(t *testing.T) {
	for _, text := range []string{`{"reply":"ready","ready":true}`, `{"reply":"x","ready":true,"missing_items":[],"clarifications":[],"tasks":[],"scopes":[],"major_decisions":[],"authority":true}`, `{} {}`} {
		if _, e := decodeLeader(text); e == nil {
			t.Fatal("malformed result accepted")
		}
	}
	for _, p := range []string{"**", "src*/*", "src/../other/**", "src/[ab]/**", "src2/**"} {
		if e := literalScope([]string{p}, []string{"src"}); e == nil {
			t.Fatalf("unsafe containment %s", p)
		}
	}
	if e := literalScope([]string{"src/file.go", "src/child/**"}, []string{"src"}); e != nil {
		t.Fatal(e)
	}
}
func TestNodeSchedulingPolicyAndBusyHeartbeat(t *testing.T) {
	for _, mode := range []string{"draining", "disabled", "tool", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			f := newServiceFixture(t)
			var n domain.Node
			for _, v := range f.snapshot(t).Nodes {
				if v.ID == f.node.NodeID {
					n = v
				}
			}
			req := domain.NodeConfigureRequest{ExpectedVersion: n.Version, Name: n.Name, Capacity: 1, Tools: []domain.ToolScheduling{{Tool: "codex", Enabled: true, Implement: true, Review: true}}}
			switch mode {
			case "draining":
				req.Draining = true
			case "disabled":
				req.DisabledNewWork = true
			case "tool":
				req.Tools[0].Enabled = false
			}
			if _, e := f.s.ConfigureNode(context.Background(), f.owner, "config", n.ID, req); e != nil {
				t.Fatal(e)
			}
			a := f.claim(t, "claim-config")
			if mode != "capacity" {
				if a != nil {
					t.Fatal("disabled node claimed")
				}
				return
			}
			if a == nil {
				t.Fatal("configured node could not claim")
			}
			f.now = f.now.Add(time.Second)
			if _, e := f.s.Heartbeat(context.Background(), f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation}); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if !snap.Nodes[0].LastSeen.Equal(f.now) {
				t.Fatal("busy heartbeat not seen")
			}
			b, e := f.s.CreateTask(context.Background(), f.owner, "second", domain.Task{Title: "Second", Description: "Second", Acceptance: "Check", Tool: "codex", AllowedPaths: []string{"src/**"}, BudgetSeconds: 30, MaxRunSeconds: 10})
			task := decodeTest[domain.Task](t, b, e)
			if _, e = f.s.TaskAction(context.Background(), f.owner, "approve-second", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version}); e != nil {
				t.Fatal(e)
			}
			if f.claim(t, "over-capacity") != nil {
				t.Fatal("capacity ignored")
			}
			if _, e = f.s.Heartbeat(context.Background(), f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation + 1}); e == nil {
				t.Fatal("heartbeat fencing weakened")
			}
		})
	}
}
func TestMajorDecisionSourceVersionsAndNoAdmission(t *testing.T) {
	f, d, result := planningFixture(t, true)
	result.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "architecture", Evidence: "Need another backend", Actions: []string{"dismiss", "edit_goal"}}}
	scheduleTest(t, f, d, "evaluate")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if snap.PlanningDocument.Status == "admitted" || snap.Tasks[1].State != "draft" {
		t.Fatal("major decision auto admitted")
	}
	decision := snap.Decisions[0]
	req := domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion - 1, Action: "edit_goal", Evidence: "owner decision"}
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "old-source", decision.ID, req); e == nil {
		t.Fatal("stale decision source accepted")
	}
	req.ExpectedSourceVersion = decision.SourceVersion
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "resolve", decision.ID, req); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).PlanningDocument.Status == "admitted" {
		t.Fatal("decision resolution implicitly changed authority")
	}
}

func TestPlanningGoalChangeFencesActiveDevelopment(t *testing.T) {
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "evaluate")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	a := f.claim(t, "active")
	if a == nil {
		t.Fatal("no implementation")
	}
	before := f.snapshot(t)
	d = *before.PlanningDocument
	d.Acceptance = "New acceptance"
	b, e := f.s.SavePlanning(context.Background(), f.owner, "change-active", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
	changed := decodeTest[domain.PlanningDocument](t, b, e)
	hb, e := f.s.Heartbeat(context.Background(), f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation})
	if e != nil || !hb.Cancel {
		t.Fatal("goal edit failed to stop development")
	}
	_, e = f.s.Complete(context.Background(), f.node, "late-success", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, Head: strings.Repeat("b", 40)})
	if e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.Candidates) != 0 || snap.PlanningDocument.Status != "planning" || snap.PlanningDocument.AuthorizationVersion == before.PlanningDocument.AuthorizationVersion || snap.PlanningDocument.UsedSeconds < before.PlanningDocument.UsedSeconds || snap.PlanningDocument.ReservedSeconds != 0 {
		t.Fatal("goal change accepted old implementation or lost accounting")
	}
	if changed.BudgetSeconds != before.PlanningDocument.BudgetSeconds || snap.Project.PolicyVersion != before.Project.PolicyVersion {
		t.Fatal("goal edit changed unrelated policy")
	}
	if f.claim(t, "fenced") != nil {
		t.Fatal("old task claimed after goal edit")
	}
}
func TestPlanningExpiredAndWrongGenerationResultsCannotDerive(t *testing.T) {
	for _, scenario := range []string{"expired", "generation"} {
		t.Run(scenario, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			scheduleTest(t, f, d, "evaluate")
			w := &PlanningWorker{Service: f.s}
			r, _, _, e := w.reserve(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			text, _ := json.Marshal(result)
			out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, Output: string(text), TreeStopped: true, ElapsedSeconds: 2}
			if scenario == "expired" {
				f.now = r.Deadline.Add(time.Second)
			} else {
				out.Generation++
			}
			e = w.record(context.Background(), r, out)
			if scenario == "generation" && e == nil {
				t.Fatal("wrong generation accepted")
			}
			if scenario == "expired" && e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if len(snap.Tasks) != 1 || snap.PlanningDocument.Status == "admitted" {
				t.Fatal("expired/generation result derived tasks")
			}
			if scenario == "expired" && snap.Project.UsedSeconds != r.GrantSeconds {
				t.Fatal("expired completion refunded uncertain grant")
			}
		})
	}
}
func TestPlanningReceiptSurvivesLeaseElapsedAfterQuietFinish(t *testing.T) {
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "evaluate")
	w := &PlanningWorker{Service: f.s}
	r, _, _, e := w.reserve(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	text, _ := json.Marshal(result)
	out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, FinishedAt: f.now.Add(time.Second), Output: string(text), TreeStopped: true, ElapsedSeconds: 2}
	bindPlanningOutcome(&out, r)
	b, _ := json.Marshal(out)
	if e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(w.receipt(r.ID), b, 0600); e != nil {
		t.Fatal(e)
	}
	f.now = r.Deadline.Add(time.Hour)
	if e = w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if snap.PlanningRuns[0].State != "succeeded" || snap.Project.UsedSeconds != 2 || len(snap.Tasks) != 2 {
		t.Fatal("quiet durable receipt lost to restart wall time")
	}
}
func TestPlanningScopeAutomationPreservesBoundaryAndSetting(t *testing.T) {
	for _, scenario := range []string{"ordinary", "protected", "outside", "disabled", "resource"} {
		t.Run(scenario, func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			result.Tasks[0].Paths = []string{"src/old/**"}
			scheduleTest(t, f, d, "initial")
			w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
			if e := w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			task := snap.Tasks[1]
			result.Tasks = []domain.TaskProposal{}
			result.Scopes = []domain.ScopeProposal{{TaskID: task.ID, ExpectedVersion: task.Version, Paths: []string{"src/old/**", "src/new/**"}, Reason: "Need another source directory"}}
			switch scenario {
			case "protected":
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					p, e := getProject(tx)
					if e != nil {
						return e
					}
					p.ProtectedPaths = append(p.ProtectedPaths, "src/new/private/**")
					return tx.Put("project", p.ID, p)
				}); e != nil {
					t.Fatal(e)
				}
			case "outside":
				result.Scopes[0].Paths = []string{"elsewhere/**"}
			case "disabled":
				settings := snap.Automation
				settings.Enabled = false
				if _, e := f.s.ConfigureAutomation(context.Background(), f.owner, "disable-scope", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
					t.Fatal(e)
				}
			case "resource":
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					task.HotResources = []string{"shared"}
					if e := tx.Put("task", task.ID, task); e != nil {
						return e
					}
					return tx.Put("resource", "shared", domain.Resource{ID: "shared", TaskID: "another"})
				}); e != nil {
					t.Fatal(e)
				}
			}
			scheduleTest(t, f, d, "scope")
			w.Executor = resultExecutor(result)
			if e := w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			got := after.Tasks[1]
			if scenario == "ordinary" {
				if got.AuthorizationVersion != task.AuthorizationVersion+1 || !containsPaths(got.AllowedPaths, []string{"src/old/**", "src/new/**"}) {
					t.Fatal("ordinary scope not automated")
				}
			} else if got.AuthorizationVersion != task.AuthorizationVersion || got.AllowedPaths[0] != task.AllowedPaths[0] || len(after.Decisions) == 0 {
				t.Fatal("unsafe or disabled scope changed authority without decision")
			}
		})
	}
}

func TestPlanningPendingCompletionCancellationAndRecovery(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "replay", true: "cancel"}[cancel], func(t *testing.T) {
			f, d, result := planningFixture(t, true)
			r := scheduleTest(t, f, d, "evaluate")
			// Simulate the durable database boundary after settlement and before task
			// derivation. These are injected records, never native Agent verification.
			if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				r.State = "pending_result"
				r.Version++
				r.Settled = true
				r.TreeStopped = true
				r.ExecutionAdapter = "injected"
				r.PendingResult = &result
				return tx.Put("planning_run", r.ID, r)
			}); e != nil {
				t.Fatal(e)
			}
			if cancel {
				if _, e := f.s.CancelPlanning(context.Background(), f.owner, "pending-cancel", domain.VersionRequest{ExpectedVersion: d.Version}); e != nil {
					t.Fatal(e)
				}
			}
			w := &PlanningWorker{Service: f.s}
			if e := w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			if e := w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			if snap.PlanningRuns[0].ExecutionAdapter != "injected" {
				t.Fatal("test executor became native evidence")
			}
			if cancel {
				if len(snap.Tasks) != 1 || snap.PlanningRuns[0].State != "stopped" {
					t.Fatal("cancelled pending result derived tasks")
				}
			} else if len(snap.Tasks) != 2 || snap.PlanningRuns[0].State != "succeeded" {
				t.Fatal("pending completion did not replay once")
			}
		})
	}
}

func TestNodeDrainingDoesNotInventStopOrRewriteProbeFacts(t *testing.T) {
	f := newServiceFixture(t)
	a := f.claim(t, "active-node")
	if a == nil {
		t.Fatal("no run")
	}
	n := f.snapshot(t).Nodes[0]
	_, e := f.s.ConfigureNode(context.Background(), f.owner, "drain-active", n.ID, domain.NodeConfigureRequest{ExpectedVersion: n.Version, Name: n.Name, Capacity: 1, Draining: true, DisabledNewWork: true, Tools: []domain.ToolScheduling{{Tool: "codex", Enabled: false}}})
	if e != nil {
		t.Fatal(e)
	}
	hb, e := f.s.Heartbeat(context.Background(), f.node, a.Run.ID, domain.HeartbeatRequest{Generation: a.Run.Generation})
	if e != nil || hb.Cancel {
		t.Fatal("draining revoked active authority")
	}
	snap := f.snapshot(t)
	if snap.Runs[0].Quiescent || snap.Runs[0].State != "running" || !snap.Nodes[0].Capabilities[0].Available {
		t.Fatal("node policy invented stop or unavailable probe fact")
	}
}
func TestPlanningDAGMapsLocalKeysAtomically(t *testing.T) {
	f, d, result := planningFixture(t, true)
	second := result.Tasks[0]
	second.Key = "b"
	second.Dependencies = []string{"a"}
	result.Tasks = append(result.Tasks, second)
	r := scheduleTest(t, f, d, "dag")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.Tasks) != 3 || snap.Tasks[2].Dependencies[0] != snap.Tasks[1].ID || len(snap.PlanningRuns[0].TaskMapping) != 2 || snap.PlanningRuns[0].TaskMapping[0].Key != "a" || snap.PlanningRuns[0].TaskMapping[1].TaskID != snap.Tasks[2].ID {
		t.Fatal("local dependency keys were not mapped")
	}
	if e := w.apply(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).Tasks) != 3 {
		t.Fatal("DAG replay duplicated tasks")
	}
}

func TestPlanningUnknownStopDecisionRemainsActionableAfterGoalEdit(t *testing.T) {
	f, d, _ := planningFixture(t, true)
	scheduleTest(t, f, d, "unknown")
	w := &PlanningWorker{Service: f.s}
	if _, _, _, e := w.reserve(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	oldDecision := f.snapshot(t).Decisions[0]
	beforeUsed := f.snapshot(t).PlanningDocument.PlanningUsedSeconds
	d = *f.snapshot(t).PlanningDocument
	d.Objective = "Updated scope"
	b, e := f.s.SavePlanning(context.Background(), f.owner, "unknown-goal-edit", domain.PlanningSaveRequest{ExpectedVersion: d.Version, Document: d})
	d = decodeTest[domain.PlanningDocument](t, b, e)
	snap := f.snapshot(t)
	decision := snap.Decisions[0]
	if decision.Status != "open" || decision.DocumentVersion != d.Version {
		t.Fatal("unknown stop decision lost across goal change")
	}
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "stale-old-stop-decision", oldDecision.ID, domain.DecisionResolveRequest{ExpectedVersion: oldDecision.Version, ExpectedSourceVersion: oldDecision.SourceVersion, Action: "confirm_stopped", Evidence: "operator inspected old tree"}); e == nil {
		t.Fatal("stale decision evidence accepted")
	}
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "stale-old-stop-source", decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion - 1, Action: "confirm_stopped", Evidence: "operator inspected old tree"}); e == nil {
		t.Fatal("stale source evidence accepted")
	}
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "blank-old-stop-evidence", decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "confirm_stopped"}); e == nil {
		t.Fatal("empty owner evidence accepted")
	}
	if _, e := f.s.ResolveDecision(context.Background(), f.owner, "stop-edited-goal", decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "confirm_stopped", Evidence: "operator inspected old process tree"}); e != nil {
		t.Fatal(e)
	}
	if after := f.snapshot(t); after.PlanningDocument.PlanningUsedSeconds != beforeUsed || len(after.Tasks) != 1 || after.PlanningDocument.Status == "admitted" {
		t.Fatal("stop evidence granted authority or refunded cumulative charge")
	}
	if _, e := f.s.SchedulePlanning(context.Background(), f.owner, "after-confirm", domain.PlanningMessageRequest{ExpectedVersion: d.Version}); e != nil {
		t.Fatal(e)
	}
}
func TestPlanningGoalExhaustionPersistsDecisionWithoutInventingSchedulingHolds(t *testing.T) {
	f, _, _ := planningFixture(t, true)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return e
		}
		d.UsedSeconds = d.BudgetSeconds
		return tx.Put("planning_document", "current", d)
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Expire(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.Decisions) != 1 || snap.Decisions[0].SourceKind != "planning_document" || snap.Decisions[0].Kind != "budget" {
		t.Fatal("goal budget exhaustion is not actionable")
	}
	if e := f.s.Expire(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).Decisions) != 1 {
		t.Fatal("budget poll duplicated decisions")
	}
}

func TestRepeatedPlanningReusesProposalKeysAndPreservesActiveAdmission(t *testing.T) {
	f, d, result := planningFixture(t, true)
	ctx := context.Background()
	scheduleTest(t, f, d, "repeat-first")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	before := f.snapshot(t)
	a := f.claim(t, "repeat-active")
	if a == nil {
		t.Fatal("missing active admitted task")
	}
	scheduleTest(t, f, d, "repeat-same")
	w.Executor = planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		for _, fact := range []string{a.Task.ID, `"key":"a"`, "Implement source", "ONLY reference real existing task IDs", "Initial planning scopes MUST be []"} {
			if !strings.Contains(prompt, fact) {
				t.Errorf("missing Leader fact %q", fact)
			}
		}
		out, _ := json.Marshal(result)
		return PlanningOutcome{TreeStopped: true, Output: string(out), ElapsedSeconds: 2}, nil
	})
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if len(after.Tasks) != len(before.Tasks) || after.PlanningDocument.Status != "admitted" {
		t.Fatal("repeat created work or invalidated admission")
	}
	for _, task := range after.Tasks {
		if task.ID == a.Task.ID && (task.CurrentRunID != a.Run.ID || task.AuthorizationVersion != a.Task.AuthorizationVersion || task.ReservedSeconds != a.Run.GrantSeconds) {
			t.Fatal("repeat reset active task accounting")
		}
	}
	// A purely conversational response cannot change admission or active work.
	result.Tasks = []domain.TaskProposal{}
	result.Ready = false
	result.Clarifications = []string{"Discussing implementation detail"}
	scheduleTest(t, f, d, "repeat-chat")
	w.Executor = resultExecutor(result)
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.snapshot(t).PlanningDocument.Status != "admitted" {
		t.Fatal("mere chat revoked admitted goal")
	}
}
func TestPlanningInventedScopeTaskKeyRollsBackEntireBatch(t *testing.T) {
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "invented-scope-key")
	result.Scopes = []domain.ScopeProposal{{TaskID: result.Tasks[0].Key, ExpectedVersion: 1, Paths: []string{"src/**"}, Reason: "invented proposal ID"}}
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.Tasks) != 1 || snap.PlanningRuns[0].State != "blocked" || len(snap.Decisions) == 0 {
		t.Fatal("invented scope authority partially applied")
	}
}

func TestLeaderNestedContractStrictness(t *testing.T) {
	_, _, valid := planningFixture(t, true)
	valid.Scopes = []domain.ScopeProposal{{TaskID: "existing-task", ExpectedVersion: 3, Paths: []string{"src/**"}, Reason: "Existing scope"}}
	valid.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "architecture", Evidence: "Owner decision needed", Actions: []string{}}}
	raw, _ := json.Marshal(valid)
	if _, err := decodeLeader(string(raw)); err != nil {
		t.Fatal("valid contract rejected", err)
	}
	for _, scenario := range []string{"acceptance-array", "unknown-task", "unknown-scope", "unknown-decision", "null-root-array", "null-task-array", "null-scope-array", "null-decision-array", "null-string", "null-bool", "null-int", "missing-nested", "float-int", "null-array-element"} {
		t.Run(scenario, func(t *testing.T) {
			var object map[string]any
			json.Unmarshal(raw, &object)
			task := object["tasks"].([]any)[0].(map[string]any)
			scope := object["scopes"].([]any)[0].(map[string]any)
			decision := object["major_decisions"].([]any)[0].(map[string]any)
			switch scenario {
			case "acceptance-array":
				task["acceptance"] = []string{"Test"}
			case "unknown-task":
				task["authority"] = true
			case "unknown-scope":
				scope["authority"] = true
			case "unknown-decision":
				decision["authority"] = true
			case "null-root-array":
				object["tasks"] = nil
			case "null-task-array":
				task["dependencies"] = nil
			case "null-scope-array":
				scope["paths"] = nil
			case "null-decision-array":
				decision["actions"] = nil
			case "null-string":
				task["acceptance"] = nil
			case "null-bool":
				object["ready"] = nil
			case "null-int":
				scope["expected_version"] = nil
			case "missing-nested":
				delete(task, "max_retries")
			case "float-int":
				task["max_run_seconds"] = 1.5
			case "null-array-element":
				task["paths"] = []any{nil}
			}
			bad, _ := json.Marshal(object)
			if _, err := decodeLeader(string(bad)); err == nil {
				t.Fatal("bad nested contract accepted", string(bad))
			}
		})
	}
}

func TestLeaderPromptDeclaresProposalTypesAndInitialScopeRule(t *testing.T) {
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "typed-prompt")
	called := false
	worker := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		called = true
		for _, value := range []any{domain.TaskProposal{}, domain.ScopeProposal{}, domain.MajorDecisionProposal{}} {
			typ := reflect.TypeOf(value)
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				label := map[reflect.Kind]string{reflect.String: "string", reflect.Int: "integer", reflect.Int64: "integer", reflect.Slice: "string array"}[field.Type.Kind()]
				if label == "" || !strings.Contains(prompt, name+" ("+label) {
					t.Errorf("missing exact proposal field type: %s %s", name, label)
				}
			}
		}
		for _, rule := range []string{"acceptance (string, never an array)", "Initial planning scopes MUST be []", "NEVER use a proposal local key as task_id", "All fields are required and non-null"} {
			if !strings.Contains(prompt, rule) {
				t.Errorf("missing prompt rule %q", rule)
			}
		}
		raw, _ := json.Marshal(result)
		return PlanningOutcome{Output: string(raw), TreeStopped: true, ElapsedSeconds: 2}, nil
	})}
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("Leader prompt was not exercised")
	}
}
