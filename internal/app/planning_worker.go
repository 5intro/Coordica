package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"coordica/internal/domain"
	"coordica/internal/localfile"
	"coordica/internal/store"
)

// PlanningExecutor is owned by the planning worker. Deterministic test
// executors use the same fencing and accounting, without native tool claims.
type PlanningExecutor interface {
	Execute(context.Context, string, string, func(int) error) (PlanningOutcome, error)
}
type PlanningOutcome struct {
	RequirementChangeID  string    `json:"requirement_change_id,omitempty"`
	RunID                string    `json:"run_id"`
	Purpose              string    `json:"purpose"`
	AccountingTarget     string    `json:"accounting_target"`
	ConversationID       string    `json:"conversation_id"`
	AuthorizationVersion int64     `json:"authorization_version"`
	FinishedAt           time.Time `json:"finished_at"`
	Generation           int64     `json:"generation"`
	DocumentVersion      int64     `json:"document_version"`
	Output               string    `json:"output"`
	SessionID            string    `json:"session_id"`
	TreeStopped          bool      `json:"tree_stopped"`
	ElapsedSeconds       int64     `json:"elapsed_seconds"`
	Error                string    `json:"error"`
}
type PlanningWorker struct {
	Service          *Service
	Executor         PlanningExecutor
	ExecutionAdapter string
}

