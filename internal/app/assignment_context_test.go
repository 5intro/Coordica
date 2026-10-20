package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func TestAssignedRequirementContextIsPinnedLimitedAndRevocationChecked(t *testing.T) {
	f, req, task := requirementFixture(t)
	ctx := context.Background()
	// Another requirement and chat provenance are not part of this task's contract.
	raw, err := f.s.SubmitRequirement(ctx, f.node, "private-other", domain.RequirementProposal{Title: "Unrelated", Body: "UNRELATED_CONTEXT"})
	_ = decodeTest[domain.Requirement](t, raw, err)
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.Requirement
		if e := tx.Get("requirement", req.ID, &r); e != nil {
			return e
		}
		r.OriginalText = "PRIVATE_ORIGINAL"
		r.AssistantSummary = "PRIVATE_SUMMARY"
		return tx.Put("requirement", r.ID, r)
	}); err != nil {
		t.Fatal(err)
	}
	a := f.claim(t, "fixed-requirement-context")
	if a == nil || a.Task.ID != task.ID || len(a.Requirements) != 1 || a.Requirements[0].ID != req.ID || a.Requirements[0].Body != req.Body || a.Requirements[0].ContentRevision != 1 {
		t.Fatal("linked contract missing", a)
	}
	encoded, _ := json.Marshal(a.Requirements)
	for _, private := range []string{"PRIVATE_ORIGINAL", "PRIVATE_SUMMARY", "UNRELATED_CONTEXT"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("unrelated provenance crossed task boundary")
		}
	}
	if err := domain.ValidateAssignmentRequirements(*a); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.RunContext(ctx, f.node, a.Run.ID)
	if err != nil || !reflect.DeepEqual(got.Requirements, a.Requirements) {
		t.Fatal("context disagrees with fixed assignment", got.Requirements, err)
	}
	foreign := f.node
	foreign.NodeID = "foreign"
	if _, err := f.s.RunContext(ctx, foreign, a.Run.ID); err == nil {
		t.Fatal("foreign node read task contract")
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.Requirement
		if e := tx.Get("requirement", req.ID, &r); e != nil {
			return e
		}
		r.ContentRevision = 2
		r.Body = "NEW_REVISION"
		return tx.Put("requirement", req.ID, r)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunContext(ctx, f.node, a.Run.ID); err == nil {
		t.Fatal("old Run read new requirement content")
	}
	if a.Requirements[0].Body != req.Body {
		t.Fatal("fixed assignment changed")
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var reqCurrent domain.Requirement
		if e := tx.Get("requirement", req.ID, &reqCurrent); e != nil {
			return e
		}
		reqCurrent.ContentRevision = 1
		reqCurrent.Body = req.Body
		if e := tx.Put("requirement", req.ID, reqCurrent); e != nil {
			return e
		}
		var n domain.Node
		if e := tx.Get("node", f.node.NodeID, &n); e != nil {
			return e
		}
		n.Revoked = true
		return tx.Put("node", n.ID, n)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunContext(ctx, f.node, a.Run.ID); err == nil {
		t.Fatal("revoked node read context")
	}
}

func TestRunContextCannotReturnANewGoalContractToOldRun(t *testing.T) {
	f, _, _ := requirementFixture(t)
	a := f.claim(t, "old-goal-context")
	if a == nil || a.GoalContext == nil {
		t.Fatal("missing goal fixture")
	}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		d, err := goalDocument(tx)
		if err != nil {
			return err
		}
		d.Version++
		d.AuthorizationVersion++
		d.Architecture = "NEW_ARCHITECTURE"
		return tx.Put("planning_document", "current", d)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunContext(context.Background(), f.node, a.Run.ID); err == nil {
		t.Fatal("old Run received a new goal")
	}
	if a.GoalContext.Architecture == "NEW_ARCHITECTURE" {
		t.Fatal("fixed assignment mutated")
	}
}

func TestOversizedContextBlocksBeforeAllocationAndDoesNotStarveOtherTasks(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	big := f.task
	big.Description = strings.Repeat("large", domain.MaxAssignmentContextBytes/4)
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("task", big.ID, big) }); err != nil {
		t.Fatal(err)
	}
	raw, err := f.s.CreateTask(ctx, f.owner, "small-context", domain.Task{Title: "Small", Description: "Small work", Acceptance: "Check", AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 20, MaxRunSeconds: 10})
	small := decodeTest[domain.Task](t, raw, err)
	raw, err = f.s.TaskAction(ctx, f.owner, "approve-small-context", small.ID, "approve", domain.VersionRequest{ExpectedVersion: small.Version})
	small = decodeTest[domain.Task](t, raw, err)
	a := f.claim(t, "skip-large-context")
	if a == nil || a.Task.ID != small.ID {
		t.Fatal("large context starved compatible work", a)
	}
	snap := f.snapshot(t)
	blocked := requirementTask(t, f, big.ID)
	if blocked.HoldReason != "human_decision" || blocked.Generation != big.Generation || blocked.CurrentRunID != "" || blocked.UsedSeconds != big.UsedSeconds || blocked.ReservedSeconds != 0 || len(snap.Runs) != 1 || snap.Project.ReservedSeconds != a.Run.GrantSeconds {
		t.Fatal("unusable input consumed an execution grant", blocked, snap.Runs)
	}
	if again := f.claim(t, "skip-large-again"); again != nil {
		t.Fatal("blocked task dispatched")
	}
	count := 0
	for _, event := range f.snapshot(t).Events {
		if event.Kind == "task.context_blocked" && event.ObjectID == big.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatal("polling duplicated context blockers", count)
	}
}

