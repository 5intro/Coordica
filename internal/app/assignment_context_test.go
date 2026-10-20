package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
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
