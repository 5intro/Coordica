package app

import (
	"context"
	"errors"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// A durable, exact-head downgrade survives removal of CurrentCandidateID and
// uncertain network responses. It grants no execution or merge authority.
type pullReviewRevocation struct {
	TaskID      string                 `json:"task_id"`
	CandidateID string                 `json:"candidate_id"`
	Spec        domain.PullRequestSpec `json:"spec"`
	NextSync    time.Time              `json:"next_sync"`
}

// Stop mutations persist this receipt before returning their ACK, even when
// the publication worker is blocked or the center exits before its next tick.
// An empty task selects all bindings for a project-wide authority change.
func queueTaskPullReviewRevocations(tx *store.Tx, task string) error {
	bindings, e := store.List[pullRequestBinding](tx, "pull_request")
	if e != nil {
		return e
	}
	for _, b := range bindings {
		if task != "" && b.TaskID != task || b.ReviewState != "success" && b.DesiredReview != "success" {
			continue
		}
		var c domain.Candidate
		if e = tx.Get("candidate", b.CandidateID, &c); e != nil {
			return e
		}
		if e = queuePullReviewRevocation(tx, c); e != nil {
			return e
		}
	}
	return nil
}

func queuePullReviewRevocation(tx *store.Tx, c domain.Candidate) error {
	var b pullRequestBinding
	if e := tx.Get("pull_request", c.TaskID, &b); errors.Is(e, domain.ErrNotFound) {
		return nil
	} else if e != nil {
		return e
	}
	if b.CandidateID != c.ID || c.State == "landed" {
		return nil
	}
	p, e := getProject(tx)
	if e != nil {
		return e
	}
	repository, _ := domain.GitHubRepository(p.RepositoryURL)
	for _, head := range []string{b.Head, b.Desired} {
		if head == "" {
			continue
		}
		id := c.ID + "/" + head
		var existing pullReviewRevocation
		if e = tx.Get("pull_review_revocation", id, &existing); e == nil {
			continue // Preserve retry backoff, including after a lost response.
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		// Base is irrelevant to a commit-status downgrade; use a retained fixed
		// object even if candidate rebuilding already cleared its former base.
		spec := domain.PullRequestSpec{Repository: repository, Branch: b.Branch, TargetBranch: p.TargetBranch, Head: head, Base: head, Number: b.Number}
		if e = tx.Put("pull_review_revocation", id, pullReviewRevocation{TaskID: c.TaskID, CandidateID: c.ID, Spec: spec}); e != nil {
			return e
		}
	}
	return nil
}

func publicationRevocationsPending(tx *store.Tx) (bool, error) {
	queued, e := store.List[pullReviewRevocation](tx, "pull_review_revocation")
	if e != nil || len(queued) != 0 {
		return len(queued) != 0, e
	}
	bindings, e := store.List[pullRequestBinding](tx, "pull_request")
	if e != nil {
		return false, e
	}
	for _, b := range bindings {
		if b.ReviewState != "success" && b.DesiredReview != "success" {
			continue
		}
		var c domain.Candidate
		if e := tx.Get("candidate", b.CandidateID, &c); e != nil {
			return false, e
		}
		if c.State != "landed" {
			return true, nil
		}
	}
	return false, nil
}

func (w *Integrator) reserveReviewStatus(ctx context.Context, c domain.Candidate) (string, bool, error) {
	state, allowed := "pending", false
	e := w.Service.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		var current domain.Candidate
		if e = tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		var t domain.Task
		if e = tx.Get("task", current.TaskID, &t); e != nil {
			return e
		}
		var b pullRequestBinding
		if e = tx.Get("pull_request", c.TaskID, &b); e != nil {
			return e
		}
		if current.Commit != c.Commit || !candidateAuthority(tx, t, p, current) || t.UsedSeconds >= t.BudgetSeconds || b.CandidateID != c.ID || b.Desired != c.Commit || current.State != "reviewing" && current.State != "ready_to_land" {
			return queuePullReviewRevocation(tx, c)
		}
		state, allowed = reviewCheck(p, t, current), true
		b.DesiredReview = state
		return tx.Put("pull_request", c.TaskID, b)
	})
	return state, allowed, e
}

func (w *Integrator) reconcilePullReviewRevocations(ctx context.Context) (bool, error) {
	s := w.Service
	var queued []pullReviewRevocation
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		bindings, e := store.List[pullRequestBinding](tx, "pull_request")
		if e != nil {
			return e
		}
		for _, b := range bindings {
			if b.ReviewState != "success" && b.DesiredReview != "success" {
				continue
			}
			var c domain.Candidate
			if e = tx.Get("candidate", b.CandidateID, &c); e != nil {
				return e
			}
			var t domain.Task
			if e = tx.Get("task", b.TaskID, &t); e != nil {
				return e
			}
			validState := c.State == "reviewing" || c.State == "ready_to_land" || c.State == "landing"
			if c.State != "landed" && (!validState || t.State != "awaiting_integration" || c.Commit != b.Desired || !candidateAuthority(tx, t, p, c) || reviewCheck(p, t, c) != "success" || t.UsedSeconds >= t.BudgetSeconds) {
				if e = queuePullReviewRevocation(tx, c); e != nil {
					return e
				}
			}
		}
		queued, e = store.List[pullReviewRevocation](tx, "pull_review_revocation")
		return e
	})
	if e != nil || len(queued) == 0 {
		return false, e
	}
	for _, revocation := range queued {
		if w.PullRequests == nil || revocation.NextSync.After(s.Now()) {
			continue
		}
		id := revocation.CandidateID + "/" + revocation.Spec.Head
		revocation.NextSync = s.Now().Add(30 * time.Second)
		if e = s.DB.Update(ctx, func(tx *store.Tx) error { return tx.Put("pull_review_revocation", id, revocation) }); e != nil {
			return true, e
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := w.PullRequests.PublishReview(bounded, revocation.Spec, "pending")
		cancel()
		if err != nil {
			continue // The persisted operation remains pending, including unknown ACK.
		}
		if e = s.DB.Update(ctx, func(tx *store.Tx) error {
			var b pullRequestBinding
			if e := tx.Get("pull_request", revocation.TaskID, &b); e != nil {
				return e
			}
			if b.CandidateID == revocation.CandidateID {
				if b.Head == revocation.Spec.Head {
					b.ReviewState = "pending"
				}
				if b.Desired == revocation.Spec.Head {
					b.DesiredReview = "pending"
				}
				if e := tx.Put("pull_request", b.TaskID, b); e != nil {
					return e
				}
			}
			if e := tx.Delete("pull_review_revocation", id); e != nil {
				return e
			}
			return s.audit(tx, Actor{ID: "center"}, "pull_request.review_revoked", revocation.CandidateID, revocation.Spec.Head)
		}); e != nil {
			return true, e
		}
	}
	var remaining []pullReviewRevocation
	e = s.DB.View(ctx, func(tx *store.Tx) error {
		var e error
		remaining, e = store.List[pullReviewRevocation](tx, "pull_review_revocation")
		return e
	})
	return len(remaining) != 0, e
}
