package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestRequirementWithdrawalRejectsEveryUnanchoredNewTask(t *testing.T) {
	for _, integrated := range []bool{false, true} {
		t.Run(fmt.Sprint(integrated), func(t *testing.T) {
			f, r, task := requirementFixture(t)
			if integrated {
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					task.State = "integrated"
					return tx.Put("task", task.ID, task)
				}); e != nil {
					t.Fatal(e)
				}
			}
			reviseRequirement(t, f, r, "withdraw")
			p := emptyRequirementPlan()
			q := taskProposal(task)
			if integrated {
				q.Key = "valid-compensation"
				p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{task.ID}})
			}
			q.Key = "unanchored-work"
			p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{}})
			before := f.snapshot(t)
			runRequirementPlan(t, f, p)
			if !reflect.DeepEqual(before.Tasks, f.snapshot(t).Tasks) || requirementDetail(t, f, r.ID).Changes[0].State != "blocked" {
				t.Fatal("withdrawal created arbitrary work or partially applied compensation")
			}
		})
	}
}

func TestRequirementOrdinaryPlanningIncludesAuthorizedAdditions(t *testing.T) {
	f, original, template := requirementFixture(t)
	ctx := context.Background()
	raw, e := f.s.CreateRequirement(ctx, f.owner, "new-authorized", domain.Requirement{Title: "Additional behavior", Body: "The new contract"})
	added := decodeTest[domain.Requirement](t, raw, e)
	p := emptyRequirementPlan()
	q := taskProposal(template)
	q.Key, q.RequirementIDs = "additional-work", []string{added.ID}
	p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{}})
	runRequirementPlan(t, f, p)
	if requirementDetail(t, f, added.ID).Changes[0].State != "applied" {
		t.Fatal("addition not admitted")
	}
	raw, e = f.s.SubmitRequirement(ctx, f.node, "node-proposal", domain.RequirementProposal{Title: "Not admitted", Body: "Needs Owner approval"})
	proposal := decodeTest[domain.Requirement](t, raw, e)
	d := *f.snapshot(t).PlanningDocument
	if containsPaths(d.RequirementIDs, []string{added.ID}) {
		t.Fatal("fixture did not use dynamic admission")
	}
	run := scheduleTest(t, f, d, "ordinary-facts")
	called := false
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		called = true
		_, input, ok := strings.Cut(prompt, " Input: ")
		var facts struct {
			Requirements []domain.Requirement `json:"requirements"`
		}
		if !ok || json.Unmarshal([]byte(input), &facts) != nil {
			t.Fatal("invalid planning facts")
		}
		byID := map[string]domain.Requirement{}
		for _, requirement := range facts.Requirements {
			byID[requirement.ID] = requirement
		}
		if len(byID) != 2 || byID[original.ID].Body != original.Body || byID[added.ID].Body != added.Body || byID[proposal.ID].ID != "" {
			t.Errorf("ordinary planning omitted authorized content or included a proposal: %+v", facts.Requirements)
		}
		output := domain.LeaderResult{Reply: "Discuss existing work", Ready: true, MissingItems: []string{}, Clarifications: []string{}, Tasks: []domain.TaskProposal{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}}
		b, _ := json.Marshal(output)
		return PlanningOutcome{Output: string(b), TreeStopped: true, ElapsedSeconds: 1}, nil
	})}
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if !called {
		t.Fatal("ordinary planner did not run")
	}
	for _, current := range f.snapshot(t).PlanningRuns {
		if current.ID == run.ID {
			if current.RequirementRevisions[added.ID] != 1 || len(current.RequirementRevisions) != 2 {
				t.Fatal("ordinary planning did not fence admitted requirements", current.RequirementRevisions)
			}
			return
		}
	}
	t.Fatal("planning run missing")
}

