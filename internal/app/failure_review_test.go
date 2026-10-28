package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func failureReviewFixture(t *testing.T, hint *domain.RecoveryHint, preflight bool) (*serviceFixture, domain.Candidate, domain.Assignment) {
	t.Helper()
	f, source := failureFixture(t)
	c := domain.Candidate{ID: "fixed", Version: 1, TaskID: source.SourceTask.ID, SourceRunID: source.SourceRunID,
		AuthorizationVersion: source.SourceTask.AuthorizationVersion, PolicyVersion: source.PolicyVersion,
		BaselineVersion: source.SourceTask.BaselineVersion, BaselineDigest: source.SourceTask.BaselineDigest,
		State: "reviewing", Main: strings.Repeat("a", 40), Head: strings.Repeat("b", 40), Commit: strings.Repeat("c", 40)}
	if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		if err := tx.Delete("failure_assessment", source.ID); err != nil {
			return err
		}
		task := source.SourceTask
		task.State = "awaiting_integration"
		task.HoldReason = ""
		task.CurrentCandidateID = c.ID
		task.Version++
		var run domain.Run
		if err := tx.Get("run", source.SourceRunID, &run); err != nil {
			return err
		}
		run.State = "succeeded"
		run.Head = c.Head
		if err := tx.Put("run", run.ID, run); err != nil {
			return err
		}
		if err := tx.Put("candidate", c.ID, c); err != nil {
			return err
		}
		return tx.Put("task", task.ID, task)
	}); err != nil {
		t.Fatal(err)
	}
	a := f.claim(t, "review-source")
	if a == nil || a.Run.Kind != "review" {
		t.Fatal("review not claimed", a)
	}
	summary, detail, session := "Missing capability", "", "review-model"
	if preflight {
		summary = "Complete fixed review input is unavailable"
		detail = "review blocked before agent execution: incomplete material"
		session = ""
	}
	_, err := f.s.Complete(context.Background(), f.node, "negative-review", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation,
		Status: "succeeded", Quiescent: true, ElapsedSeconds: 2, SessionID: session, Detail: detail,
		Review: &domain.Review{Candidate: c.Commit, Verdict: "blocked", Summary: summary, Findings: []string{"missing input"}, Recovery: hint}})
	if err != nil {
		t.Fatal(err)
	}
	return f, c, *a
}

func TestFailureRecoveryReviewHintDefersRepairAndInputRetryKeepsFixedCode(t *testing.T) {
	f, c, first := failureReviewFixture(t, &domain.RecoveryHint{Category: "evidence_missing", Reason: "Input failed before review execution"}, true)
	before := failureCases(t, f.s)[0]
	if before.SourceTask.Repairs != 0 || before.SourceTask.HoldReason != "failure_assessment" {
		t.Fatal("hint burned code repair allowance", before)
	}
	got := runRecovery(t, f, domain.FailureRecoveryPlan{Category: "evidence_missing", Reason: "Retry complete input on unchanged code", Action: "retry_review_input", Tasks: []domain.TaskProposal{}})
	if got.State != "applied" {
		t.Fatal(got)
	}
	var after domain.Task
	var candidate domain.Candidate
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		if err := tx.Get("task", before.SourceTask.ID, &after); err != nil {
			return err
		}
		return tx.Get("candidate", c.ID, &candidate)
	}); err != nil {
		t.Fatal(err)
	}
	want := before.SourceTask
	want.Version++
	want.Retries++
	want.HoldReason = ""
	if !reflect.DeepEqual(after, want) || candidate.Commit != c.Commit || candidate.Review != nil || candidate.State != "reviewing" {
		t.Fatal("input retry changed code/accounting or kept stale review", after, candidate)
	}
	next := f.claim(t, "new-independent-review")
	if next == nil || next.Run.Base != c.Commit || next.Run.Generation <= first.Run.Generation {
		t.Fatal("fresh fixed review not scheduled", next)
	}
	_, err := f.s.Complete(context.Background(), f.node, "old-review-late", first.Run.ID, domain.CompleteRequest{
		Generation: first.Run.Generation, Status: "succeeded", Quiescent: true, Review: &domain.Review{Candidate: c.Commit, Verdict: "approve", Summary: "late", Findings: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("candidate", c.ID, &candidate) }); err != nil {
		t.Fatal(err)
	}
	if candidate.Review != nil {
		t.Fatal("old verdict approved new review")
	}
}

