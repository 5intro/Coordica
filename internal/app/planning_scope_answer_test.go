package app

import (
	"context"
	"reflect"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestScopeQuestionsCannotBecomeOrdinaryPlanningAnswers(t *testing.T) {
	f, worker, assignment, scope := scopeFixture(t)
	ctx := context.Background()
	task := stopScope(t, f, assignment)
	before := f.snapshot(t)
	result := scopeResult(task, scope)
	result.Ready = false
	result.Clarifications = []string{"需要哪一个共享目录？"}
	worker.Executor = resultExecutor(result)
	if err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if after.ScopeRequests[0].State != "blocked" {
		t.Fatal("scope question lost its blocked scope request")
	}
	var source domain.PlanningRun
	for _, run := range after.PlanningRuns {
		if run.ScopeRequestID == scope.ID {
			source = run
		}
	}
	if source.ID == "" {
		t.Fatal("scope assessment was not executed")
	}
	prior, current := before.PlanningDocument, after.PlanningDocument
	if prior.LeaderRunID != current.LeaderRunID || prior.LeaderSession != current.LeaderSession ||
		!reflect.DeepEqual(prior.LeaderResult, current.LeaderResult) || !reflect.DeepEqual(prior.MissingItems, current.MissingItems) ||
		len(before.LeaderMessages) != len(after.LeaderMessages) {
		t.Error("scope result replaced the ordinary goal's answer source")
	}
	reply := domain.PlanningMessageRequest{ExpectedVersion: after.PlanningDocument.Version,
		ReplyToRunID: source.ID, Question: result.Clarifications[0], Text: "src/shared"}
	if _, err := f.s.SchedulePlanning(ctx, f.owner, "scope-answer", reply); err == nil {
		t.Error("scope answer scheduled an unrelated ordinary evaluation")
	}
	// Reject persisted bindings created by an older implementation, too.
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		document := after.PlanningDocument
		document.LeaderRunID, document.LeaderResult = source.ID, &result
		return tx.Put("planning_document", "current", document)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SchedulePlanning(ctx, f.owner, "legacy-scope-answer", reply); err == nil {
		t.Error("legacy scope binding was accepted as ordinary planning authority")
	}
	if len(f.snapshot(t).PlanningRuns) != len(after.PlanningRuns) {
		t.Fatal("rejected scope reply partially created a planning run")
	}
}
