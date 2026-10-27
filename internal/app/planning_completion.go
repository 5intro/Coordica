package app

import (
	"context"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Receipt settlement remains one transaction: counters, stop evidence and the
// bound result are persisted together before any plan can be applied.
func (w *PlanningWorker) record(ctx context.Context, original domain.PlanningRun, out PlanningOutcome) error {
	s := w.Service
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.PlanningRun
		if e := tx.Get("planning_run", original.ID, &r); e != nil {
			return e
		}
		if r.ID != original.ID || r.Purpose != original.Purpose || r.FailureAssessmentID != original.FailureAssessmentID || r.RequirementChangeID != original.RequirementChangeID || r.AccountingTarget != original.AccountingTarget || r.ConversationID != original.ConversationID || r.AuthorizationVersion != original.AuthorizationVersion || r.Generation != out.Generation || r.DocumentVersion != out.DocumentVersion || out.RunID != "" && !planningReceiptMatches(r, out) {
			return domain.Fail("stale_run", "planning receipt generation mismatch")
		}
		if r.Settled && (r.State != "unknown_live" || !out.TreeStopped) {
			return nil
		}
		if r.Settled && r.State == "unknown_live" && out.TreeStopped {
			if !planningReceiptMatches(r, out) {
				return domain.Fail("stale_run", "late stop requires a bound same-Run receipt")
			}
			// Late trusted stop evidence closes only the barrier. Keep the full-grant
			// historical charge and every result/session/document field untouched.
			if e := s.reconcilePlanningStopDecisions(tx, r); e != nil {
				return e
			}
			r.TreeStopped = true
			r.State = "stopped"
			r.Version++
			if e := tx.Put("planning_run", r.ID, r); e != nil {
				return e
			}
			if e := syncConversation(tx, r); e != nil {
				return e
			}
			return s.audit(tx, Actor{ID: "center"}, "planning.late_stop", r.ID, "")
		}
		if r.Purpose == domain.ConsultationPurpose {
			return w.recordConsultation(tx, &r, out)
		}
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		retryQueued := false
		wasUnknown := r.State == "unknown_live"
		finished := out.FinishedAt
		if finished.IsZero() {
			finished = s.Now()
		}
		timely := finished.Before(r.LeaseUntil) && finished.Before(r.Deadline) && !finished.Before(r.StartedAt)
		if !r.Settled {
			elapsed := out.ElapsedSeconds
			wall := int64(finished.Sub(r.StartedAt)/time.Second) + 1
			if elapsed < wall {
				elapsed = wall
			}
			if !out.TreeStopped || !timely {
				elapsed = r.GrantSeconds
			}
			if elapsed < 0 {
				elapsed = 0
			}
			if elapsed > r.GrantSeconds {
				elapsed = r.GrantSeconds
			}
			r.ElapsedSeconds = elapsed
			d.ReservedSeconds -= r.GrantSeconds
			d.PlanningReservedSeconds -= r.GrantSeconds
			p.ReservedSeconds -= r.GrantSeconds
			d.UsedSeconds += elapsed
			d.PlanningUsedSeconds += elapsed
			p.UsedSeconds += elapsed
			p.Version++
			r.Settled = true
		}
		r.TreeStopped = out.TreeStopped
		r.SessionID = out.SessionID
		r.Detail = out.Error
		r.Version++
		if !out.TreeStopped {
			r.State = "unknown_live"
			if e = s.decision(tx, "planning_run", r.ID, r.Version, d, "unknown_live", "process tree stop could not be proven", []string{"confirm_stopped"}); e != nil {
				return e
			}
		} else if wasUnknown || !timely || d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion || r.State == "cancel_requested" || !requirementPlanCurrent(tx, r) || !failurePlanningCurrent(tx, r) {
			r.State = "stopped"
			if !timely && !wasUnknown && d.Version == r.DocumentVersion {
				if e = s.decision(tx, "planning_run", r.ID, r.Version, d, "planning_expired", "completion arrived outside its lease/deadline", []string{"dismiss", "evaluate"}); e != nil {
					return e
				}
			}
		} else if out.Error != "" {
			r.State = "failed"
			settings, e := automation(tx)
			if e != nil {
				return e
			}
			if r.Attempt < r.MaxRetries && d.PlanningUsedSeconds+d.PlanningReservedSeconds < settings.PlanningBudgetSeconds && d.UsedSeconds+d.ReservedSeconds < d.BudgetSeconds {
				retry := r
				retry.ID = ID("plan_")
				retry.Version = 1
				retry.Generation++
				retry.Attempt++
				retry.State = "queued"
				retry.Settled = false
				retry.TreeStopped = false
				retry.GrantSeconds = 0
				retry.ElapsedSeconds = 0
				retry.PendingResult = nil
				retry.RequirementResult = nil
				retry.FailureResult = nil
				retry.TaskIDs = nil
				retry.TaskMapping = nil
				retry.ProcessID = 0
				retry.WorkingDirectory = ""
				retryQueued = true
				if e = tx.Put("planning_run", retry.ID, retry); e != nil {
					return e
				}
				if r.FailureAssessmentID != "" {
					var f domain.FailureAssessment
					if e = tx.Get("failure_assessment", r.FailureAssessmentID, &f); e != nil {
						return e
					}
					f.RunID = retry.ID
					if e = (FailureRecoveries{Service: s}).save(tx, &f); e != nil {
						return e
					}
				}
			} else {
				if e = s.decision(tx, "planning_run", r.ID, r.Version, d, "planning_failure", out.Error, []string{"dismiss", "evaluate"}); e != nil {
					return e
				}
			}
		} else {
			var result domain.LeaderResult
			var requirementResult *domain.RequirementPlan
			var failureResult *domain.FailureRecoveryPlan
			var err error
			if r.Purpose == domain.FailureRecoveryPurpose {
				var plan domain.FailureRecoveryPlan
				plan, err = decodeFailurePlan(out.Output)
				failureResult = &plan
			} else if r.RequirementChangeID != "" {
				var plan domain.RequirementPlan
				plan, err = decodeRequirementPlan(out.Output)
				requirementResult = &plan
			} else {
				result, err = decodeLeader(out.Output)
			}
			if err != nil {
				r.State = "failed"
				r.Detail = err.Error()
				if e = s.decision(tx, "planning_run", r.ID, r.Version, d, "invalid_result", r.Detail, []string{"dismiss", "evaluate"}); e != nil {
					return e
				}
			} else {
				r.State = "pending_result"
				if failureResult != nil {
					r.FailureResult = failureResult
				} else if requirementResult != nil {
					r.RequirementResult = requirementResult
				} else {
					r.PendingResult = &result
				}
			}
		}
		if r.State == "stopped" || r.State == "unknown_live" || r.State == "failed" && !retryQueued {
			if e = s.blockPlanningAssessment(tx, r, d, "assessment stopped, expired or failed: "+r.Detail); e != nil {
				return e
			}
		}
		if e = tx.Put("project", p.ID, p); e != nil {
			return e
		}
		if e = tx.Put("planning_document", "current", d); e != nil {
			return e
		}
		if e = tx.Put("planning_run", r.ID, r); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "planning.finished", r.ID, r.State)
	})
	if e != nil {
		return e
	}
	if e := s.cleanupPlanningDirectory(ctx, original.ID); e != nil {
		return e
	}
	return w.apply(ctx, original.ID)
}
