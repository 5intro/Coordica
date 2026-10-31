package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestExternalReviewMergedReceiptStillRequiresProviderEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "running", "unresolved", "stale", "success"} {
		t.Run(mode, func(t *testing.T) {
			f := protocol(t)
			f.spec.Number, f.merged, f.testMerge = 7, true, strings.Repeat("d", 40)
			f.spec.ReviewProviders = []domain.ExternalReviewPolicy{{Provider: "codex", ActorID: 42, AppID: 99, CheckNames: map[string]string{"security_review": "Codex security"}, Kinds: []string{"security_review"}}}
			head, status := f.spec.Head, "completed"
			if mode == "stale" {
				head = f.spec.Base
			}
			if mode == "running" {
				status = "running"
			}
			if mode != "missing" {
				f.comments = []map[string]any{externalComment(10, head, status, mode == "unresolved")}
			}
			fixtureReviewChecks(f, nil)
			if mode == "unresolved" {
				fixtureReviewChecks(f, map[string]string{"security_review": "failure"})
			}
			facts, err := f.client.Inspect(context.Background(), f.spec)
			if err != nil || facts.State != "merged" || len(facts.ExternalReviews) != 1 {
				t.Fatal("merged PR lost its external review evidence", facts, err)
			}
			merge, err := f.client.Merge(context.Background(), f.spec)
			if mode == "success" {
				if err != nil || merge != f.testMerge || facts.ExternalReviews[0].State != "success" {
					t.Fatal("valid merged receipt was not acknowledged", merge, facts, err)
				}
			} else if !errors.Is(err, domain.ErrPublicationNotReady) || merge != "" {
				t.Fatal("merged flag bypassed external review gate", merge, err)
			}
			if f.merge != 0 {
				t.Fatal("observing an existing merge submitted another merge")
			}
		})
	}
}