func TestRequirementImpactsIncludeLandedDependencyClosure(t *testing.T) {
	for _, omitDownstream := range []bool{false, true} {
		t.Run(fmt.Sprint(omitDownstream), func(t *testing.T) {
			f, r, task := requirementFixture(t)
			other := addUnrelatedGoalRequirement(t, f)
			dependent, verified, cancelled := task, task, task
			dependent.ID, dependent.State = "task_dependent", "integrated"
			dependent.RequirementIDs, dependent.RequirementRevisions = []string{other.ID}, map[string]int64{other.ID: 1}
			dependent.Dependencies = []string{task.ID}
			verified.ID, verified.State = "task_verified", "verified"
			verified.RequirementIDs, verified.RequirementRevisions = []string{other.ID}, map[string]int64{other.ID: 1}
			verified.Dependencies, verified.Verification = []string{dependent.ID}, &domain.VerificationSpec{Commit: strings.Repeat("a", 40)}
			cancelled.ID, cancelled.State, cancelled.Dependencies = "task_cancelled", "cancelled", []string{task.ID}
			cancelled.RequirementIDs = []string{other.ID}
			task.State = "integrated"
			if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				for _, item := range []domain.Task{task, dependent, verified, cancelled} {
					if e := tx.Put("task", item.ID, item); e != nil {
						return e
					}
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			change := reviseRequirement(t, f, r, "revise")
			if len(change.Impacts) != 3 {
				t.Fatalf("dependency closure lost landed work: %+v", change.Impacts)
			}
			for _, impact := range change.Impacts {
				if !impact.Integrated || impact.Direct != (impact.Task.ID == task.ID) || impact.Task.ID == cancelled.ID {
					t.Fatal("incorrect frozen impact", impact)
				}
			}
			p := emptyRequirementPlan()
			q := taskProposal(task)
			q.Key = "correct-contract"
			related := []string{task.ID}
			if !omitDownstream {
				related = append(related, dependent.ID, verified.ID)
				q.RequirementIDs = append([]string{r.ID}, other.ID)
			}
			p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: related})
			before := f.snapshot(t)
			runRequirementPlan(t, f, p)
			state := requirementDetail(t, f, r.ID).Changes[0].State
			if omitDownstream {
				if state != "blocked" || !reflect.DeepEqual(before.Tasks, f.snapshot(t).Tasks) {
					t.Fatal("landed downstream silently omitted")
				}
				return
			}
			if state != "applied" {
				t.Fatal("explicit downstream correction rejected", requirementDetail(t, f, r.ID).Changes[0])
			}
			for _, historical := range []domain.Task{task, dependent, verified, cancelled} {
				if !reflect.DeepEqual(historical, requirementTask(t, f, historical.ID)) {
					t.Fatal("historical task rewritten", historical.ID)
				}
			}
			if f.claim(t, "correct-downstream") == nil {
				t.Fatal("downstream corrective task not schedulable")
			}
		})
	}
}

func TestRequirementLargeImpactPlanUpdatesAtomically(t *testing.T) {
	f, r, template := requirementFixture(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 100; i++ {
			task := template
			task.ID = fmt.Sprintf("task_bulk_%03d", i)
			task.BudgetSeconds, task.MaxRunSeconds, task.UsedSeconds = 10, 5, 1
			task.Retries, task.Repairs = 1, 1
			if e := tx.Put("task", task.ID, task); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	change := reviseRequirement(t, f, r, "revise")
	if len(change.Impacts) != 101 {
		t.Fatal("incorrect bulk fixture")
	}
	before := f.snapshot(t)
	p := emptyRequirementPlan()
	for _, task := range before.Tasks {
		if containsPaths(task.RequirementIDs, []string{r.ID}) {
			p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: task.Version, Proposal: taskProposal(task)})
		}
	}
	runRequirementPlan(t, f, p)
	if current := requirementDetail(t, f, r.ID).Changes[0]; current.State != "applied" {
		t.Fatal("large fixed impact set cannot be replanned", current.State, current.Detail)
	}
	after := f.snapshot(t)
	if len(after.Tasks) != len(before.Tasks) {
		t.Fatal("replanning replaced task identities")
	}
	byID := map[string]domain.Task{}
	for _, task := range after.Tasks {
		byID[task.ID] = task
	}
	for _, old := range before.Tasks {
		current := byID[old.ID]
		if !containsPaths(old.RequirementIDs, []string{r.ID}) {
			if !reflect.DeepEqual(old, current) {
				t.Fatal("unaffected task changed")
			}
			continue
		}
		if current.RequirementRevisions[r.ID] != 2 || current.HoldReason != "" || current.UsedSeconds != old.UsedSeconds || current.Retries != old.Retries || current.Repairs != old.Repairs {
			t.Fatal("bulk revision lost fencing or cumulative accounting", old.ID)
		}
	}
}

func TestRequirementPlanRetainsFiniteBounds(t *testing.T) {
	f, r, task := requirementFixture(t)
	p := emptyRequirementPlan()
	for i := 0; i < 101; i++ {
		q := taskProposal(task)
		q.Key = fmt.Sprintf("new_%03d", i)
		p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{}})
	}
	raw, _ := json.Marshal(p)
	if _, e := decodeRequirementPlan(string(raw)); e == nil {
		t.Fatal("new task count is unbounded")
	}
	if _, e := decodeRequirementPlan(strings.Repeat(" ", (1<<20)+1)); e == nil {
		t.Fatal("output byte limit removed")
	}
	reviseRequirement(t, f, r, "revise")
	current := requirementTask(t, f, task.ID)
	p = emptyRequirementPlan()
	for i := 0; i < 101; i++ {
		p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: taskProposal(current)})
	}
	before := f.snapshot(t)
	runRequirementPlan(t, f, p)
	if !reflect.DeepEqual(before.Tasks, f.snapshot(t).Tasks) || requirementDetail(t, f, r.ID).Changes[0].State != "blocked" {
		t.Fatal("update count exceeded the fixed impact bound")
	}
}
