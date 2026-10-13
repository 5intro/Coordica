package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestProjectStatisticsBeyondLegacyCapAllowLongRunAtomicSettlement(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.TotalBudgetSeconds = 1
		p.UsedSeconds = 1000000
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.TaskAction(ctx, f.owner, "pause-fixture", f.task.ID, "pause", domain.VersionRequest{ExpectedVersion: f.task.Version}); e != nil {
		t.Fatal(e)
	}
	raw, e := f.s.CreateTask(ctx, f.owner, "long-task", domain.Task{Title: "Long task", Description: "Several hours of work", Acceptance: "bounded execution", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 48 * 3600, MaxRunSeconds: 12 * 3600, MaxRetries: 1})
	task := decodeTest[domain.Task](t, raw, e)
	if _, e = f.s.TaskAction(ctx, f.owner, "approve-long", task.ID, "approve", domain.VersionRequest{ExpectedVersion: task.Version}); e != nil {
		t.Fatal(e)
	}
	raw, e = f.s.Claim(ctx, f.node, "long", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true}}}, strings.Repeat("a", 40))
	a := decodeTest[*domain.Assignment](t, raw, e)
	if a == nil || a.Run.GrantSeconds != 12*3600 || a.Run.Deadline.Sub(a.Run.StartedAt) != 12*time.Hour {
		t.Fatal("legacy project/hour ceiling", a)
	}
	if a.Run.LeaseUntil.Sub(a.Run.StartedAt) != f.s.Lease {
		t.Fatal("deadline confused with lease")
	}
	p := f.snapshot(t).Project
	if p.UsedSeconds != 1000000 || p.ReservedSeconds != a.Run.GrantSeconds {
		t.Fatal("reservation accounting", p)
	}
	request := domain.CompleteRequest{Generation: a.Run.Generation, Quiescent: true, ElapsedSeconds: 3, Status: "failed"}
	if _, e := f.s.Complete(ctx, f.node, "settle", a.Run.ID, request); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t).Project
	if after.ReservedSeconds != 0 || after.UsedSeconds != 1000003 {
		t.Fatal("long run settlement", after)
	}
	if _, e := f.s.Complete(ctx, f.node, "settle", a.Run.ID, request); e != nil {
		t.Fatal(e)
	}
	if got := f.snapshot(t).Project; got.UsedSeconds != after.UsedSeconds || got.ReservedSeconds != 0 {
		t.Fatal("duplicate settlement changed statistics")
	}
}
func TestPlanningAndRepeatedConsultationsBeyondLegacyLifetimeSpending(t *testing.T) {
	f, d, result := planningFixture(t, true)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.TotalBudgetSeconds = 1
		p.UsedSeconds = 1000000
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	scheduleTest(t, f, d, "plan-beyond-cap")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if got := f.snapshot(t); len(got.Tasks) != 2 || got.PlanningRuns[0].State != "succeeded" || got.Project.UsedSeconds <= 1000000 {
		t.Fatal("project gate blocked goal-authorized planning")
	}
	a, _ := chatFixture(t, f)
	// A legacy consultation cap is statistics only, including after goal changes.
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		return tx.Put("consultation_account", "project", domain.ConsultationAccount{ID: "project", BudgetSeconds: 1, UsedSeconds: 1000000})
	}); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"advice-one", "advice-two"} {
		q := consultTest(t, f, a, d.Version, key, "consultation")
		w.Executor = planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
			return PlanningOutcome{TreeStopped: true, ElapsedSeconds: 2, Output: `{"reply":"still consulting"}`}, nil
		})
		if e := w.Tick(ctx); e != nil {
			t.Fatal(e)
		}
		if getConversation(t, f, q.ID).Status != "succeeded" {
			t.Fatal("consultation acquired hidden lifetime cap")
		}
	}
	if f.snapshot(t).ConsultationAccount.UsedSeconds != 1000004 {
		t.Fatal("consult history reset")
	}
}

func TestLongTaskConfigurationStillRejectsUnboundedDurationAndRetries(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	valid := domain.Task{Title: "Long", Description: "Hours", Acceptance: "bounded", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: domain.MaxTaskBudgetSeconds, MaxRunSeconds: domain.MaxRunDurationSeconds, MaxRetries: 2, MaxRepairs: 2}
	if _, e := f.s.CreateTask(ctx, f.owner, "finite-max", valid); e != nil {
		t.Fatal(e)
	}
	for _, limit := range []string{"task", "run", "retries", "repairs"} {
		bad := valid
		switch limit {
		case "task":
			bad.BudgetSeconds++
		case "run":
			bad.MaxRunSeconds++
		case "retries":
			bad.MaxRetries++
		case "repairs":
			bad.MaxRepairs++
		}
		if _, e := f.s.CreateTask(ctx, f.owner, "invalid-"+limit, bad); e == nil {
			t.Fatal("unbounded config", limit)
		}
	}
}
