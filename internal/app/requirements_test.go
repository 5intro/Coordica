package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func requirementFixture(t *testing.T) (*serviceFixture, domain.Requirement, domain.Task) {
	t.Helper()
	f, d, result := planningFixture(t, true)
	scheduleTest(t, f, d, "admit")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	snap := f.snapshot(t)
	for _, task := range snap.Tasks {
		if len(task.RequirementIDs) > 0 {
			return f, snap.Requirements[0], task
		}
	}
	t.Fatal("task missing")
	return nil, domain.Requirement{}, domain.Task{}
}
func requirementTask(t *testing.T, f *serviceFixture, id string) domain.Task {
	t.Helper()
	for _, v := range f.snapshot(t).Tasks {
		if v.ID == id {
			return v
		}
	}
	t.Fatal("missing task", id)
	return domain.Task{}
}
func reviseRequirement(t *testing.T, f *serviceFixture, r domain.Requirement, kind string) domain.RequirementChange {
	t.Helper()
	in := domain.RequirementEdit{ExpectedVersion: r.Version, Reason: "Owner changed the expected behavior"}
	if kind == "revise" {
		in.Title = r.Title + " revised"
		in.Body = r.Body + " with new acceptance"
	}
	b, e := f.s.EditRequirement(context.Background(), f.owner, ID("edit_"), r.ID, kind, in)
	return decodeTest[domain.RequirementChange](t, b, e)
}
func taskProposal(t domain.Task) domain.TaskProposal {
	return domain.TaskProposal{Key: t.ID, Title: t.Title, Objective: t.Description, Acceptance: t.Acceptance, RequirementIDs: t.RequirementIDs, Paths: t.AllowedPaths, Dependencies: t.Dependencies, HotResources: t.HotResources, BudgetSeconds: t.BudgetSeconds, MaxRunSeconds: t.MaxRunSeconds, MaxRetries: t.MaxRetries, MaxRepairs: t.MaxRepairs, TestRunID: t.TestRunID}
}
func requirementExecutor(plan domain.RequirementPlan) PlanningExecutor {
	return planningExecFunc(func(_ context.Context, _ string, prompt string) (PlanningOutcome, error) {
		if !strings.HasPrefix(prompt, "COORDICA_REQUIREMENT_CHANGE_V1\n") {
			panic("wrong planning protocol")
		}
		b, _ := json.Marshal(plan)
		return PlanningOutcome{Output: string(b), TreeStopped: true, ElapsedSeconds: 1}, nil
	})
}
func emptyRequirementPlan() domain.RequirementPlan {
	return domain.RequirementPlan{Reply: "Replanned against the changed requirement", Ready: true, Updates: []domain.RequirementTaskUpdate{}, Tasks: []domain.RequirementNewTask{}, MajorDecisions: []domain.MajorDecisionProposal{}}
}
func runRequirementPlan(t *testing.T, f *serviceFixture, p domain.RequirementPlan) {
	t.Helper()
	w := &PlanningWorker{Service: f.s, Executor: requirementExecutor(p)}
	if e := w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func requirementDetail(t *testing.T, f *serviceFixture, id string) domain.RequirementDetail {
	t.Helper()
	d, e := f.s.RequirementDetail(context.Background(), f.owner, id, 0)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestRequirementRevisionRaceHistoryReplayAndMetadataLinks(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	b, e := f.s.CreateRequirement(ctx, f.owner, "other", domain.Requirement{Title: "Other", Body: "Other work"})
	other := decodeTest[domain.Requirement](t, b, e)
	_, e = f.s.LinkRequirements(ctx, f.owner, "link", r.ID, domain.RequirementLinkRequest{ExpectedVersion: r.Version, OtherID: other.ID, OtherExpectedVersion: other.Version})
	if e != nil {
		t.Fatal(e)
	}
	r = requirementDetail(t, f, r.ID).Requirement
	if e = f.s.DB.View(ctx, func(tx *store.Tx) error { return requirementTaskGate(tx, task) }); e != nil {
		t.Fatal("metadata links invalidated work", e)
	}
	in := domain.RequirementEdit{ExpectedVersion: r.Version, Title: "Changed", Body: "Changed contract", Reason: "Changed needs"}
	var wg sync.WaitGroup
	success := make(chan []byte, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := f.s.EditRequirement(ctx, f.owner, ID("race_"), r.ID, "revise", in)
			if e == nil {
				success <- b
			}
		}()
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatal("expected exactly one optimistic writer", len(success))
	}
	d := requirementDetail(t, f, r.ID)
	if len(d.Revisions) != 2 || d.Requirement.ContentRevision != 2 || d.Revisions[1].Requirement.Body != r.Body {
		t.Fatal("missing immutable history", d)
	}
	in.ExpectedVersion = d.Requirement.Version
	in.Title = "Third"
	b, e = f.s.EditRequirement(ctx, f.owner, "same-revision", r.ID, "revise", in)
	first := decodeTest[domain.RequirementChange](t, b, e)
	b, e = f.s.EditRequirement(ctx, f.owner, "same-revision", r.ID, "revise", in)
	again := decodeTest[domain.RequirementChange](t, b, e)
	if first.ID != again.ID || requirementDetail(t, f, r.ID).Requirement.ContentRevision != 3 {
		t.Fatal("replay wrote another revision")
	}
	if _, e = f.s.EditRequirement(ctx, f.node, "forbidden", r.ID, "withdraw", domain.RequirementEdit{ExpectedVersion: 4, Reason: "no"}); e == nil {
		t.Fatal("node edited Owner requirement")
	}
}
func TestRequirementEditStopsOldRunBeforeReplanAndRejectsLateResult(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	assignment := f.claim(t, "old-work")
	if assignment == nil || assignment.Task.ID != task.ID {
		t.Fatal("claim")
	}
	change := reviseRequirement(t, f, r, "revise")
	if change.State != "waiting_stop" {
		t.Fatal(change.State)
	}
	hb, e := f.s.Heartbeat(ctx, f.node, assignment.Run.ID, domain.HeartbeatRequest{Generation: assignment.Run.Generation, ElapsedSeconds: 2})
	if e != nil || !hb.Cancel {
		t.Fatal("old process not cancelled", hb, e)
	}
	before := len(f.snapshot(t).PlanningRuns)
	w := &PlanningWorker{Service: f.s, Executor: requirementExecutor(emptyRequirementPlan())}
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if len(f.snapshot(t).PlanningRuns) != before {
		t.Fatal("planned before stop proof")
	}
	if _, e = f.s.Complete(ctx, f.node, "old-stopped", assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "completed", ElapsedSeconds: 3, Quiescent: true}); e != nil {
		t.Fatal(e)
	}
	old := requirementTask(t, f, task.ID)
	p := emptyRequirementPlan()
	q := taskProposal(old)
	q.Objective = "Implement revised requirement"
	p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: old.ID, ExpectedVersion: old.Version, Proposal: q})
	runRequirementPlan(t, f, p)
	revised := requirementTask(t, f, task.ID)
	if revised.HoldReason != "" || revised.RequirementRevisions[r.ID] != 2 || revised.UsedSeconds != old.UsedSeconds || revised.Retries != old.Retries {
		t.Fatal("invalid replan", revised)
	}
	next := f.claim(t, "new-work")
	if next == nil || next.Run.ID == assignment.Run.ID || next.Run.RequirementRevisions[r.ID] != 2 {
		t.Fatal("new revision not pinned")
	}
	frozen := f.snapshot(t)
	_, e = f.s.Complete(ctx, f.node, "late-old", assignment.Run.ID, domain.CompleteRequest{Generation: assignment.Run.Generation, Status: "completed", ElapsedSeconds: 30, Quiescent: true})
	if e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if !reflect.DeepEqual(frozen.Tasks, after.Tasks) || !reflect.DeepEqual(frozen.Candidates, after.Candidates) || !reflect.DeepEqual(frozen.Project, after.Project) {
		t.Fatal("late result changed new execution")
	}
}
func TestRequirementWithdrawSharedTaskAndPreserveCompletedHistory(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "exclusive", true: "shared"}[shared], func(t *testing.T) {
			f, r, task := requirementFixture(t)
			ctx := context.Background()
			var other domain.Requirement
			if shared {
				b, e := f.s.CreateRequirement(ctx, f.owner, "other", domain.Requirement{Title: "Shared", Body: "Shared work"})
				other = decodeTest[domain.Requirement](t, b, e)
				if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
					task.RequirementIDs = append(task.RequirementIDs, other.ID)
					task.RequirementRevisions[other.ID] = 1
					var change domain.RequirementChange
					if e := tx.Get("requirement_change", requirementRevisionID(other.ID, 1), &change); e != nil {
						return e
					}
					change.State = "superseded"
					if e := tx.Put("requirement_change", change.ID, change); e != nil {
						return e
					}
					return tx.Put("task", task.ID, task)
				}); e != nil {
					t.Fatal(e)
				}
			}
			reviseRequirement(t, f, r, "withdraw")
			current := requirementTask(t, f, task.ID)
			if !shared {
				if current.State != "cancelled" {
					t.Fatal("exclusive work not cancelled")
				}
				runRequirementPlan(t, f, emptyRequirementPlan())
			} else {
				if current.State == "cancelled" {
					t.Fatal("shared work cancelled")
				}
				q := taskProposal(current)
				q.RequirementIDs = []string{other.ID}
				p := emptyRequirementPlan()
				p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: q})
				// The other requirement is already within this test's authorized goal.
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					d, e := goalDocument(tx)
					if e != nil {
						return e
					}
					d.RequirementIDs = append(d.RequirementIDs, other.ID)
					return tx.Put("planning_document", "current", d)
				}); e != nil {
					t.Fatal(e)
				}
				runRequirementPlan(t, f, p)
				current = requirementTask(t, f, task.ID)
				if current.HoldReason != "" || !sameStrings(current.RequirementIDs, []string{other.ID}) {
					t.Fatal("shared replan failed", current)
				}
			}
			detail := requirementDetail(t, f, r.ID)
			if detail.Requirement.State != "withdrawn" || len(detail.Revisions) != 2 {
				t.Fatal("history lost")
			}
		})
	}
}
func TestRequirementIntegratedWithdrawalRequiresCompensationThroughNormalGates(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error { task.State = "integrated"; return tx.Put("task", task.ID, task) }); e != nil {
		t.Fatal(e)
	}
	reviseRequirement(t, f, r, "withdraw")
	runRequirementPlan(t, f, emptyRequirementPlan())
	d := requirementDetail(t, f, r.ID)
	if d.Changes[0].State != "blocked" {
		t.Fatal("integrated change silently applied")
	}
	b, e := f.s.ReassessRequirement(ctx, f.owner, "reevaluate", r.ID, domain.VersionRequest{ExpectedVersion: d.Changes[0].Version})
	decodeTest[domain.RequirementChange](t, b, e)
	p := emptyRequirementPlan()
	q := taskProposal(task)
	q.Key = "compensate"
	q.Title = "Compensate withdrawn work"
	p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{task.ID}})
	runRequirementPlan(t, f, p)
	d = requirementDetail(t, f, r.ID)
	if d.Changes[0].State != "applied" {
		t.Fatal(d.Changes[0])
	}
	if requirementTask(t, f, task.ID).State != "integrated" {
		t.Fatal("history rewritten")
	}
	assignment := f.claim(t, "compensation")
	if assignment == nil || assignment.Task.ID == task.ID || assignment.Run.RequirementRevisions[r.ID] != 2 {
		t.Fatal("compensation not schedulable")
	}
}
func TestRequirementReplanAtomicScopeBudgetAndCounters(t *testing.T) {
	for _, kind := range []string{"scope", "budget", "retries", "shared", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			f, r, task := requirementFixture(t)
			reviseRequirement(t, f, r, "revise")
			current := requirementTask(t, f, task.ID)
			q := taskProposal(current)
			switch kind {
			case "scope":
				q.Paths = []string{"outside/**"}
			case "budget":
				q.BudgetSeconds++
			case "retries":
				q.MaxRetries++
			case "shared":
				q.RequirementIDs = []string{}
			case "cycle":
				q.Dependencies = []string{task.ID}
			}
			p := emptyRequirementPlan()
			p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: q})
			before := f.snapshot(t)
			runRequirementPlan(t, f, p)
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Candidates, after.Candidates) || requirementDetail(t, f, r.ID).Changes[0].State != "blocked" {
				t.Fatal("invalid batch partially applied")
			}
		})
	}
}
func TestRequirementPausedAndOldPlanningResultFence(t *testing.T) {
	f, r, _ := requirementFixture(t)
	ctx := context.Background()
	reviseRequirement(t, f, r, "revise")
	snap := f.snapshot(t)
	_, e := f.s.ProjectAction(ctx, f.owner, "pause-req", "pause", domain.VersionRequest{ExpectedVersion: snap.Project.Version})
	if e != nil {
		t.Fatal(e)
	}
	w := &PlanningWorker{Service: f.s, Executor: requirementExecutor(emptyRequirementPlan())}
	before := f.snapshot(t)
	if e = w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	after := f.snapshot(t)
	if len(after.PlanningRuns) != len(before.PlanningRuns) || !after.Project.Paused {
		t.Fatal("paused change dispatched")
	}
	// A result captured before this content edit can never derive a current task.
	if e = f.s.DB.View(ctx, func(tx *store.Tx) error {
		if requirementPlanCurrent(tx, domain.PlanningRun{RequirementRevisions: map[string]int64{r.ID: 1}}) {
			t.Fatal("stale planning result allowed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestRequirementPublicationAndVerificationAreFenced(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		return tx.Put("candidate", "busy", domain.Candidate{ID: "busy", TaskID: task.ID, State: "landing"})
	}); e != nil {
		t.Fatal(e)
	}
	before := f.snapshot(t)
	_, e := f.s.EditRequirement(ctx, f.owner, "during-landing", r.ID, "withdraw", domain.RequirementEdit{ExpectedVersion: r.Version, Reason: "withdraw"})
	if e == nil {
		t.Fatal("raced publication")
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("publication rejection wrote data")
	}
	if e = f.s.DB.Update(ctx, func(tx *store.Tx) error {
		if e := tx.Put("candidate", "busy", domain.Candidate{ID: "busy", TaskID: task.ID, State: "integrated"}); e != nil {
			return e
		}
		task.Verification = &domain.VerificationSpec{}
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	reviseRequirement(t, f, r, "revise")
	if requirementTask(t, f, task.ID).State != "cancelled" {
		t.Fatal("stale verification still runnable")
	}
}

func TestRequirementAdditionMergesUnissuedOrQueuesDependentFollowup(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "followup"}[running], func(t *testing.T) {
			f, r, task := requirementFixture(t)
			ctx := context.Background()
			if running {
				if f.claim(t, "running") == nil {
					t.Fatal("claim")
				}
				task = requirementTask(t, f, task.ID)
			}
			b, e := f.s.CreateRequirement(ctx, f.owner, "new-requirement", domain.Requirement{Title: "Related extension", Body: "Additional behavior"})
			added := decodeTest[domain.Requirement](t, b, e)
			// A raw Task cannot admit the new requirement before Leader assessment.
			if e = f.s.DB.View(ctx, func(tx *store.Tx) error {
				d, e := goalDocument(tx)
				if e != nil {
					return e
				}
				if goalAllowsRequirement(tx, d, added.ID) {
					t.Fatal("unassessed input granted authority")
				}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			p := emptyRequirementPlan()
			q := taskProposal(task)
			q.RequirementIDs = []string{r.ID, added.ID}
			q.Objective = "Build source plus related extension"
			if running {
				q.Key = "followup"
				q.Dependencies = []string{task.ID}
				p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{task.ID}})
			} else {
				p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: task.Version, Proposal: q})
			}
			runRequirementPlan(t, f, p)
			detail := requirementDetail(t, f, added.ID)
			if len(detail.Changes) != 1 || detail.Changes[0].State != "applied" {
				t.Fatal("addition not applied", detail.Changes)
			}
			if running {
				if !reflect.DeepEqual(task, requirementTask(t, f, task.ID)) {
					t.Fatal("live contract changed")
				}
				if f.claim(t, "must-wait") != nil {
					t.Fatal("followup ran before dependency")
				}
			} else {
				updated := requirementTask(t, f, task.ID)
				if !sameStrings(updated.RequirementIDs, []string{r.ID, added.ID}) {
					t.Fatal("unissued task not merged")
				}
				a := f.claim(t, "merged-claim")
				if a == nil || a.Task.ID != task.ID {
					t.Fatal("merged work not schedulable")
				}
			}
		})
	}
}
func TestRequirementFollowupCannotOmitRunningDependency(t *testing.T) {
	f, _, task := requirementFixture(t)
	if f.claim(t, "running") == nil {
		t.Fatal("claim")
	}
	ctx := context.Background()
	b, e := f.s.CreateRequirement(ctx, f.owner, "add", domain.Requirement{Title: "Followup", Body: "Related work"})
	r := decodeTest[domain.Requirement](t, b, e)
	q := taskProposal(task)
	q.Key = "follow"
	q.RequirementIDs = []string{r.ID}
	q.Dependencies = []string{}
	p := emptyRequirementPlan()
	p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{task.ID}})
	before := f.snapshot(t)
	runRequirementPlan(t, f, p)
	after := f.snapshot(t)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || requirementDetail(t, f, r.ID).Changes[0].State != "blocked" {
		t.Fatal("followup bypassed original execution")
	}
}
func TestRequirementConcurrentOwnerHoldAndCumulativeLimitsSurviveReplanning(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		task.HoldReason = "human_decision"
		task.UsedSeconds = 11
		task.Retries = task.MaxRetries
		task.Repairs = task.MaxRepairs
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	reviseRequirement(t, f, r, "revise")
	r = requirementDetail(t, f, r.ID).Requirement
	reviseRequirement(t, f, r, "revise")
	current := requirementTask(t, f, task.ID)
	p := emptyRequirementPlan()
	p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: taskProposal(current)})
	runRequirementPlan(t, f, p)
	updated := requirementTask(t, f, task.ID)
	if updated.UsedSeconds != 11 || updated.Retries != task.MaxRetries || updated.Repairs != task.MaxRepairs || updated.HoldReason != "human_decision" || updated.RequirementRevisions[r.ID] != 3 {
		t.Fatal("change reset accounting or an independent hold", updated)
	}
}
func TestRequirementLeaderOutputDuringAnotherRevisionIsHistorical(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	reviseRequirement(t, f, r, "revise")
	current := requirementTask(t, f, task.ID)
	p := emptyRequirementPlan()
	p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: taskProposal(current)})
	w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
		latest := requirementDetail(t, f, r.ID).Requirement
		reviseRequirement(t, f, latest, "revise")
		raw, _ := json.Marshal(p)
		return PlanningOutcome{TreeStopped: true, Output: string(raw), ElapsedSeconds: 1}, nil
	})}
	if e := w.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	updated := requirementTask(t, f, task.ID)
	if updated.RequirementRevisions[r.ID] != 1 || updated.HoldReason != "requirement_changed" {
		t.Fatal("late Leader result applied", updated)
	}
	detail := requirementDetail(t, f, r.ID)
	if detail.Changes[1].State != "superseded" || detail.Requirement.ContentRevision != 3 {
		t.Fatal("old assessment not retired", detail.Changes)
	}
}
func TestRequirementStrictResultRejectsMissingNullAndUnknownFields(t *testing.T) {
	p := emptyRequirementPlan()
	raw, _ := json.Marshal(p)
	if _, e := decodeRequirementPlan(string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"reply":"ok","ready":true,"updates":[],"tasks":[]}`, `{"reply":"ok","ready":true,"updates":null,"tasks":[],"major_decisions":[]}`, `{"reply":"ok","ready":true,"updates":[],"tasks":[],"major_decisions":[],"command":"merge"}`, `{"reply":"ok","reply":"again","ready":true,"updates":[],"tasks":[],"major_decisions":[]}`} {
		if _, e := decodeRequirementPlan(raw); e == nil {
			t.Fatal("ambiguous result accepted", raw)
		}
	}
}

func TestRequirementDraftEditNeedsNoExecutionAssessmentAndHistoryIsPaged(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	b, e := f.s.CreateRequirement(ctx, f.owner, "draft", domain.Requirement{Title: "Draft", Body: "Original"})
	r := decodeTest[domain.Requirement](t, b, e)
	for i := 0; i < 23; i++ {
		change := reviseRequirement(t, f, r, "revise")
		if change.State != "applied" {
			t.Fatal("unplanned draft required execution assessment")
		}
		r = requirementDetail(t, f, r.ID).Requirement
	}
	latest := requirementDetail(t, f, r.ID)
	if len(latest.Revisions) != 20 || latest.Before != 5 {
		t.Fatal("unbounded or invalid history", len(latest.Revisions), latest.Before)
	}
	older, e := f.s.RequirementDetail(ctx, f.owner, r.ID, latest.Before)
	if e != nil || len(older.Revisions) != 4 || older.Before != 0 || older.Revisions[3].Requirement.Body != "Original" {
		t.Fatal("history pagination lost original", older, e)
	}
}

func TestRequirementEmptyCenterAndRetiredAssessmentsKeepStopBarriers(t *testing.T) {
	db, e := store.Open(t.TempDir() + "/empty.db")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db, t.TempDir())
	ctx := context.Background()
	w := &PlanningWorker{Service: s}
	if e = w.queueRequirementChanges(ctx); e != nil {
		t.Fatal("unused center needs no project", e)
	}
	if e = s.DB.Update(ctx, func(tx *store.Tx) error {
		change := domain.RequirementChange{ID: "change", State: "applied"}
		if e := tx.Put("requirement_change", change.ID, change); e != nil {
			return e
		}
		for _, tc := range []struct {
			id, kind string
			stopped  bool
		}{{"old", "invalid_plan", true}, {"live", "invalid_plan", false}, {"stop", "unknown_live", true}} {
			plan := domain.PlanningRun{ID: tc.id, RequirementChangeID: change.ID, TreeStopped: tc.stopped, Settled: true}
			if e := tx.Put("planning_run", plan.ID, plan); e != nil {
				return e
			}
			decision := domain.Decision{ID: tc.id, Version: 1, SourceKind: "planning_run", SourceID: plan.ID, Status: "open", Kind: tc.kind}
			if e := tx.Put("decision", decision.ID, decision); e != nil {
				return e
			}
		}
		return s.retireRequirementDecisions(tx)
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.DB.View(ctx, func(tx *store.Tx) error {
		for _, id := range []string{"old", "live", "stop"} {
			var d domain.Decision
			if e := tx.Get("decision", id, &d); e != nil {
				return e
			}
			if (d.Status == "superseded") != (id == "old") {
				t.Fatal("incorrect retirement", d)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func addUnrelatedGoalRequirement(t *testing.T, f *serviceFixture) domain.Requirement {
	t.Helper()
	r := domain.Requirement{ID: "req_unrelated", Version: 1, ContentRevision: 1, State: "active", Title: "Independent work", Body: "Not part of the edited task", CreatedAt: f.now}
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if e := tx.Put("requirement", r.ID, r); e != nil {
			return e
		}
		d, e := goalDocument(tx)
		if e != nil {
			return e
		}
		d.RequirementIDs = append(d.RequirementIDs, r.ID)
		return tx.Put("planning_document", "current", d)
	}); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRequirementChangeRejectsUnrelatedGoalRequirementsAtomically(t *testing.T) {
	for _, kind := range []string{"revise-update", "revise-new", "withdraw-new"} {
		t.Run(kind, func(t *testing.T) {
			f, r, task := requirementFixture(t)
			other := addUnrelatedGoalRequirement(t, f)
			operation := "revise"
			if kind == "withdraw-new" {
				operation = "withdraw"
			}
			reviseRequirement(t, f, r, operation)
			current := requirementTask(t, f, task.ID)
			p := emptyRequirementPlan()
			q := taskProposal(current)
			if operation == "revise" {
				p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: q})
			}
			if kind == "revise-update" {
				p.Updates[0].Proposal.RequirementIDs = append([]string{r.ID}, other.ID)
			} else {
				q.Key = "extra"
				q.Title = "Additional work"
				q.Objective = "Changed requirement plus unrelated work"
				q.RequirementIDs = []string{r.ID, other.ID}
				p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{}})
			}
			before := f.snapshot(t)
			runRequirementPlan(t, f, p)
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || requirementDetail(t, f, r.ID).Changes[0].State != "blocked" {
				t.Fatal("unrelated requirement reshaped work or partially applied")
			}
		})
	}
}
func TestRequirementSharedCompensationRetainsRecordedContext(t *testing.T) {
	f, r, task := requirementFixture(t)
	other := addUnrelatedGoalRequirement(t, f)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		task.RequirementIDs = append(task.RequirementIDs, other.ID)
		task.RequirementRevisions[other.ID] = 1
		task.State = "integrated"
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	reviseRequirement(t, f, r, "withdraw")
	p := emptyRequirementPlan()
	q := taskProposal(task)
	q.Key = "compensate"
	p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{task.ID}})
	runRequirementPlan(t, f, p)
	if requirementDetail(t, f, r.ID).Changes[0].State != "applied" {
		t.Fatal("recorded shared context rejected")
	}
	a := f.claim(t, "shared-compensation")
	if a == nil || !sameStrings(a.Task.RequirementIDs, []string{r.ID, other.ID}) {
		t.Fatal("shared compensation not schedulable")
	}
}
func TestRequirementReassessmentCannotRebindExcludedGoalRequirement(t *testing.T) {
	f, r, _ := requirementFixture(t)
	other := addUnrelatedGoalRequirement(t, f)
	reviseRequirement(t, f, r, "revise")
	runRequirementPlan(t, f, emptyRequirementPlan())
	detail := requirementDetail(t, f, r.ID)
	if detail.Changes[0].State != "blocked" {
		t.Fatal("fixture not blocked")
	}
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		d, e := goalDocument(tx)
		if e != nil {
			return e
		}
		d.RequirementIDs = []string{other.ID}
		d.Version++
		d.AuthorizationVersion++
		d.ApprovedVersion = d.Version
		d.AdmittedDocumentVersion = d.Version
		d.ApprovedAuthorizationVersion = d.AuthorizationVersion
		d.AdmittedAuthorizationVersion = d.AuthorizationVersion
		return tx.Put("planning_document", "current", d)
	}); e != nil {
		t.Fatal(e)
	}
	before := f.snapshot(t)
	if _, e := f.s.ReassessRequirement(context.Background(), f.owner, "excluded", r.ID, domain.VersionRequest{ExpectedVersion: detail.Changes[0].Version}); e == nil {
		t.Fatal("excluded requirement rebound")
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("rejected reassessment mutated state")
	}
}

func TestRequirementProposalsNeverAutomaticallyAcquireAuthority(t *testing.T) {
	for _, source := range []string{"node", "center-chat", "node-chat", "owner"} {
		t.Run(source, func(t *testing.T) {
			f, _, _ := requirementFixture(t)
			ctx := context.Background()
			var raw json.RawMessage
			var e error
			switch source {
			case "owner":
				raw, e = f.s.CreateRequirement(ctx, f.owner, "new", domain.Requirement{Title: "New work", Body: "Proposal content"})
			case "node":
				raw, e = f.s.SubmitRequirement(ctx, f.node, "new", domain.RequirementProposal{Title: "New work", Body: "Proposal content"})
			default:
				var actor Actor
				if source == "center-chat" {
					actor, _ = chatFixture(t, f)
				} else {
					actor, _ = nodeChatFixture(t, f, f.node, "node-chat")
				}
				raw, e = f.s.ChatSubmitRequirement(ctx, actor, "new", domain.ChatRequirementInput{Title: "New work", OriginalText: "Proposal content", Source: domain.ChatSource{ClientID: actor.ID, SessionID: "session", MessageID: "message"}})
			}
			r := decodeTest[domain.Requirement](t, raw, e)
			var change domain.RequirementChange
			if e = f.s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("requirement_change", requirementRevisionID(r.ID, 1), &change) }); source == "owner" {
				if e != nil || change.State != "queued" {
					t.Fatal("Owner addition not queued", e)
				}
			} else {
				if !errors.Is(e, domain.ErrNotFound) {
					t.Fatal("proposal acquired change authority", e)
				}
				before := f.snapshot(t)
				w := &PlanningWorker{Service: f.s, Executor: planningExecFunc(func(context.Context, string, string) (PlanningOutcome, error) {
					t.Fatal("proposal invoked Leader")
					return PlanningOutcome{}, nil
				})}
				if e = w.Tick(ctx); e != nil {
					t.Fatal(e)
				}
				after := f.snapshot(t)
				if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.PlanningDocument, after.PlanningDocument) {
					t.Fatal("proposal changed authority")
				}
			}
		})
	}
}
func TestRequirementReplanningSerializesOrdinaryEvaluation(t *testing.T) {
	for _, prequeued := range []bool{false, true} {
		t.Run(fmt.Sprint(prequeued), func(t *testing.T) {
			f, r, task := requirementFixture(t)
			ctx := context.Background()
			doc := *f.snapshot(t).PlanningDocument
			var ordinary domain.PlanningRun
			if prequeued {
				ordinary = scheduleTest(t, f, doc, "ordinary-before")
			}
			reviseRequirement(t, f, r, "revise")
			frozen := f.snapshot(t)
			if !prequeued {
				if _, e := f.s.SchedulePlanning(ctx, f.owner, "ordinary-after", domain.PlanningMessageRequest{ExpectedVersion: doc.Version}); e == nil {
					t.Fatal("ordinary evaluation bypassed pending change")
				}
				return
			}
			w := &PlanningWorker{Service: f.s}
			if e := w.queueRequirementChanges(ctx); e != nil {
				t.Fatal(e)
			}
			for _, plan := range f.snapshot(t).PlanningRuns {
				if plan.Purpose == requirementPurpose && plan.State == "queued" {
					t.Fatal("parallel requirement assessment queued")
				}
			}
			result := domain.LeaderResult{Reply: "Ready", Ready: true, Tasks: []domain.TaskProposal{taskProposal(task)}, MissingItems: []string{}, Clarifications: []string{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}}
			result.Tasks[0].Key = "fresh-key"
			result.Tasks[0].Title = "Replacement"
			result.Tasks[0].Objective = "Fresh counters for the same requirement"
			result.Tasks[0].RequirementIDs = []string{r.ID}
			w.Executor = resultExecutor(result)
			if e := w.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(frozen.Tasks, after.Tasks) {
				t.Fatal("ordinary plan minted replacement work")
			}
			for _, plan := range after.PlanningRuns {
				if plan.ID == ordinary.ID && plan.State == "succeeded" {
					t.Fatal("ordinary change bypass accepted")
				}
			}
		})
	}
}
func TestRequirementQueuedCancellationIsRecoverable(t *testing.T) {
	for _, action := range []string{"cancel", "save"} {
		for _, settled := range []bool{false, true} {
			t.Run(fmt.Sprint(action, settled), func(t *testing.T) {
				f, r, _ := requirementFixture(t)
				ctx := context.Background()
				reviseRequirement(t, f, r, "revise")
				w := &PlanningWorker{Service: f.s}
				if e := w.queueRequirementChanges(ctx); e != nil {
					t.Fatal(e)
				}
				if settled {
					if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
						plans, e := store.List[domain.PlanningRun](tx, "planning_run")
						if e != nil {
							return e
						}
						for _, plan := range plans {
							if plan.Purpose == requirementPurpose {
								plan.GrantSeconds = 5
								plan.Settled = true
								plan.TreeStopped = true
								if e = tx.Put("planning_run", plan.ID, plan); e != nil {
									return e
								}
							}
						}
						return nil
					}); e != nil {
						t.Fatal(e)
					}
				}
				doc := *f.snapshot(t).PlanningDocument
				var e error
				if action == "cancel" {
					_, e = f.s.CancelPlanning(ctx, f.owner, "cancel", domain.VersionRequest{ExpectedVersion: doc.Version})
				} else {
					_, e = f.s.SavePlanning(ctx, f.owner, "save", domain.PlanningSaveRequest{ExpectedVersion: doc.Version, Document: doc})
				}
				if e != nil {
					t.Fatal(e)
				}
				change := requirementDetail(t, f, r.ID).Changes[0]
				if change.State != "blocked" {
					t.Fatal("cancelled assessment stuck", change)
				}
				if action == "cancel" {
					if _, e = f.s.ReassessRequirement(ctx, f.owner, "retry", r.ID, domain.VersionRequest{ExpectedVersion: change.Version}); e != nil {
						t.Fatal("stopped assessment not recoverable", e)
					}
				}
			})
		}
	}
}
func TestRequirementEditsInvalidateFixedCandidatesWithoutErasingEvidence(t *testing.T) {
	for _, kind := range []string{"revise", "withdraw"} {
		for _, state := range []string{"accepted", "reviewing", "ready_to_land"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				f, r, task := requirementFixture(t)
				candidate := domain.Candidate{ID: "fixed", Version: 1, TaskID: task.ID, State: state, Commit: strings.Repeat("c", 40), Detail: "Old evidence"}
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					task.State = "awaiting_integration"
					task.CurrentCandidateID = candidate.ID
					if e := tx.Put("task", task.ID, task); e != nil {
						return e
					}
					return tx.Put("candidate", candidate.ID, candidate)
				}); e != nil {
					t.Fatal(e)
				}
				change := reviseRequirement(t, f, r, kind)
				after := f.snapshot(t)
				var got domain.Candidate
				for _, c := range after.Candidates {
					if c.ID == candidate.ID {
						got = c
					}
				}
				if got.State != "invalidated" || got.Version != 2 || got.Commit != candidate.Commit || requirementTask(t, f, task.ID).CurrentCandidateID != "" {
					t.Fatal("abandoned candidate remains active or lost evidence", got)
				}
				if len(change.Impacts) != 1 || change.Impacts[0].Task.CurrentCandidateID != candidate.ID {
					t.Fatal("impact history lost fixed candidate")
				}
			})
		}
	}
}
func TestRequirementNewKeysCannotShadowExistingDependencyIDs(t *testing.T) {
	f, r, task := requirementFixture(t)
	reviseRequirement(t, f, r, "revise")
	current := requirementTask(t, f, task.ID)
	p := emptyRequirementPlan()
	p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: task.ID, ExpectedVersion: current.Version, Proposal: taskProposal(current)})
	q := taskProposal(current)
	q.Key = task.ID
	q.Title = "Different followup"
	q.Objective = "New work"
	p.Tasks = append(p.Tasks, domain.RequirementNewTask{Proposal: q, RelatedTaskIDs: []string{}})
	before := f.snapshot(t)
	runRequirementPlan(t, f, p)
	change := requirementDetail(t, f, r.ID).Changes[0]
	if change.State != "blocked" || !strings.Contains(change.Detail, "冲突") || !reflect.DeepEqual(before.Tasks, f.snapshot(t).Tasks) {
		t.Fatal("local key shadowed an existing task", change)
	}
}

func TestRequirementRevisionCannotApproveAManualDraft(t *testing.T) {
	f, r, task := requirementFixture(t)
	ctx := context.Background()
	task.Title = "Unapproved draft"
	task.Description = "Manual work awaiting approval"
	raw, e := f.s.CreateTask(ctx, f.owner, "manual-draft", task)
	draft := decodeTest[domain.Task](t, raw, e)
	if draft.State != "draft" || draft.ApprovedVersion != 0 {
		t.Fatal("fixture already approved")
	}
	reviseRequirement(t, f, r, "revise")
	p := emptyRequirementPlan()
	for _, t0 := range f.snapshot(t).Tasks {
		if containsPaths(t0.RequirementIDs, []string{r.ID}) {
			p.Updates = append(p.Updates, domain.RequirementTaskUpdate{TaskID: t0.ID, ExpectedVersion: t0.Version, Proposal: taskProposal(t0)})
		}
	}
	runRequirementPlan(t, f, p)
	updated := requirementTask(t, f, draft.ID)
	if requirementDetail(t, f, r.ID).Changes[0].State != "applied" || updated.State != "draft" || updated.ApprovedVersion != 0 || updated.RequirementRevisions[r.ID] != 2 {
		t.Fatal("replanning acquired draft authority", updated)
	}
	for _, other := range f.snapshot(t).Tasks {
		if other.ID != draft.ID && !domain.TaskFinished(other) {
			if _, e = f.s.TaskAction(ctx, f.owner, "cancel-"+other.ID, other.ID, "cancel", domain.VersionRequest{ExpectedVersion: other.Version}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if got := f.claim(t, "before-manual-approval"); got != nil {
		t.Fatal("draft was claimable", got.Task.ID)
	}
	if _, e = f.s.TaskAction(ctx, f.owner, "approve-draft", draft.ID, "approve", domain.VersionRequest{ExpectedVersion: updated.Version}); e != nil {
		t.Fatal(e)
	}
	if got := f.claim(t, "after-manual-approval"); got == nil || got.Task.ID != draft.ID {
		t.Fatal("normal approval did not authorize revised draft")
	}
}