func TestRequirementAssignmentsFenceLegacyAndUnknownContextProtocols(t *testing.T) {
	for _, version := range []int{0, -1, domain.AssignmentContextVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f, _, task := requirementFixture(t)
			ctx := context.Background()
			before := f.snapshot(t)
			request := domain.ClaimRequest{AssignmentContextVersion: version, Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true, Review: true}}}
			// No compatible work must reach Git preflight either.
			w := &Integrator{Service: f.s, Git: gitops.New("missing-git-must-not-start")}
			raw, err := w.Claim(ctx, f.node, "old-context", request)
			if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
				t.Fatal("incompatible claimant received requirement-bound work", got)
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Runs, after.Runs) || !reflect.DeepEqual(before.Project, after.Project) {
				t.Fatal("protocol mismatch allocated, held or charged work")
			}
			// A null legacy claim stays null, even after an upgraded claimant wins.
			a := f.claim(t, "new-context")
			if a == nil || a.Task.ID != task.ID || len(a.Requirements) != 1 {
				t.Fatal("compatible Runner could not claim untouched work", a)
			}
			raw, err = f.s.Claim(ctx, f.node, "old-context", request, strings.Repeat("a", 40))
			if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
				t.Fatal("legacy replay received upgraded assignment")
			}
			// The same gate applies to fixed-candidate code review.
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				var current domain.Task
				if err := tx.Get("task", task.ID, &current); err != nil {
					return err
				}
				current.CurrentRunID, current.State = "", "awaiting_integration"
				current.CurrentCandidateID = "candidate-context"
				if err := tx.Delete("run", a.Run.ID); err != nil {
					return err
				}
				if err := tx.Put("task", current.ID, current); err != nil {
					return err
				}
				return tx.Put("candidate", "candidate-context", domain.Candidate{ID: "candidate-context", TaskID: task.ID, State: "reviewing", Commit: strings.Repeat("c", 40), BaselineVersion: task.BaselineVersion, BaselineDigest: task.BaselineDigest, AuthorizationVersion: task.AuthorizationVersion, PolicyVersion: a.Project.PolicyVersion})
			}); err != nil {
				t.Fatal(err)
			}
			raw, err = f.s.Claim(ctx, f.node, "legacy-review", request, strings.Repeat("a", 40))
			if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
				t.Fatal("legacy reviewer received work")
			}
			if got := f.claim(t, "new-review"); got == nil || got.Run.Kind != "review" || len(got.Requirements) != 1 {
				t.Fatal("compatible review was blocked", got)
			}
		})
	}
}

func TestLegacyClaimStillAllowsTasksWithoutLinkedRequirements(t *testing.T) {
	f := newServiceFixture(t)
	raw, err := f.s.Claim(context.Background(), f.node, "legacy-unbound", domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "codex", Available: true, Structured: true, Cancel: true}}}, strings.Repeat("a", 40))
	if a := decodeTest[*domain.Assignment](t, raw, err); a == nil || a.Task.ID != f.task.ID {
		t.Fatal("unbound legacy work unnecessarily blocked")
	}
}

func TestHistoricalDuplicateBindingsBlockOnlyTheBadTask(t *testing.T) {
	f, req, bad := requirementFixture(t)
	good := bad
	good.ID, good.Title = "task_healthy_context", "Healthy later task"
	bad.RequirementIDs = []string{req.ID, req.ID}
	if _, err := f.s.CreateTask(context.Background(), f.owner, "reject-duplicate", bad); err == nil {
		t.Fatal("Owner task admission accepted duplicates")
	}
	// Simulate a previously admitted task from before uniqueness validation.
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if err := tx.Put("task", bad.ID, bad); err != nil {
			return err
		}
		return tx.Put("task", good.ID, good)
	}); err != nil {
		t.Fatal(err)
	}
	a := f.claim(t, "past-bad-context")
	if a == nil || a.Task.ID != good.ID {
		t.Fatal("bad binding starved healthy work", a)
	}
	got := requirementTask(t, f, bad.ID)
	if got.HoldReason != "human_decision" || got.Generation != bad.Generation || got.CurrentRunID != "" || got.UsedSeconds != bad.UsedSeconds || got.ReservedSeconds != bad.ReservedSeconds {
		t.Fatal("bad input acquired a grant or reset accounting", got)
	}
	if next := f.claim(t, "bad-context-again"); next != nil {
		t.Fatal("quarantined input dispatched")
	}
	snapshot := f.snapshot(t)
	if len(snapshot.Runs) != 1 || snapshot.Project.ReservedSeconds != a.Run.GrantSeconds {
		t.Fatal("bad input reserved resources")
	}
	count := 0
	for _, event := range snapshot.Events {
		if event.ObjectID == bad.ID && event.Kind == "task.context_blocked" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("repeated polls duplicated the block", count)
	}
}

func TestLeaderTaskAdmissionRejectsDuplicateBindingsAtomically(t *testing.T) {
	f, d, result := planningFixture(t, true)
	proposal := &result.Tasks[0]
	proposal.RequirementIDs = append(proposal.RequirementIDs, proposal.RequirementIDs[0])
	scheduleTest(t, f, d, "duplicate-proposal")
	w := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t)
	if len(snapshot.Tasks) != 1 || snapshot.PlanningRuns[0].State != "blocked" {
		t.Fatal("duplicate proposal partially admitted", snapshot.PlanningRuns)
	}
}
