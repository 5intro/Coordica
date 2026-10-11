package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func scopeFixture(t *testing.T) (*serviceFixture, *PlanningWorker, domain.Assignment, domain.ScopeAssessment) {
	t.Helper()
	ctx := context.Background()
	f, d, result := planningFixture(t, true)
	result.Tasks[0].Paths = []string{"src/old/**"}
	result.Tasks[0].MaxRetries = 2
	result.Tasks[0].MaxRepairs = 2
	scheduleTest(t, f, d, "scope-initial")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	a := f.claim(t, "scope-claim")
	if a == nil {
		t.Fatal("missing admitted assignment")
	}
	req := domain.RunScopeRequest{Generation: a.Run.Generation, Reason: "Need the shared source helper for the existing requirement", Paths: []string{"src/shared/**"}}
	b, e := f.s.RequestScope(ctx, f.node, "scope-request", a.Run.ID, req)
	v := decodeTest[domain.ScopeAssessment](t, b, e)
	replay, e := f.s.RequestScope(ctx, f.node, "scope-request", a.Run.ID, req)
	if e != nil || string(replay) != string(b) {
		t.Fatal("request replay changed identity", e)
	}
	return f, w, *a, v
}
func stopScope(t *testing.T, f *serviceFixture, a domain.Assignment) domain.Task {
	t.Helper()
	if _, e := f.s.Complete(context.Background(), f.node, "scope-stopped", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "stopped", Quiescent: true, ElapsedSeconds: 3}); e != nil {
		t.Fatal(e)
	}
	for _, task := range f.snapshot(t).Tasks {
		if task.ID == a.Task.ID {
			return task
		}
	}
	t.Fatal("task absent")
	return domain.Task{}
}
func scopeResult(task domain.Task, v domain.ScopeAssessment) domain.LeaderResult {
	return domain.LeaderResult{Reply: "Ordinary extension for the same requirement", Ready: true, MissingItems: []string{}, Clarifications: []string{}, MajorDecisions: []domain.MajorDecisionProposal{}, Tasks: []domain.TaskProposal{}, Scopes: []domain.ScopeProposal{{TaskID: task.ID, ExpectedVersion: task.Version, Paths: append(append([]string{}, task.AllowedPaths...), v.Paths...), Reason: v.Reason}}}
}
func TestRunScopeAutomaticallyAssessedOnlyAfterExactStop(t *testing.T) {
	f, w, a, v := scopeFixture(t)
	ctx := context.Background()
	beforePlans := len(f.snapshot(t).PlanningRuns)
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).PlanningRuns) != beforePlans {
		t.Fatal("assessment ran before quiet evidence")
	}
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if e := tx.Get("task", a.Task.ID, &task); e != nil {
			return e
		}
		task.Repairs = 1
		task.Retries = 1
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	task := stopScope(t, f, a)
	w.Executor = planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		for _, fact := range []string{v.ID, v.RunID, v.Reason, "src/shared/**", "Implement source", "proposal_mappings", "existing_tasks", "ONLY reference real existing task IDs"} {
			if !strings.Contains(prompt, fact) {
				t.Errorf("missing context %q", fact)
			}
		}
		out, _ := json.Marshal(scopeResult(task, v))
		return PlanningOutcome{TreeStopped: true, Output: string(out), ElapsedSeconds: 2}, nil
	})
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	var got domain.Task
	for _, q := range snap.Tasks {
		if q.ID == task.ID {
			got = q
		}
	}
	if got.State != "ready" || got.HoldReason != "" || got.AuthorizationVersion != task.AuthorizationVersion+1 || !containsPaths(got.AllowedPaths, append(task.AllowedPaths, v.Paths...)) || got.UsedSeconds != task.UsedSeconds || got.Repairs != 1 || got.Retries != 1 || got.BudgetSeconds != task.BudgetSeconds {
		t.Fatalf("ordinary scope failed or reset cumulative authority/accounting: %+v plans=%+v decisions=%+v", got, snap.PlanningRuns, snap.Decisions)
	}
	if snap.ScopeRequests[0].State != "consumed" {
		t.Fatal("scope request not consumed")
	}
	plans := len(snap.PlanningRuns)
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).PlanningRuns) != plans {
		t.Fatal("consumed request was requeued")
	}
	if _, e := f.s.Complete(ctx, f.node, "delayed-scope-old-output", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, Head: strings.Repeat("b", 40), ArtifactID: "invented"}); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	for _, q := range after.Tasks {
		if q.ID == got.ID && (q.AuthorizationVersion != got.AuthorizationVersion || q.State != "ready") {
			t.Fatal("late old output changed scope admission")
		}
	}
	if len(after.Candidates) != 0 {
		t.Fatal("late old result accepted")
	}
}
func TestRunScopeUnsafeAndPolicyRacesRemainActionable(t *testing.T) {
	for _, scenario := range []string{"outside", "protected", "disabled", "policy-race", "resource", "budget", "retries", "stale-version", "shrink", "extra-permission", "new-work", "major-goal", "unknown-tree"} {
		t.Run(scenario, func(t *testing.T) {
			f, w, a, v := scopeFixture(t)
			ctx := context.Background()
			task := stopScope(t, f, a)
			result := scopeResult(task, v)
			switch scenario {
			case "outside":
				result.Scopes[0].Paths = append(task.AllowedPaths, "src2/**")
			case "protected":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					p, e := getProject(tx)
					if e != nil {
						return e
					}
					p.ProtectedPaths = []string{"src/shared/private/**"}
					return tx.Put("project", p.ID, p)
				}); e != nil {
					t.Fatal(e)
				}
			case "disabled":
				settings := f.snapshot(t).Automation
				settings.Enabled = false
				if _, e := f.s.ConfigureAutomation(ctx, f.owner, "scope-disable", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
					t.Fatal(e)
				}
			case "resource":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					task.HotResources = []string{"shared"}
					if e := tx.Put("task", task.ID, task); e != nil {
						return e
					}
					return tx.Put("resource", "shared", domain.Resource{ID: "shared", TaskID: "other"})
				}); e != nil {
					t.Fatal(e)
				}
			case "budget", "retries":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					if scenario == "budget" {
						task.UsedSeconds = task.BudgetSeconds
					} else {
						task.Retries = task.MaxRetries
					}
					return tx.Put("task", task.ID, task)
				}); e != nil {
					t.Fatal(e)
				}
			case "stale-version":
				result.Scopes[0].ExpectedVersion--
			case "shrink":
				result.Scopes[0].Paths = v.Paths
			case "extra-permission":
				result.Scopes[0].Paths = append(result.Scopes[0].Paths, "src/invented/**")
			case "new-work":
				result.Tasks = []domain.TaskProposal{{Key: "invented"}}
			case "major-goal":
				result.Scopes = nil
				result.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "architecture", Evidence: "Replace Go with TS", Actions: []string{"edit_goal"}}}
			case "unknown-tree":
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					return tx.Put("run", "unknown-tree", domain.Run{ID: "unknown-tree", TaskID: task.ID, State: "lost", Quiescent: false})
				}); e != nil {
					t.Fatal(e)
				}
			}
			w.Executor = planningExecFunc(func(_ context.Context, _ string, _ string) (PlanningOutcome, error) {
				if scenario == "policy-race" {
					settings := f.snapshot(t).Automation
					settings.Enabled = false
					if _, e := f.s.ConfigureAutomation(ctx, f.owner, "scope-race", domain.AutomationRequest{ExpectedVersion: settings.Version, Settings: settings}); e != nil {
						t.Fatal(e)
					}
				}
				out, _ := json.Marshal(result)
				return PlanningOutcome{TreeStopped: true, Output: string(out), ElapsedSeconds: 2}, nil
			})
			if e := w.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			snap := f.snapshot(t)
			for _, got := range snap.Tasks {
				if got.ID == task.ID && (got.HoldReason != "scope_requested" || got.AuthorizationVersion != task.AuthorizationVersion || !reflect.DeepEqual(got.AllowedPaths, task.AllowedPaths)) {
					t.Fatalf("unsafe scope admitted: %+v", got)
				}
			}
			if len(snap.Decisions) == 0 || snap.ScopeRequests[0].State != "blocked" {
				t.Fatalf("missing actionable failure: %+v", snap.ScopeRequests)
			}
		})
	}
}
func TestRunScopeRejectsStaleGenerationAndReasonOnlyStaysSafe(t *testing.T) {
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "initial-scope")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	a := f.claim(t, "scope-generation")
	if a == nil {
		t.Fatal("missing claim")
	}
	if _, e := f.s.RequestScope(context.Background(), f.node, "scope-stale", a.Run.ID, domain.RunScopeRequest{Generation: a.Run.Generation - 1, Reason: "Need source", Paths: []string{"src/x/**"}}); e == nil {
		t.Fatal("stale scope accepted")
	}
	if _, e := f.s.RequestScope(context.Background(), f.node, "scope-reason-only", a.Run.ID, domain.RunScopeRequest{Generation: a.Run.Generation, Reason: "Need source"}); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	if len(snap.ScopeRequests) != 0 || len(snap.Decisions) == 0 {
		t.Fatal("reason-only request fabricated ordinary permission")
	}
}

