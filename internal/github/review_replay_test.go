package github

import (
	"context"
	"encoding/json"
	"testing"

	"coordica/internal/domain"
)

func TestExternalReviewDeletedUnseenCommentCannotReplayOldApproval(t *testing.T) {
	for _, state := range []string{"pending", "failure", "success"} {
		t.Run(state, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			// JSON keeps this regression runnable on the previous contract too.
			if err := json.Unmarshal([]byte(`[{"provider":"codex","actor_id":42,"app_id":99,"kinds":["security_review"],"check_names":{"security_review":"Codex security"}}]`), &f.spec.ReviewProviders); err != nil {
				t.Fatal(err)
			}
			f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", false), externalComment(11, f.spec.Head, "running", false)}
			f.comments = f.comments[:1] // The writer deletes the newest result before the first Center read.
			status := "completed"
			if state == "pending" {
				status = "in_progress"
			}
			f.checks = []map[string]any{
				{"id": 1, "name": "ci", "head_sha": f.spec.Head, "status": "completed", "conclusion": "success", "app": map[string]int{"id": 42}},
				{"id": 10, "name": "Codex security", "head_sha": f.spec.Head, "status": "completed", "conclusion": "success", "app": map[string]int{"id": 99}, "details_url": "https://github.com/owner/repo/pull/7#issuecomment-10"},
				{"id": 20, "name": "Codex security", "head_sha": f.spec.Head, "status": status, "conclusion": state, "app": map[string]int{"id": 99}, "details_url": "https://github.com/owner/repo/pull/7#issuecomment-11"},
			}
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if err != nil || facts.MergeReady || len(facts.ExternalReviews) != 1 || facts.ExternalReviews[0].State == "success" {
				t.Fatal("deleted unseen comment resurrected an older approval", facts, err)
			}
			if _, err := f.client.Merge(context.Background(), f.spec); err == nil || f.merge != 0 {
				t.Fatal("comment replay reached merge", err, f.merge)
			}
		})
	}
}

func TestExternalReviewCommentOnlyPolicyCannotApprove(t *testing.T) {
	policy := []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, Kinds: []string{"security_review"}}}
	report := []domain.ExternalReview{{Provider: "codex", Kind: "security_review", ActorID: 42, AppID: 99, ReportID: 10, Commit: "fixed", State: "success"}}
	if domain.ValidateExternalReviewPolicy(policy) == nil || domain.ExternalReviewsReady(policy, report, "fixed") {
		t.Fatal("deletable comments alone granted publication authority")
	}
}
