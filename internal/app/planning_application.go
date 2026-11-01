package app

import (
	"context"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Applying a result is separate from executing/settling it. The whole proposal
// is committed or rolled back; rejected proposals retain their failure evidence.
func (w *PlanningWorker) apply(ctx context.Context, id string) error {
	s := w.Service
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.PlanningRun
		if e := tx.Get("planning_run", id, &r); e != nil {
			return e
		}
		if r.Purpose == domain.ConsultationPurpose {
			return nil
		}
		if r.State != "pending_result" || r.PendingResult == nil && r.RequirementResult == nil && r.FailureResult == nil {
			return nil
		}
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		if p.Paused {
			return nil
		}
		r.Version++
		if d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion || !requirementPlanCurrent(tx, r) || !failurePlanningCurrent(tx, r) {
			r.State = "stale"
			if e = s.blockPlanningAssessment(tx, r, d, "goal or requirement changed before apply"); e != nil {
				return e
			}
		} else if r.Purpose == domain.FailureRecoveryPurpose {
			if e = (FailureRecoveries{Service: s}).apply(tx, &r, &d); e != nil {
				return e
			}
		} else if r.RequirementChangeID != "" {
			if r.RequirementResult == nil {
				return domain.Fail("invalid_result", "缺少需求变更结构化计划")
			}
			if e = s.applyRequirementPlan(tx, &r, d, *r.RequirementResult); e != nil {
				return e
			}
			r.State = "succeeded"
			message := domain.LeaderMessage{ID: ID("msg_"), DocumentVersion: d.Version, Role: "leader", Text: r.RequirementResult.Reply, CreatedAt: s.Now()}
			if e = tx.Put("leader_message", message.ID, message); e != nil {
				return e
			}
		} else {
			if e = s.derivePlan(tx, &r, &d, *r.PendingResult); e != nil {
				return e
			}
			if e := recordLeaderAnswer(tx, s, &r, &d); e != nil {
				return e
			}
		}
		if e = tx.Put("planning_document", "current", d); e != nil {
			return e
		}
		if e = tx.Put("planning_run", id, r); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "planning.applied", id, r.State)
	})
	if e == nil {
		return nil
	}
	// The failed transaction rolled back the entire proposal batch. Keep its
	// structured evidence and actionable failure without any partial tasks.
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.PlanningRun
		if err := tx.Get("planning_run", id, &r); err != nil {
			return err
		}
		if r.Purpose == domain.ConsultationPurpose {
			return nil
		}
		if r.State != "pending_result" {
			return nil
		}
		var d domain.PlanningDocument
		if err := tx.Get("planning_document", "current", &d); err != nil {
			return err
		}
		if r.ScopeRequestID != "" {
			var v domain.ScopeAssessment
			if err := tx.Get("scope_request", r.ScopeRequestID, &v); err != nil {
				return err
			}
			v.State = "blocked"
			v.Version++
			if err := tx.Put("scope_request", v.ID, v); err != nil {
				return err
			}
		}
		r.State = "blocked"
		r.Detail = e.Error()
		r.Version++
		if err := blockRequirementAssessment(tx, r, r.Detail); err != nil {
			return err
		}
		if err := blockFailureAssessment(tx, s, r, r.Detail); err != nil {
			return err
		}
		if err := tx.Put("planning_run", id, r); err != nil {
			return err
		}
		if queued, err := retryPlanningInputRejection(tx, s, r, d, e); err != nil || queued {
			return err
		}
		return s.decision(tx, "planning_run", id, r.Version, d, "invalid_plan", r.Detail, []string{"dismiss", "edit_goal", "evaluate"})
	})
}
func (w *PlanningWorker) Recover(ctx context.Context) error { return w.reconcile(ctx, true) }
