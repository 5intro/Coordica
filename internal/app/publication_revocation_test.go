package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func hostedRequirementFixture(t *testing.T) (*itFixture, *testPullHost, domain.Requirement) {
	t.Helper()
	f, h := hostedFixture(t)
	r := domain.Requirement{ID: ID("req_"), Version: 1, ContentRevision: 1, State: "active", Title: "Feature", Body: "Add the scoped feature"}
	task := f.snapshot(t).Tasks[0]
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		task.RequirementIDs = []string{r.ID}
		task.RequirementRevisions = map[string]int64{r.ID: 1}
		if e := tx.Put("requirement", r.ID, r); e != nil {
			return e
		}
		return tx.Put("task", task.ID, task)
	}); e != nil {
		t.Fatal(e)
	}
	f.implement(t, "src/feature.txt")
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.reviewAndValidate(t)
	refreshHosted(t, f) // Internal review succeeds; external CI still waits.
	if h.reviewCheck != "success" || h.merges != 0 {
		t.Fatal("fixture did not retain a reviewed, unmerged PR")
	}
	return f, h, r
}

func editHostedRequirement(t *testing.T, f *itFixture, r domain.Requirement, kind string) {
	t.Helper()
	in := domain.RequirementEdit{ExpectedVersion: r.Version, Reason: "Owner updated acceptance"}
	if kind == "revise" {
		in.Title, in.Body = "Updated feature", "The previous candidate no longer satisfies the requirement"
	}
	if _, e := f.s.EditRequirement(context.Background(), f.owner, ID("edit_"), r.ID, kind, in); e != nil {
		t.Fatal(e)
	}
}

func queuedReviewRevocations(t *testing.T, f *itFixture) []pullReviewRevocation {
	t.Helper()
	var values []pullReviewRevocation
	if e := f.s.DB.View(context.Background(), func(tx *store.Tx) error {
		var e error
		values, e = store.List[pullReviewRevocation](tx, "pull_review_revocation")
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return values
}

func TestGitHubRequirementInvalidationRevokesGreenStatusAcrossRestart(t *testing.T) {
	for _, kind := range []string{"revise", "withdraw"} {
		t.Run(kind, func(t *testing.T) {
			f, h, r := hostedRequirementFixture(t)
			before := f.snapshot(t).Tasks[0]
			commit := f.candidate(t).Commit
			editHostedRequirement(t, f, r, kind)
			queued := queuedReviewRevocations(t, f)
			if len(queued) != 1 || queued[0].Spec.Head != commit || f.snapshot(t).Tasks[0].CurrentCandidateID != "" {
				t.Fatal("invalidation did not atomically retain exact-head revocation")
			}
			if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
				p, e := getProject(tx)
				if e != nil {
					return e
				}
				p.Paused = true
				return tx.Put("project", p.ID, p)
			}); e != nil {
				t.Fatal(e)
			}
			if e := f.s.DB.View(context.Background(), initializationQuiet); e == nil {
				t.Fatal("reinitialization ignored pending remote authority")
			}
			h.failStatus = true // Remote applies the downgrade; its response is lost.
			if e := f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if len(queuedReviewRevocations(t, f)) != 1 || h.reviewCheck != "pending" {
				t.Fatal("unknown ACK discarded")
			}
			statuses := h.statuses
			f.w = &Integrator{Service: f.s, Git: f.g, PullRequests: h}
			if e := f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if h.statuses != statuses {
				t.Fatal("restart ignored persisted backoff")
			}
			now := f.s.Now().Add(31 * time.Second)
			f.s.Now = func() time.Time { return now }
			if e := f.w.Tick(context.Background()); e != nil {
				t.Fatal(e)
			}
			if len(queuedReviewRevocations(t, f)) != 0 || h.reviewCheck != "pending" || h.merges != 0 || h.creates != 1 {
				t.Fatal("detached candidate retained remote approval or created another PR")
			}
			after := f.snapshot(t).Tasks[0]
			if after.Retries != before.Retries || after.Repairs != before.Repairs || f.candidate(t).State != "invalidated" {
				t.Fatal("revocation changed task accounting or revived work")
			}
			if e := f.s.DB.View(context.Background(), initializationQuiet); e != nil {
				t.Fatal("acknowledged revocation still blocks initialization", e)
			}
		})
	}
}

func TestGitHubLateFactsCannotResurrectWithdrawnCandidate(t *testing.T) {
	f, h, r := hostedRequirementFixture(t)
	c := f.candidate(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		var b pullRequestBinding
		if e := tx.Get("pull_request", c.TaskID, &b); e != nil {
			return e
		}
		b.ReviewState = "pending"
		return tx.Put("pull_request", c.TaskID, b)
	}); e != nil {
		t.Fatal(e)
	}
	reads := h.reads
	h.base = strings.Repeat("d", 40)
	h.onInspect = func() {
		if h.reads == reads+2 {
			editHostedRequirement(t, f, r, "withdraw")
		}
	}
	refreshHosted(t, f)
	current := f.candidate(t)
	if current.State != "invalidated" || current.Commit != c.Commit || f.snapshot(t).Tasks[0].CurrentCandidateID != "" || h.merges != 0 {
		t.Fatal("late observation revived revoked work")
	}
	if e := f.w.invalidate(context.Background(), c, "stale main"); e != nil {
		t.Fatal(e)
	}
	if f.candidate(t).State != "invalidated" {
		t.Fatal("invalidation helper resurrected stale candidate")
	}
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if h.reviewCheck != "pending" {
		t.Fatal("in-flight success survived revocation")
	}
}

func TestGitHubPauseRevokesOnlyUnmergedApproval(t *testing.T) {
	f, h, _ := hostedRequirementFixture(t)
	if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		p.Paused = true
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
	ensures, creates := h.ensures, h.creates
	if e := f.w.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if h.reviewCheck != "pending" || h.ensures != ensures || h.creates != creates || h.merges != 0 {
		t.Fatal("pause failed to downgrade approval or published new work")
	}
}