func TestRunScopeUnknownOldTreeRequiresOwnerEvidenceThenAutomaticallyProgresses(t *testing.T) {
	f, w, a, v := scopeFixture(t)
	ctx := context.Background()
	f.now = a.Run.LeaseUntil.Add(time.Second)
	if e := f.s.Expire(ctx); e != nil {
		t.Fatal(e)
	}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	var old domain.Run
	for _, r := range snap.Runs {
		if r.ID == a.Run.ID {
			old = r
		}
	}
	if old.State != "lost" || old.Quiescent || old.ElapsedSeconds != old.GrantSeconds || snap.ScopeRequests[0].State != "waiting_stop" {
		t.Fatal("unknown tree did not preserve stop fence/full charge")
	}
	var decision domain.Decision
	for _, q := range snap.Decisions {
		if q.SourceKind == "scope_request" && q.SourceID == v.ID {
			decision = q
		}
	}
	if decision.ID == "" || !strings.Contains(decision.Evidence, "/api/v1/runs/"+old.ID+"/confirm-stopped") || !strings.Contains(decision.Evidence, "generation=") {
		t.Fatal("stop prerequisite lacks original Run proof interface", decision)
	}
	for _, evidence := range []string{"", "prose alone claims the tree stopped"} {
		if _, e := f.s.ResolveDecision(ctx, f.owner, ID("scope-false-stop"), decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion, Action: "confirm_run_stopped", Evidence: evidence}); e == nil {
			t.Fatal("offered action pretended to confirm the Run stop")
		}
	}
	if f.snapshot(t).Decisions[0].Status != "open" {
		t.Fatal("stop prerequisite resolved without proof")
	}
	if _, e := f.s.ConfirmStopped(ctx, f.owner, "scope-stale-stop", old.ID, domain.StopConfirmation{ExpectedVersion: old.Version - 1, Evidence: "Actual process-tree inspection"}); e == nil {
		t.Fatal("stale Run stop proof accepted")
	}
	if _, e := f.s.ResolveDecision(ctx, f.owner, "scope-stale-source", decision.ID, domain.DecisionResolveRequest{ExpectedVersion: decision.Version, ExpectedSourceVersion: decision.SourceVersion - 1, Action: "confirm_run_stopped", Evidence: "Actual process-tree inspection"}); e == nil {
		t.Fatal("stale scope source accepted")
	}
	if _, e := f.s.ConfirmStopped(ctx, f.owner, "scope-owner-stop", old.ID, domain.StopConfirmation{ExpectedVersion: old.Version, Evidence: "Actual process-tree inspection found all old owner processes stopped"}); e != nil {
		t.Fatal(e)
	}
	quiet := recoveryRun(t, f.snapshot(t), old.ID)
	if !quiet.Quiescent || quiet.Generation != v.Generation || quiet.State != "stopped" || quiet.ElapsedSeconds != old.GrantSeconds {
		t.Fatal("shared stop proof changed generation or refunded unknown spend", quiet)
	}
	if _, e := f.s.ConfirmStopped(ctx, f.owner, "scope-replayed-stale-stop", old.ID, domain.StopConfirmation{ExpectedVersion: old.Version, Evidence: "Actual process-tree inspection"}); e == nil {
		t.Fatal("old Run version proof reused")
	}
	var task domain.Task
	for _, q := range f.snapshot(t).Tasks {
		if q.ID == a.Task.ID {
			task = q
		}
	}
	w.Executor = resultExecutor(scopeResult(task, v))
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if after.ScopeRequests[0].State != "consumed" {
		t.Fatal("actual stop did not trigger ordinary assessment", after.Decisions)
	}
	for _, q := range after.Tasks {
		if q.ID == task.ID && (q.UsedSeconds != old.GrantSeconds || q.HoldReason != "" || q.State != "ready") {
			t.Fatal("scope refunded unknown execution or blocked progression")
		}
	}
	for _, d := range after.Decisions {
		if d.SourceKind == "scope_request" && d.SourceID == v.ID && d.Status == "open" {
			t.Fatal("consumed request left a manual stop decision")
		}
	}
	if _, e := f.s.Complete(ctx, f.node, "scope-discard-old-output", old.ID, domain.CompleteRequest{Generation: old.Generation, Status: "succeeded", Quiescent: true, Head: strings.Repeat("b", 40), ArtifactID: "invented"}); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).Candidates) != 0 {
		t.Fatal("old uncertain output gained authority after stop proof")
	}
}