func (w *PlanningWorker) receipt(id string) string {
	return filepath.Join(w.Service.Dir, "planning-outcomes", id+".json")
}
func (w *PlanningWorker) reserve(ctx context.Context) (domain.PlanningRun, domain.PlanningDocument, []domain.LeaderMessage, error) {
	s := w.Service
	var run domain.PlanningRun
	var d domain.PlanningDocument
	var messages []domain.LeaderMessage
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		busy, e := initializationBusy(tx)
		if e != nil {
			return e
		}
		if busy {
			return nil
		}
		runs, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return e
		}
		for _, r := range runs {
			if r.State == "running" || r.State == "unknown_live" || r.State == "pending_result" || r.State == "cancel_requested" && !r.Settled && r.GrantSeconds > 0 {
				return nil
			}
		}
		for _, r := range runs {
			if r.State != "queued" {
				continue
			}
			if r.Purpose == domain.ConsultationPurpose {
				d, e = goalDocument(tx)
			} else {
				e = tx.Get("planning_document", "current", &d)
			}
			if e != nil {
				return e
			}
			p, e := getProject(tx)
			if e != nil {
				return e
			}
			settings, e := automation(tx)
			if e != nil {
				return e
			}
			if r.Purpose == domain.ConsultationPurpose {
				if e = w.reserveConsultation(tx, &r, d, &p, settings); e != nil {
					return e
				}
				if r.State == "running" {
					run = r
				}
				return nil
			}
			requirements, e := authorizedGoalRequirements(tx, d)
			if e != nil {
				return e
			}
			ids := make([]string, 0, len(requirements)+1)
			for _, requirement := range requirements {
				ids = append(ids, requirement.ID)
			}
			if r.RequirementChangeID != "" {
				var change domain.RequirementChange
				if e = tx.Get("requirement_change", r.RequirementChangeID, &change); e != nil {
					return e
				}
				ids = append(ids, change.RequirementID)
			}
			r.RequirementRevisions, e = requirementPlanBindings(tx, ids)
			if e != nil {
				return e
			}
			if d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion || (r.ScopeRequestID != "" || r.RequirementChangeID != "") && (!settings.Enabled || settings.Version != r.AutomationVersion) || !requirementPlanCurrent(tx, r) {
				r.State = "stale"
				r.Version++
				if e = s.blockPlanningAssessment(tx, r, d, "goal, requirement or automation policy changed before assessment"); e != nil {
					return e
				}
				return tx.Put("planning_run", r.ID, r)
			}
			baseline, e := initialization(tx)
			if e != nil {
				return e
			}
			if p.Paused || baselineGate(tx, nil, false) != nil || r.BaselineVersion != baseline.ApprovedVersion || r.BaselineDigest != baseline.ApprovedDigest {
				return nil
			}
			grant := limit(settings.PlanningTimeoutSeconds, settings.PlanningBudgetSeconds-d.PlanningUsedSeconds-d.PlanningReservedSeconds, d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds)
			if grant <= 0 {
				r.State = "blocked"
				r.Detail = "planning or goal budget exhausted"
				r.Version++
				if e = tx.Put("planning_run", r.ID, r); e != nil {
					return e
				}
				if e = s.blockPlanningAssessment(tx, r, d, r.Detail); e != nil {
					return e
				}
				return s.decision(tx, "planning_run", r.ID, r.Version, d, "budget", r.Detail, []string{"dismiss", "edit_budget"})
			}
			r.State = "running"
			r.ExecutionAdapter = "injected"
			if w.ExecutionAdapter != "" {
				r.ExecutionAdapter = w.ExecutionAdapter
			}
			r.Version++
			r.StartedAt = s.Now()
			r.GrantSeconds = grant
			r.Deadline = r.StartedAt.Add(time.Duration(grant) * time.Second)
			r.LeaseUntil = r.StartedAt.Add(s.Lease)
			if r.Deadline.Before(r.LeaseUntil) {
				r.LeaseUntil = r.Deadline
			}
			d.ReservedSeconds += grant
			d.PlanningReservedSeconds += grant
			p.ReservedSeconds += grant
			p.Version++
			if e = tx.Put("planning_document", "current", d); e != nil {
				return e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return e
			}
			if e = tx.Put("planning_run", r.ID, r); e != nil {
				return e
			}
			if e = s.audit(tx, Actor{ID: "center"}, "planning.started", r.ID, ""); e != nil {
				return e
			}
			messages, e = store.List[domain.LeaderMessage](tx, "leader_message")
			run = r
			return e
		}
		return nil
	})
	return run, d, messages, e
}
func (w *PlanningWorker) Tick(ctx context.Context) error {
	if e := w.reconcile(ctx, false); e != nil {
		return e
	}
	if e := w.queueScopes(ctx); e != nil {
		return e
	}
	if e := w.queueRequirementChanges(ctx); e != nil {
		return e
	}
	if w.Executor == nil {
		return errors.New("planning executor required")
	}
	r, d, messages, e := w.reserve(ctx)
	if e != nil || r.ID == "" {
		return e
	}
	// Keep the read-only working directory away from center credentials and
	// publication state. Only document/chat context is supplied to the adapter.
	dir, e := os.MkdirTemp("", "coordica-planning-"+r.ID+"-"+fmt.Sprint(r.Generation)+"-")
	if e != nil {
		return w.record(ctx, r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Error: e.Error()})
	}
	if e = w.Service.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.PlanningRun
		if e := tx.Get("planning_run", r.ID, &current); e != nil {
			return e
		}
		if current.State != "running" || current.Generation != r.Generation || current.Purpose == domain.ConsultationPurpose && !consultationCurrent(tx, current) {
			return domain.Fail("stale_run", "planning canceled before launch")
		}
		current.WorkingDirectory = dir
		return tx.Put("planning_run", r.ID, current)
	}); e != nil {
		os.RemoveAll(dir)
		return w.record(context.WithoutCancel(ctx), r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Error: e.Error()})
	}
	var prompt string
	if r.RequirementChangeID != "" {
		prompt, e = w.requirementPrompt(ctx, r, dir)
		if e != nil {
			os.RemoveAll(dir)
			return w.record(context.WithoutCancel(ctx), r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Error: e.Error()})
		}
	} else if r.Purpose == domain.ConsultationPurpose {
		prompt, e = w.consultationPrompt(ctx, r)
		if e != nil {
			os.RemoveAll(dir)
			return w.record(context.WithoutCancel(ctx), r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Error: e.Error()})
		}
	} else {
		prompt, e = w.planningPrompt(ctx, r, d, messages, dir)
		if e != nil {
			os.RemoveAll(dir)
			return w.record(context.WithoutCancel(ctx), r, PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, TreeStopped: true, Error: e.Error()})
		}

	}
	execCtx, finish := startPlanningWatch(ctx, w, r)
	defer finish()
	outcome, err := executePlanningLeader(execCtx, w, r, dir, prompt)
	if outcome.TreeStopped {
		defer os.RemoveAll(dir)
	}
	outcome.FinishedAt = w.Service.Now()
	outcome.Generation = r.Generation
	outcome.DocumentVersion = r.DocumentVersion
	bindPlanningOutcome(&outcome, r)
	if err != nil {
		outcome.Error = errDetail(err)
	}
	// Persist evidence before database completion. Recovery never relies on PID
	// disappearance or a model's assertion that execution has stopped.
	data, e := json.Marshal(outcome)
	if e == nil {
		e = os.MkdirAll(filepath.Dir(w.receipt(r.ID)), 0700)
	}
	if e == nil {
		e = localfile.WritePrivate(w.receipt(r.ID), data)
	}
	if e != nil {
		outcome.Error = "durable receipt failed: " + e.Error()
		if settleErr := w.record(context.WithoutCancel(ctx), r, outcome); settleErr != nil {
			return errors.Join(e, settleErr)
		}
		return e
	}
	return w.record(context.WithoutCancel(ctx), r, outcome)
}
func (w *PlanningWorker) record(ctx context.Context, original domain.PlanningRun, out PlanningOutcome) error {
	s := w.Service
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		var r domain.PlanningRun
		if e := tx.Get("planning_run", original.ID, &r); e != nil {
			return e
		}
		if r.ID != original.ID || r.Purpose != original.Purpose || r.RequirementChangeID != original.RequirementChangeID || r.AccountingTarget != original.AccountingTarget || r.ConversationID != original.ConversationID || r.AuthorizationVersion != original.AuthorizationVersion || r.Generation != out.Generation || r.DocumentVersion != out.DocumentVersion || out.RunID != "" && !planningReceiptMatches(r, out) {
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
		} else if wasUnknown || !timely || d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion || r.State == "cancel_requested" || !requirementPlanCurrent(tx, r) {
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
				retry.TaskIDs = nil
				retry.TaskMapping = nil
				retry.ProcessID = 0
				retry.WorkingDirectory = ""
				retryQueued = true
				if e = tx.Put("planning_run", retry.ID, retry); e != nil {
					return e
				}
			} else {
				if e = s.decision(tx, "planning_run", r.ID, r.Version, d, "planning_failure", out.Error, []string{"dismiss", "evaluate"}); e != nil {
					return e
				}
			}
		} else {
			var result domain.LeaderResult
			var requirementResult *domain.RequirementPlan
			var err error
			if r.RequirementChangeID != "" {
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
				if requirementResult != nil {
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

// Close only the open stop decision fenced to this exact pre-stop source
// version. Its evidence and identity remain historical; no authority is granted.
func (s *Service) reconcilePlanningStopDecisions(tx *store.Tx, r domain.PlanningRun) error {
	all, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, d := range all {
		if d.Status != "open" || d.Kind != "unknown_live" || d.SourceKind != "planning_run" || d.SourceID != r.ID || d.SourceVersion != r.Version {
			continue
		}
		d.Status = "resolved"
		d.Resolution = "reconciled: authenticated same-Run process tree stop receipt"
		d.Version++
		if e = tx.Put("decision", d.ID, d); e != nil {
			return e
		}
		if e = s.audit(tx, Actor{ID: "center"}, "decision.stop_reconciled", d.ID, d.Resolution); e != nil {
			return e
		}
	}
	return nil
}
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
		if r.State != "pending_result" || r.PendingResult == nil && r.RequirementResult == nil {
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
		if d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion || !requirementPlanCurrent(tx, r) {
			r.State = "stale"
			if e = s.blockPlanningAssessment(tx, r, d, "goal or requirement changed before apply"); e != nil {
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
		if err := tx.Put("planning_run", id, r); err != nil {
			return err
		}
		return s.decision(tx, "planning_run", id, r.Version, d, "invalid_plan", r.Detail, []string{"dismiss", "edit_goal", "evaluate"})
	})
}
func (w *PlanningWorker) Recover(ctx context.Context) error { return w.reconcile(ctx, true) }
func (w *PlanningWorker) reconcile(ctx context.Context, restart bool) error {
	var runs []domain.PlanningRun
	if e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var e error
		runs, e = store.List[domain.PlanningRun](tx, "planning_run")
		return e
	}); e != nil {
		return e
	}
	for _, r := range runs {
		if r.TreeStopped && r.WorkingDirectory != "" {
			if e := w.Service.cleanupPlanningDirectory(ctx, r.ID); e != nil {
				return e
			}
		}
		if r.State == "pending_result" {
			if e := w.apply(ctx, r.ID); e != nil {
				return e
			}
			continue
		}
		if r.State != "running" && r.State != "cancel_requested" && r.State != "unknown_live" {
			continue
		}
		if r.GrantSeconds == 0 {
			if e := w.Service.DB.Update(ctx, func(tx *store.Tx) error {
				var current domain.PlanningRun
				if e := tx.Get("planning_run", r.ID, &current); e != nil {
					return e
				}
				if current.Generation != r.Generation || current.GrantSeconds != 0 || current.State != "cancel_requested" {
					return nil
				}
				r = current
				r.State = "stopped"
				r.TreeStopped = true
				r.Settled = true
				r.Version++
				if e := tx.Put("planning_run", r.ID, r); e != nil {
					return e
				}
				return syncConversation(tx, r)
			}); e != nil {
				return e
			}
			continue
		}
		out := PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, Error: "center restart: no durable process-tree stop evidence"}
		data, e := os.ReadFile(w.receipt(r.ID))
		if e == nil {
			if e = json.Unmarshal(data, &out); e != nil || !planningReceiptMatches(r, out) {
				// Mismatched/untrusted evidence cannot clear a stop barrier.
				if r.State == "unknown_live" {
					continue
				}
				out = PlanningOutcome{Generation: r.Generation, DocumentVersion: r.DocumentVersion, Error: "invalid durable receipt"}
			}
			if r.State == "unknown_live" && !out.TreeStopped {
				continue
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		} else if r.State == "unknown_live" || !restart && w.Service.Now().Before(r.LeaseUntil) {
			continue
		}
		if e = w.record(ctx, r, out); e != nil {
			return e
		}
	}
	return nil
}

func bindPlanningOutcome(out *PlanningOutcome, r domain.PlanningRun) {
	out.RequirementChangeID = r.RequirementChangeID
	out.RunID = r.ID
	out.Purpose = r.Purpose
	out.AccountingTarget = r.AccountingTarget
	out.ConversationID = r.ConversationID
	out.AuthorizationVersion = r.AuthorizationVersion
}
func planningReceiptMatches(r domain.PlanningRun, out PlanningOutcome) bool {
	return out.RunID == r.ID && out.Purpose == r.Purpose && out.RequirementChangeID == r.RequirementChangeID && out.AccountingTarget == r.AccountingTarget && out.ConversationID == r.ConversationID && out.AuthorizationVersion == r.AuthorizationVersion && out.Generation == r.Generation && out.DocumentVersion == r.DocumentVersion
}

func (s *Service) blockPlanningAssessment(tx *store.Tx, r domain.PlanningRun, d domain.PlanningDocument, detail string) error {
	if e := blockRequirementAssessment(tx, r, detail); e != nil {
		return e
	}
	return s.blockScopeAssessment(tx, r, d, detail)
}
