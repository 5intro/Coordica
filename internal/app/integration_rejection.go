package app

import (
	"context"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/process"
	"coordica/internal/store"
)

// Rejection binds any newly built fixed repair input and transitions the
// candidate, task, decision and audit in one version-fenced transaction.
func (w *Integrator) reject(ctx context.Context, c domain.Candidate, cause error, repairable bool) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		if t.CurrentCandidateID != c.ID {
			return nil
		}
		if current.Commit == "" && c.Commit != "" {
			if current.State != "accepted" || current.Version != c.Version || current.Base != c.Base || current.Head != c.Head {
				return domain.Fail("candidate_changed", "built repair input no longer matches the accepted candidate")
			}
			current.Main, current.Commit = c.Main, c.Commit
			current.Protected = current.Protected || c.Protected
		}
		current.State = "rejected"
		current.PreparationStopped = !errors.Is(cause, process.ErrTreeNotStopped)
		current.Detail = errDetail(cause)
		current.Version++
		project, e := getProject(tx)
		if e != nil {
			return e
		}
		if !project.Paused && t.AuthorizationVersion == current.AuthorizationVersion && project.PolicyVersion == current.PolicyVersion {
			queueRepair(&t, &current, repairable)
		} else if project.Paused && t.HoldReason == "" && t.State != "cancelled" && t.State != "integrated" {
			t.HoldReason = "paused"
		}
		t.Version++
		if e := tx.Put("candidate", current.ID, current); e != nil {
			return e
		}
		if e := tx.Put("task", t.ID, t); e != nil {
			return e
		}
		if e := s.taskDecision(tx, t, current.Detail); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.rejected", c.ID, current.Detail)
	})
}
