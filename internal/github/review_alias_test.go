package github

import (
	"context"
	"testing"

	"coordica/internal/domain"
)

func TestExternalReviewCannotAliasRequiredCIAndMaskItsFailure(t *testing.T) {
	for _, name := range []string{"ci", "CI"} {
		t.Run(name, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number = 7
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99,
				Kinds: []string{"security_review"}, CheckNames: map[string]string{"security_review": name}}}
			f.comments = []map[string]any{externalComment(10, f.spec.Head, "completed", false)}
			fixtureReviewChecks(f, nil)
			f.checks[0]["conclusion"] = "failure" // Actual CI App fails; the review App's same-named check passes.
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if err == nil || facts.MergeReady {
				t.Fatal("review check replaced independent CI", name, facts, err)
			}
			if _, err := f.client.Merge(context.Background(), f.spec); err == nil || f.merge != 0 {
				t.Fatal("aliased gate reached merge", err, f.merge)
			}
			if err := f.client.PublishReview(context.Background(), f.spec, "success"); err == nil || f.status != 0 {
				t.Fatal("aliased spec published green", err, f.status)
			}
			// Revocations retain fixed object identity but intentionally carry no
			// potentially invalid legacy enrollment or test-name policy.
			downgrade := f.spec
			downgrade.ReviewProviders, downgrade.RequiredChecks = nil, nil
			if err := f.client.PublishReview(context.Background(), downgrade, "pending"); err != nil || f.status != 1 {
				t.Fatal("invalid old policy prevented a safe downgrade", err, f.status)
			}
		})
	}
}
