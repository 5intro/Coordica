package app

import (
	"context"
	"errors"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Paused/held work may refresh an existing PR, but cannot push, create a PR,
// report success, repair code, or merge. Observations never restore authority.
func (w *Integrator) observePullRequest(ctx context.Context, c domain.Candidate) error {
	if w.PullRequests == nil || c.Commit == "" {
		return nil
	}
	var p domain.Project
	var t domain.Task
	var b pullRequestBinding
	due := false
	e := w.Service.DB.Update(ctx, func(tx *store.Tx) error {
		var e error
		p, e = getProject(tx)
		if e != nil {
			return e
		}
		if !domain.GitHubPublication(p) {
			return nil
		}
		if e = tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		if t.CurrentCandidateID != c.ID {
			return nil
		}
		if e = tx.Get("pull_request", t.ID, &b); errors.Is(e, domain.ErrNotFound) {
			return nil
		} else if e != nil {
			return e
		}
		if b.Number == 0 || b.NextSync.After(w.Service.Now()) {
			return nil
		}
		b.NextSync = w.Service.Now().Add(30 * time.Second)
		due = true
		return tx.Put("pull_request", t.ID, b)
	})
	if e != nil || !due {
		return e
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	facts, e := w.PullRequests.Inspect(bounded, pullSpec(p, t, c, b))
	if e != nil {
		return w.recordPullError(ctx, c, "PR 只读同步失败；项目保持暂停或阻断")
	}
	facts.SyncedAt = w.Service.Now()
	return w.Service.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		var task domain.Task
		if e := tx.Get("task", t.ID, &task); e != nil {
			return e
		}
		if current.Commit != c.Commit || task.CurrentCandidateID != c.ID {
			return nil
		}
		if err := (ReviewIssues{Service: w.Service}).fenceExternal(tx, task, current, &facts); err != nil {
			return err
		}
		current.PullRequest = &facts
		current.Version++
		if e := tx.Put("candidate", c.ID, current); e != nil {
			return e
		}
		return w.Service.audit(tx, Actor{ID: "center"}, "pull_request.observed", c.ID, facts.State)
	})
}
