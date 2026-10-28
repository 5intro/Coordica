package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestReviewIssueReconsiderationPreservesVerdictAndLimits(t *testing.T) {
	for _, mode := range []string{"recheck", "stale candidate", "replacement", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			f := newITFixture(t)
			f.implement(t, "src/feature.txt")
			a := f.claim(t, "review")
			v := negativeLedgerReview(a)
			if _, err := f.s.Complete(context.Background(), f.node, "negative", a.Run.ID, domain.CompleteRequest{Generation: a.Run.Generation, Status: "succeeded", Quiescent: true, Review: &v}); err != nil {
				t.Fatal(err)
			}
			c := f.candidate(t)
			api := ReviewIssues{Service: f.s}
			page, err := api.List(context.Background(), f.owner, domain.ReviewIssueQuery{TaskID: a.Task.ID, Limit: 1})
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
			issue := page.Items[0]
			in := domain.ReviewIssueDecision{ExpectedVersion: issue.Version, CandidateID: c.ID, CandidateVersion: c.Version, Commit: c.Commit,
				Disposition: "false_positive", Reason: "Existing transaction already guarantees this invariant", RequestRecheck: true}
			if mode == "stale candidate" {
				in.CandidateVersion--
			}
			if mode == "replacement" {
				f.claim(t, "implement")
			}
			if mode == "exhausted" {
				if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					var task domain.Task
					if err := tx.Get("task", a.Task.ID, &task); err != nil {
						return err
					}
					task.Retries = task.MaxRetries
					return tx.Put("task", task.ID, task)
				}); err != nil {
					t.Fatal(err)
				}
			}
			_, err = api.Decide(context.Background(), f.owner, "disposition", issue.ID, in)
			if mode != "recheck" {
				if err == nil {
					t.Fatal("invalid recheck accepted", mode)
				}
				page, err = api.List(context.Background(), f.owner, domain.ReviewIssueQuery{TaskID: a.Task.ID, Limit: 1})
				if err != nil || page.Items[0].Version != issue.Version || page.Items[0].State != "open" {
					t.Fatal("partial disposition committed", page, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = api.Decide(context.Background(), f.owner, "disposition", issue.ID, in); err != nil {
				t.Fatal(err)
			}
			snap := f.snapshot(t)
			if snap.Tasks[0].Retries != a.Task.Retries+1 || snap.Tasks[0].Repairs != 1 || snap.Tasks[0].HoldReason != "paused" ||
				f.candidate(t).Review != nil || f.candidate(t).Commit != c.Commit || f.candidate(t).State != "reviewing" {
				t.Fatal("recheck approved, dispatched, reset counts or changed candidate", snap.Tasks, f.candidate(t))
			}
			if err = f.s.DB.View(context.Background(), func(tx *store.Tx) error {
				var receipt domain.Review
				if err := tx.Get("review_receipt", a.Run.ID, &receipt); err != nil {
					return err
				}
				if receipt.Verdict != "changes_requested" || len(receipt.Issues) != 1 {
					t.Fatal("original review was lost", receipt)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
