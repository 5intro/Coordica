package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func admittedBudgetFixture(t *testing.T) (*serviceFixture, domain.PlanningDocument) {
	t.Helper()
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "initial-budget-plan")
	if err := (&PlanningWorker{Service: f.s, Executor: resultExecutor(result)}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f, *f.snapshot(t).PlanningDocument
}

func goalBudgetRequest(d domain.PlanningDocument) domain.GoalBudgetRequest {
	return domain.GoalBudgetRequest{ExpectedGoalVersion: d.Version, ExpectedAuthorizationVersion: d.AuthorizationVersion,
		BudgetSeconds: d.BudgetSeconds + 10000, Reason: "Owner grants a new finite allowance after diagnosed failures"}
}

func TestGoalBudgetIncreasePreservesCurrentGrantAndAllAuthority(t *testing.T) {
	f, d := admittedBudgetFixture(t)
	a := f.claim(t, "active-before-increase")
	if a == nil {
		t.Fatal("fixture failed to issue work")
	}
	before := f.snapshot(t)
	in := goalBudgetRequest(d)
	raw, err := IncreaseGoalBudget(context.Background(), f.s, f.owner, "increase", in)
	state := decodeTest[domain.GoalBudgetState](t, raw, err)
	after := f.snapshot(t)
	expected := *before.PlanningDocument
	expected.BudgetSeconds = in.BudgetSeconds
	if !reflect.DeepEqual(expected, *after.PlanningDocument) || !reflect.DeepEqual(before.Tasks, after.Tasks) ||
		!reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.Project, after.Project) || state.Version != 1 {
		t.Fatal("allowance edit changed goal content, task counters, Run deadline or project state")
	}
	replay, err := IncreaseGoalBudget(context.Background(), f.s, f.owner, "increase", in)
	if err != nil || string(replay) != string(raw) {
		t.Fatal("allowance replay changed receipt", err)
	}
	if _, err = IncreaseGoalBudget(context.Background(), f.s, f.owner, "stale", in); err == nil {
		t.Fatal("stale allowance revision was accepted")
	}
	f.now = f.now.Add(time.Duration(a.Run.GrantSeconds+1) * time.Second)
	hb, err := f.s.Heartbeat(context.Background(), f.node, a.Run.ID, domain.HeartbeatRequest{
		Generation: a.Run.Generation, ElapsedSeconds: a.Run.GrantSeconds,
	})
	if err != nil || !hb.Cancel || hb.Reason != "budget" {
		t.Fatal("new goal allowance extended an already issued finite Run", hb, err)
	}
	current, err := ReadGoalBudget(context.Background(), f.s, f.owner)
	s := f.snapshot(t)
	if err != nil || current.Version != 1 || current.UsedSeconds != s.PlanningDocument.UsedSeconds ||
		current.ReservedSeconds != s.PlanningDocument.ReservedSeconds {
		t.Fatal("current statistics were replaced by stale approval-time statistics", current, err)
	}
}

func TestGoalBudgetIncreaseRejectsInvalidOrUnauthorizedRequests(t *testing.T) {
	for _, scenario := range []string{"node", "chat", "zero", "unbounded", "decrease", "wrong goal", "wrong authority", "reason", "not admitted"} {
		t.Run(scenario, func(t *testing.T) {
			f, d := admittedBudgetFixture(t)
			in, actor := goalBudgetRequest(d), f.owner
			switch scenario {
			case "node":
				actor = f.node
			case "chat":
				actor = Actor{ID: "chat", Role: "chat"}
			case "zero":
				in.BudgetSeconds = 0
			case "unbounded":
				in.BudgetSeconds = domain.MaxTaskBudgetSeconds + 1
			case "decrease":
				in.BudgetSeconds = d.BudgetSeconds - 1
			case "wrong goal":
				in.ExpectedGoalVersion++
			case "wrong authority":
				in.ExpectedAuthorizationVersion++
			case "reason":
				in.Reason = strings.Repeat("x", 2049)
			case "not admitted":
				if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					d.Status = "planning"
					return tx.Put("planning_document", "current", d)
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := f.snapshot(t)
			if _, err := IncreaseGoalBudget(context.Background(), f.s, actor, "invalid", in); err == nil {
				t.Fatal("invalid increase admitted")
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed allowance update partially mutated state or audit")
			}
		})
	}
}

func TestGoalBudgetIncreaseAndClaimSerializeAccounting(t *testing.T) {
	f, d := admittedBudgetFixture(t)
	var wg sync.WaitGroup
	var claim json.RawMessage
	var claimErr, budgetErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, budgetErr = IncreaseGoalBudget(context.Background(), f.s, f.owner, "concurrent-budget", goalBudgetRequest(d))
	}()
	go func() {
		defer wg.Done()
		claim, claimErr = f.s.Claim(context.Background(), f.node, "concurrent-claim", domain.ClaimRequest{
			AssignmentContextVersion: domain.AssignmentContextVersion,
			Capabilities:             []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}},
		}, strings.Repeat("a", 40))
	}()
	wg.Wait()
	a := decodeTest[*domain.Assignment](t, claim, claimErr)
	if budgetErr != nil || a == nil {
		t.Fatal("concurrent increase or dispatch failed", budgetErr, a)
	}
	s := f.snapshot(t)
	if s.PlanningDocument.BudgetSeconds != d.BudgetSeconds+10000 || s.PlanningDocument.UsedSeconds != d.UsedSeconds ||
		s.PlanningDocument.ReservedSeconds != a.Run.GrantSeconds || a.Run.GoalVersion != d.Version ||
		a.Run.GoalAuthorizationVersion != d.AuthorizationVersion {
		t.Fatal("allowance increase raced reservation or changed authority", s.PlanningDocument, a)
	}
}

func TestGoalBudgetIncreaseDoesNotClearHoldsOrAuthorizeANewGoal(t *testing.T) {
	f, d := admittedBudgetFixture(t)
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		tasks, err := store.List[domain.Task](tx, "task")
		if err != nil {
			return err
		}
		for _, task := range tasks {
			task.HoldReason = "budget"
			if err = tx.Put("task", task.ID, task); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := IncreaseGoalBudget(ctx, f.s, f.owner, "held-budget", goalBudgetRequest(d)); err != nil {
		t.Fatal(err)
	}
	if a := f.claim(t, "still-held"); a != nil {
		t.Fatal("allowance edit resumed a held task", a)
	}
	changed := *f.snapshot(t).PlanningDocument
	changed.Objective = "New goal needs its own authorization"
	raw, err := f.s.SavePlanning(ctx, f.owner, "new-goal", domain.PlanningSaveRequest{ExpectedVersion: changed.Version, Document: changed})
	newGoal := decodeTest[domain.PlanningDocument](t, raw, err)
	state, err := ReadGoalBudget(ctx, f.s, f.owner)
	if err != nil || state.Version != 0 || state.GoalVersion != newGoal.Version {
		t.Fatal("new goal inherited old allowance revision", state, err)
	}
	in := goalBudgetRequest(newGoal)
	if _, err = IncreaseGoalBudget(ctx, f.s, f.owner, "not-authorized-new-goal", in); err == nil {
		t.Fatal("allowance approval admitted a changed goal")
	}
}
