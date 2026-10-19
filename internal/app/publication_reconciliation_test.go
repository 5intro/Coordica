package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
