package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestExternalReviewFencedFailureDoesNotSpendRepair(t *testing.T) {
	for _, mode := range []string{"stale failure", "human approval", "human changes", "current failure", "CI failure"} {
		t.Run(mode, func(t *testing.T) {
			f, h := hostedFixture(t)
			if err := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				p, err := getProject(tx)
				if err != nil {
					return err
				}
				p.Publication.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99,
					Kinds: []string{"security_review"}, CheckNames: map[string]string{"security_review": "Codex security"}}}
				return tx.Put("project", p.ID, p)
			}); err != nil {
				t.Fatal(err)
			}
			h.external = mergedExternalReportAt("pending", 20)
			f.implement(t, "src/feature.txt")
			if err := f.w.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.reviewAndValidate(t)
			refreshHosted(t, f)
			before := f.snapshot(t).Tasks[0]
			h.external, h.review = mergedExternalReportAt("failure", 10), "changes_requested"
			if mode == "human changes" || mode == "human approval" {
				state := "CHANGES_REQUESTED"
				if mode == "human approval" {
					state = "APPROVED"
				}
				h.remoteReviews = []domain.RemoteReview{{ID: 1, Author: "human", State: state, Commit: f.candidate(t).Commit}}
			}
			if mode == "current failure" {
				h.external = mergedExternalReportAt("failure", 21)
			}
			if mode == "CI failure" {
				h.ci = "failure"
			}
			refreshHosted(t, f)
			after, candidate := f.snapshot(t).Tasks[0], f.candidate(t)
			shouldRepair := mode == "human changes" || mode == "current failure" || mode == "CI failure"
			if shouldRepair {
				if candidate.State != "rejected" || after.Repairs != before.Repairs+1 {
					t.Fatal("independent current failure lost its repair path", candidate.State, after)
				}
			} else if candidate.State == "rejected" || after.Repairs != before.Repairs || after.Retries != before.Retries || h.merges != 0 {
				t.Fatal("fenced stale evidence consumed a repair or merged", candidate.State, after)
			}
			if mode != "current failure" && candidate.PullRequest.ExternalReviews[0].State != "pending" {
				t.Fatal("old failure escaped its receipt fence", candidate.PullRequest)
			}
			if mode == "human approval" && candidate.PullRequest.Review != "approved" {
				t.Fatal("independent human approval was discarded", candidate.PullRequest)
			}
		})
	}
}
