package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func TestGitHubHostedIntentRequiresMatchingMergedPR(t *testing.T) {
	for _, kind := range []string{"missing_adapter", "inspect_error", "open_pr", "wrong_pr"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f, h, _ := hostedRequirementFixture(t)
			before := f.snapshot(t)
			c := f.candidate(t)
			in := domain.Intent{ID: ID("intent_"), CandidateID: c.ID, Expected: c.Main, Desired: c.Commit, State: "unknown", CreatedAt: f.s.Now(), PublicationMode: "github", PullRequestNumber: 7}
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				c.State = "landing"
				if e := tx.Put("candidate", c.ID, c); e != nil {
					return e
				}
				if e := tx.Put("resource", "shared-source", domain.Resource{ID: "shared-source", TaskID: c.TaskID}); e != nil {
					return e
				}
				return tx.Put("intent", in.ID, in)
			}); e != nil {
				t.Fatal(e)
			}
			// Another actor landed this commit, then advanced main. Ancestry
			// proves the code is present, but does not prove this PR was merged.
			if _, e := f.g.Publish(ctx, f.w.repo(), f.remote, "refs/heads/main", gitops.OID(c.Main), gitops.OID(c.Commit)); e != nil {
				t.Fatal(e)
			}
			other := filepath.Join(t.TempDir(), "other_actor")
			if e := f.g.Clone(ctx, f.remote, other); e != nil {
				t.Fatal(e)
			}
			if e := f.g.Checkout(ctx, other, gitops.OID(c.Commit)); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(other, "src", "later.txt"), []byte("later\n"), 0600); e != nil {
				t.Fatal(e)
			}
			later, e := f.g.CommitAll(ctx, other, "later main", gitops.Identity{})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.g.Publish(ctx, other, f.remote, "refs/heads/main", gitops.OID(c.Commit), later); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "missing_adapter":
				f.w.PullRequests = nil
			case "inspect_error":
				h.failInspect = true
			case "wrong_pr":
				h.merged, h.number = true, 8
			}
			if e = f.w.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			after := f.snapshot(t)
			if e = f.s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("intent", in.ID, &in) }); e != nil {
				t.Fatal(e)
			}
			if in.State != "unknown" || f.candidate(t).State != "reconciling" || after.Tasks[0].State != before.Tasks[0].State || after.Tasks[0].Retries != before.Tasks[0].Retries || after.Project.UsedSeconds != before.Project.UsedSeconds || h.merges != 0 {
				t.Fatal("Git ancestry completed an unconfirmed hosted intent", in.State, f.candidate(t).State, after.Tasks[0].State)
			}
			if e = f.s.DB.View(ctx, func(tx *store.Tx) error {
				var resource domain.Resource
				return tx.Get("resource", "shared-source", &resource)
			}); e != nil {
				t.Fatal("unconfirmed PR released its resource", e)
			}
			// A fresh worker may complete only after the original PR receipt is
			// available. It observes that receipt without submitting another merge.
			h.failInspect, h.merged, h.number = false, true, 7
			f.w = &Integrator{Service: f.s, Git: f.g, PullRequests: h}
			if e = f.w.Tick(ctx); e != nil {
				t.Fatal(e)
			}
			if f.candidate(t).State != "landed" || f.snapshot(t).Tasks[0].State != "integrated" || h.merges != 0 {
				t.Fatal("matching merged PR was not reconciled exactly once")
			}
			if e = f.s.DB.View(ctx, func(tx *store.Tx) error {
				var resource domain.Resource
				return tx.Get("resource", "shared-source", &resource)
			}); !errors.Is(e, domain.ErrNotFound) {
				t.Fatal("confirmed merge retained its resource", e)
			}
		})
	}
}

// A cached green PR can be externally merged before its next scheduled sync.
func TestGitHubCachedReadyCandidateObservesExternalMerge(t *testing.T) {
	for _, kind := range []string{"merged", "inspect_error", "wrong_pr", "authority_changed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f, h, r := hostedRequirementFixture(t)
			c := f.candidate(t)
			if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				c.PullRequest.MergeReady, c.PullRequest.CI = true, "success"
				if e := tx.Put("candidate", c.ID, c); e != nil {
					return e
				}
				var b pullRequestBinding
				if e := tx.Get("pull_request", c.TaskID, &b); e != nil {
					return e
				}
				b.NextSync = f.s.Now().Add(time.Hour)
				return tx.Put("pull_request", c.TaskID, b)
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := f.g.Publish(ctx, f.w.repo(), f.remote, "refs/heads/main", gitops.OID(c.Main), gitops.OID(c.Commit)); e != nil {
				t.Fatal(e)
			}
			h.merged = true
			beforeReads, beforeEnsures, beforeStatuses := h.reads, h.ensures, h.statuses
			switch kind {
			case "inspect_error":
				h.failInspect = true
			case "wrong_pr":
				h.number = 8
			case "authority_changed":
				h.onInspect = func() { h.onInspect = nil; editHostedRequirement(t, f, r, "revise") }
			}
			_ = f.w.Tick(ctx) // Authority changes may reject landing after observation.
			after := f.candidate(t)
			reads := 1
			if kind == "merged" {
				reads++ // The persisted intent independently confirms its receipt.
			}
			if h.reads != beforeReads+reads || h.ensures != beforeEnsures || h.statuses != beforeStatuses || h.merges != 0 {
				t.Fatal("main change did not trigger read-only PR confirmation", h.reads-beforeReads, h.ensures-beforeEnsures, h.statuses-beforeStatuses, h.merges)
			}
			if kind == "merged" {
				if after.State != "landed" || after.PullRequest.State != "merged" || f.snapshot(t).Tasks[0].State != "integrated" {
					t.Fatal("cached open facts invalidated an externally merged candidate", after.State)
				}
			} else if kind == "authority_changed" {
				if after.State != "invalidated" || after.PullRequest.State == "merged" || f.snapshot(t).Tasks[0].CurrentCandidateID != "" || f.snapshot(t).Tasks[0].State == "integrated" {
					t.Fatal("late merged receipt restored revoked authority")
				}
			} else {
				if after.State != "ready_to_land" || after.Commit != c.Commit || after.PullRequest.State == "merged" {
					t.Fatal("unconfirmed PR destroyed or completed candidate", after.State)
				}
				h.failInspect, h.number = false, 7
				// Simulate the next scheduled sync after the failed observation.
				if e := f.s.DB.Update(ctx, func(tx *store.Tx) error {
					var b pullRequestBinding
					if e := tx.Get("pull_request", c.TaskID, &b); e != nil {
						return e
					}
					b.NextSync = time.Time{}
					return tx.Put("pull_request", c.TaskID, b)
				}); e != nil {
					t.Fatal(e)
				}
				if e := f.w.Tick(ctx); e != nil {
					t.Fatal(e)
				}
				if f.candidate(t).State != "landed" || h.merges != 0 {
					t.Fatal("fresh receipt did not recover external merge")
				}
			}
		})
	}
}
