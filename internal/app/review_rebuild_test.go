package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func TestReviewRebuiltCandidateMustRevalidatePreviousDisposition(t *testing.T) {
	ctx := context.Background()
	f := newITFixture(t)
	f.implement(t, "src/feature.txt")
	first := f.claim(t, "review")
	negative := negativeLedgerReview(first)
	if _, err := f.s.Complete(ctx, f.node, "negative", first.Run.ID, domain.CompleteRequest{
		Generation: first.Run.Generation, Status: "succeeded", Quiescent: true, Review: &negative,
	}); err != nil {
		t.Fatal(err)
	}
	api := ReviewIssues{Service: f.s}
	page, err := api.List(ctx, f.owner, domain.ReviewIssueQuery{TaskID: first.Task.ID, Limit: 64})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	issue := page.Items[0]
	c := f.candidate(t)
	if _, err := api.Decide(ctx, f.owner, "reconsider", issue.ID, domain.ReviewIssueDecision{
		ExpectedVersion: issue.Version, CandidateID: c.ID, CandidateVersion: c.Version, Commit: c.Commit,
		Disposition: "accepted_risk", Reason: "Owner requests independent reconsideration", RequestRecheck: true,
	}); err != nil {
		t.Fatal(err)
	}
	task := f.snapshot(t).Tasks[0]
	if _, err := f.s.TaskAction(ctx, f.owner, "resume-review", task.ID, "resume", domain.VersionRequest{ExpectedVersion: task.Version}); err != nil {
		t.Fatal(err)
	}
	review := f.claim(t, "review")
	page, err = api.List(ctx, f.owner, domain.ReviewIssueQuery{TaskID: review.Task.ID, Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	verified := domain.Review{Candidate: review.Run.Base, Verdict: "approve", Summary: "Finding disproven by current code and regression", Findings: []string{},
		RulesDigest: review.Run.ReviewRulesDigest, Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{{IssueID: issue.ID,
			ExpectedVersion: page.Items[0].Version, Disposition: "false_positive", Reason: "Rechecked transaction rejects duplicate keys before writing"}}}
	if _, err := f.s.Complete(ctx, f.node, "reconsidered-result", review.Run.ID, domain.CompleteRequest{
		Generation: review.Run.Generation, Status: "succeeded", Quiescent: true, Review: &verified,
	}); err != nil {
		t.Fatal(err)
	}
	before := f.candidate(t)
	page, err = api.List(ctx, f.owner, domain.ReviewIssueQuery{TaskID: review.Task.ID, Limit: 64})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "false_positive" || page.Items[0].SourceRunID != review.Run.ID {
		t.Fatal("fixture did not bind the verified resolution", page, err)
	}
	oldIssue := page.Items[0]
	seed := filepath.Join(filepath.Dir(f.s.Dir), "seed")
	if err := os.WriteFile(filepath.Join(seed, "src", "main-update.txt"), []byte("main moved\n"), 0600); err != nil {
		t.Fatal(err)
	}
	main, err := f.g.CommitAll(ctx, seed, "concurrent main", gitops.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	itGit(t, "git", seed, "push", "--", f.remote, string(main)+":refs/heads/main")
	for i := 0; i < 3; i++ {
		if err := f.w.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rebuilt := f.candidate(t)
	if rebuilt.ID != before.ID || rebuilt.Commit == before.Commit || rebuilt.Main != string(main) || rebuilt.State != "reviewing" || rebuilt.Review != nil {
		t.Fatal("real integration did not rebuild the same candidate", before, rebuilt)
	}
	if err := f.s.DB.View(ctx, func(tx *store.Tx) error {
		if err := reviewLedgerReady(tx, review.Task.ID, rebuilt.Commit); err == nil {
			t.Fatal("rebuild silently discarded historical issue verification")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	next := f.claim(t, "review")
	if next.ReviewContext == nil || len(next.ReviewContext.Issues) != 1 || next.ReviewContext.Issues[0].ID != oldIssue.ID {
		t.Fatal("rebuilt assignment lost its issue ledger", next.ReviewContext)
	}
	approval := domain.Review{Candidate: next.Run.Base, Verdict: "approve", Summary: "0 new issues", Findings: []string{},
		RulesDigest: next.Run.ReviewRulesDigest, Issues: []domain.ReviewFinding{}, Resolutions: []domain.ReviewResolution{}}
	if _, err := f.s.Complete(ctx, f.node, "omitted-resolution", next.Run.ID, domain.CompleteRequest{
		Generation: next.Run.Generation, Status: "succeeded", Quiescent: true, Review: &approval,
	}); err == nil {
		t.Fatal("new review inherited an old disposition")
	}
	approval.Resolutions = []domain.ReviewResolution{{IssueID: oldIssue.ID, ExpectedVersion: oldIssue.Version, Disposition: "fixed", Reason: "Rechecked retry safety and its regression against rebuilt commit"}}
	if _, err := f.s.Complete(ctx, f.node, "verified-resolution", next.Run.ID, domain.CompleteRequest{
		Generation: next.Run.Generation, Status: "succeeded", Quiescent: true, Review: &approval,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.candidate(t); got.State != "landed" || got.Commit != rebuilt.Commit {
		t.Fatal(got)
	}
	page, err = api.List(ctx, f.owner, domain.ReviewIssueQuery{TaskID: review.Task.ID, Limit: 64})
	if err != nil || page.Items[0].ResolutionCommit != rebuilt.Commit || page.Items[0].RulesDigest != next.Run.ReviewRulesDigest {
		t.Fatal("resolution provenance did not advance to the current rule version", page, err)
	}
}