func TestFailureRecoveryCannotUseMaterialRetryForNativeModelVerdict(t *testing.T) {
	f, _, _ := failureReviewFixture(t, &domain.RecoveryHint{Category: "evidence_missing", Reason: "Reviewer did execute"}, false)
	got := runRecovery(t, f, domain.FailureRecoveryPlan{Category: "evidence_missing", Reason: "Cannot bypass real verdict", Action: "retry_review_input", Tasks: []domain.TaskProposal{}})
	if got.State != "needs_owner" {
		t.Fatal("native verdict retried as preflight", got)
	}
}

func TestFailureRecoveryLegacyNegativeReviewRetainsBehavior(t *testing.T) {
	f, _, _ := failureReviewFixture(t, nil, false)
	cases := failureCases(t, f.s)
	if len(cases) != 1 || cases[0].SourceTask.HoldReason != "human_decision" {
		t.Fatal("legacy blocked verdict changed", cases)
	}
}

func TestFailureRecoveryParentRevocationStopsLiveFollowupAndRejectsItsResult(t *testing.T) {
	f, source := failureFixture(t)
	got := runRecovery(t, f, followupPlan(source))
	child := f.claim(t, "claim-related-work")
	if child == nil || child.Task.ID != got.FollowupTaskID || child.FailureContext == nil {
		t.Fatal("related work missing fixed context", child)
	}
	parent := got.SourceTask
	parent.Version = got.WaitingTaskVersion
	if _, err := f.s.TaskAction(context.Background(), f.owner, "pause-source", parent.ID, "pause", domain.VersionRequest{ExpectedVersion: parent.Version}); err != nil {
		t.Fatal(err)
	}
	reply, err := f.s.Heartbeat(context.Background(), f.node, child.Run.ID, domain.HeartbeatRequest{Generation: child.Run.Generation})
	if err != nil || !reply.Cancel {
		t.Fatal("parent revocation did not stop child", reply, err)
	}
	_, err = f.s.Complete(context.Background(), f.node, "late-child-success", child.Run.ID, domain.CompleteRequest{
		Generation: child.Run.Generation, Status: "succeeded", Quiescent: true, ElapsedSeconds: 1, Head: strings.Repeat("f", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.snapshot(t).Candidates) != 0 {
		t.Fatal("revoked child produced a new candidate")
	}
	if err := (FailureRecoveries{Service: f.s}).queue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if failureCases(t, f.s)[0].State != "stale" {
		t.Fatal("revoked source was resumed")
	}
}

func TestFailureRecoveryUnplannedReviewNeverGetsAnUnserviceableHold(t *testing.T) {
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	c := f.candidate(t)
	review := f.claim(t, "review")
	if review.FailureRecoveryVersion != 0 {
		t.Fatal("unplanned project advertised automatic diagnosis")
	}
	_, err := f.s.Complete(context.Background(), f.node, "hint-without-goal", review.Run.ID, domain.CompleteRequest{Generation: review.Run.Generation,
		Status: "succeeded", Quiescent: true, ElapsedSeconds: 1, Review: testReview(review, domain.Review{Candidate: c.Commit, Verdict: "blocked", Summary: "Need evidence", Findings: []string{},
			Recovery: &domain.RecoveryHint{Category: "evidence_missing", Reason: "No admitted goal for a follow-up"}})})
	if err != nil {
		t.Fatal(err)
	}
	if task := f.snapshot(t).Tasks[0]; task.HoldReason != "human_decision" {
		t.Fatal("orphan diagnosis hold", task)
	}
	if len(failureCases(t, f.s)) != 0 {
		t.Fatal("unplanned task acquired goal authority")
	}
}

func TestFailureRecoveryHistoricalHeartbeatCannotRewriteFailureEvidence(t *testing.T) {
	f, source := failureFixture(t)
	var before, after domain.Run
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("run", source.SourceRunID, &before) }); err != nil {
		t.Fatal(err)
	}
	reply, err := f.s.Heartbeat(context.Background(), f.node, before.ID, domain.HeartbeatRequest{Generation: before.Generation, Detail: "late text that is not the failure evidence"})
	if err != nil || !reply.Cancel {
		t.Fatal("historical heartbeat not fenced", reply, err)
	}
	if err := f.s.DB.View(context.Background(), func(tx *store.Tx) error { return tx.Get("run", before.ID, &after) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("late heartbeat rewrote terminal source evidence", before, after)
	}
}
