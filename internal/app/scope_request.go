package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"fmt"
	"strings"
)

// RequestScope stops the exact running implementation. It conveys no authority;
// the planning worker assesses the request only after durable stop evidence.
func (s *Service) RequestScope(ctx context.Context, a Actor, key, id string, req domain.RunScopeRequest) (json.RawMessage, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, domain.Fail("invalid_input", "scope reason required")
	}
	if len(req.Paths) == 0 {
		return s.Block(ctx, a, key, id, req.Generation, "request_scope: "+req.Reason)
	}
	if a.Role != "node" || strings.TrimSpace(req.Reason) == "" || len(req.Paths) > 100 {
		return nil, domain.Fail("invalid_input", "node, reason and bounded paths required")
	}
	return s.mutateWithCheck(ctx, a, key, "run.scope."+id, req, func(tx *store.Tx) error {
		_, err := nodeConsoleActor(tx, a)
		return err
	}, func(tx *store.Tx) (any, error) {
		var r domain.Run
		if e := tx.Get("run", id, &r); e != nil {
			return nil, e
		}
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return nil, e
		}
		if r.NodeID != a.NodeID || r.Generation != req.Generation || r.Kind != "implement" || r.State != "running" || !s.Now().Before(r.LeaseUntil) || t.CurrentRunID != r.ID || t.Generation != r.Generation || t.AuthorizationVersion != r.AuthorizationVersion || t.State != "executing" {
			return nil, domain.Fail("stale_run", "scope request does not own the live implementation")
		}
		settings, e := automation(tx)
		if e != nil {
			return nil, e
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		v := domain.ScopeAssessment{ID: ID("scope_"), Version: 1, RunID: r.ID, Generation: r.Generation, TaskID: t.ID, TaskVersion: t.Version + 1, AuthorizationVersion: t.AuthorizationVersion, GoalVersion: t.GoalVersion, GoalAuthorizationVersion: t.GoalAuthorizationVersion, AutomationVersion: settings.Version, PolicyVersion: p.PolicyVersion, Reason: req.Reason, Paths: req.Paths, State: "waiting_stop"}
		t.HoldReason = "scope_requested"
		t.Version++
		r.State = "cancel_requested"
		r.Detail = req.Reason
		r.Version++
		if e = tx.Put("scope_request", v.ID, v); e != nil {
			return nil, e
		}
		if e = tx.Put("task", t.ID, t); e != nil {
			return nil, e
		}
		if e = tx.Put("run", r.ID, r); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "run.scope_requested", r.ID, v.ID)
	})
}

func (w *PlanningWorker) queueScopes(ctx context.Context) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		requests, e := store.List[domain.ScopeAssessment](tx, "scope_request")
		if e != nil {
			return e
		}
		plans, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return e
		}
		// Advisory execution cannot incidentally drive scope assessment or
		// create Owner decisions in the shared worker's preflight.
		for _, r := range plans {
			if r.Purpose == domain.ConsultationPurpose && (r.State == "queued" || r.State == "running" || r.State == "pending_result" || r.State == "unknown_live" || r.State == "cancel_requested") {
				return nil
			}
		}
		for _, v := range requests {
			if v.State != "waiting_stop" {
				continue
			}
			var old domain.Run
			if e = tx.Get("run", v.RunID, &old); e != nil {
				return e
			}
			var t domain.Task
			if e = tx.Get("task", v.TaskID, &t); e != nil {
				return e
			}
			settings, e := automation(tx)
			if e != nil {
				return e
			}
			p, e := getProject(tx)
			if e != nil {
				return e
			}
			var d domain.PlanningDocument
			if e = tx.Get("planning_document", "current", &d); e != nil {
				return e
			}
			reason := ""
			if old.Generation != v.Generation || old.AuthorizationVersion != v.AuthorizationVersion || t.Generation != v.Generation || t.AuthorizationVersion != v.AuthorizationVersion || t.GoalVersion != v.GoalVersion || t.GoalAuthorizationVersion != v.GoalAuthorizationVersion || t.HoldReason != "scope_requested" || (t.CurrentRunID != "" && t.CurrentRunID != old.ID) || t.CurrentCandidateID != "" || t.State != "executing" {
				reason = "scope request authority changed"
			}
			if goalGate(tx, &t, false) != nil || !settings.Enabled || settings.Version != v.AutomationVersion || p.PolicyVersion != v.PolicyVersion || p.Paused {
				reason = "goal or automation policy changed"
			}
			if reason == "" && !old.Quiescent {
				if old.State == "lost" {
					if e = s.scopeWaitDecision(tx, v, old, d); e != nil {
						return e
					}
				}
				continue
			}
			if reason != "" {
				if e = s.closeScopeWait(tx, v.ID, "superseded", reason); e != nil {
					return e
				}
			} else {
				if e = s.closeScopeWait(tx, v.ID, "resolved", "actual original Run stop received; assessment resumes automatically"); e != nil {
					return e
				}
				if (old.State != "stopped" && old.State != "lost") || t.CurrentRunID != "" {
					reason = "old execution not stopped"
				}
			}
			if literalScope(v.Paths, d.AllowedRoots) != nil || protectedOverlap(v.Paths, p.ProtectedPaths) {
				reason = "requested paths outside ordinary goal permissions"
			}
			if t.UsedSeconds+t.ReservedSeconds >= t.BudgetSeconds || t.Retries >= t.MaxRetries || d.PlanningUsedSeconds+d.PlanningReservedSeconds >= settings.PlanningBudgetSeconds {
				reason = "scope assessment budget or retries exhausted"
			}
			count := 0
			for _, prior := range requests {
				if prior.TaskID == t.ID {
					count++
				}
			}
			if count > t.MaxRetries+1 {
				reason = "scope assessment count exhausted"
			}
			if e = requireQuiescence(tx, t.ID); e != nil {
				reason = "task process tree is unknown"
			}
			if reason != "" {
				v.State = "blocked"
				v.Version++
				if e = tx.Put("scope_request", v.ID, v); e != nil {
					return e
				}
				if e = s.decision(tx, "task", t.ID, t.Version, d, "scope", reason, []string{"edit_scope", "edit_goal", "edit_budget", "dismiss"}); e != nil {
					return e
				}
				continue
			}
			busy := false
			for _, r := range plans {
				if r.State == "queued" || r.State == "running" || r.State == "pending_result" || r.State == "unknown_live" || r.State == "cancel_requested" {
					busy = true
				}
			}
			if busy {
				continue
			}
			r := domain.PlanningRun{ID: ID("plan_"), Version: 1, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion, AutomationVersion: settings.Version, Generation: 1, State: "queued", MaxRetries: settings.PlanningMaxRetries, ScopeRequestID: v.ID}
			v.State = "queued"
			v.TaskVersion = t.Version
			v.Version++
			if e = tx.Put("planning_run", r.ID, r); e != nil {
				return e
			}
			if e = tx.Put("scope_request", v.ID, v); e != nil {
				return e
			}
			return s.audit(tx, Actor{ID: "center"}, "scope.assessment_queued", v.ID, r.ID)
		}
		return nil
	})
}

// Stop prerequisites are navigation, not submit-to-resolve choices. Reuse the
// exact request's open decision and refresh only changed bound evidence.
func (s *Service) scopeWaitDecision(tx *store.Tx, scope domain.ScopeAssessment, run domain.Run, d domain.PlanningDocument) error {
	proof := fmt.Sprintf("Scope assessment requires actual process-tree inspection. Inspect original Run %s, generation=%d. Use POST /api/v1/runs/%s/confirm-stopped with expected_version=%d and owner inspection evidence. inspect_run is source navigation only; this decision cannot confirm or acknowledge a stop. Assessment resumes automatically after the shared Run stop-proof interface succeeds.", run.ID, scope.Generation, run.ID, run.Version)
	all, err := store.List[domain.Decision](tx, "decision")
	if err != nil {
		return err
	}
	var kept bool
	for _, v := range all {
		if v.Status != "open" || v.Kind != "scope_wait_stop" || v.SourceKind != "scope_request" || v.SourceID != scope.ID {
			continue
		}
		if kept || v.SourceVersion != scope.Version || v.DocumentVersion != d.Version || v.AuthorizationVersion != d.AuthorizationVersion {
			v.Status = "superseded"
			v.Resolution = "scope stop prerequisite source changed or duplicate"
			v.Version++
			if err = tx.Put("decision", v.ID, v); err != nil {
				return err
			}
			continue
		}
		kept = true
		if v.Evidence != proof || len(v.Actions) != 1 || v.Actions[0] != "inspect_run" {
			v.Evidence = proof
			v.Actions = []string{"inspect_run"}
			v.Version++
			if err = tx.Put("decision", v.ID, v); err != nil {
				return err
			}
			if err = s.audit(tx, Actor{ID: "center"}, "decision.updated", v.ID, proof); err != nil {
				return err
			}
		}
	}
	if kept {
		return nil
	}
	return s.decision(tx, "scope_request", scope.ID, scope.Version, d, "scope_wait_stop", proof, []string{"inspect_run"})
}

func (s *Service) closeScopeWait(tx *store.Tx, scopeID, status, reason string) error {
	all, err := store.List[domain.Decision](tx, "decision")
	if err != nil {
		return err
	}
	for _, v := range all {
		if v.Status == "open" && v.Kind == "scope_wait_stop" && v.SourceKind == "scope_request" && v.SourceID == scopeID {
			v.Status = status
			v.Resolution = reason
			v.Version++
			if err = tx.Put("decision", v.ID, v); err != nil {
				return err
			}
			if err = s.audit(tx, Actor{ID: "center"}, "decision."+status, v.ID, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// deriveScope changes only the original task's ordinary path authority. All
// receipt, task, goal, automation and policy fences are checked in this commit.
func (s *Service) deriveScope(tx *store.Tx, r *domain.PlanningRun, d domain.PlanningDocument, result domain.LeaderResult, settings domain.AutomationSettings, p domain.Project) error {
	var v domain.ScopeAssessment
	if e := tx.Get("scope_request", r.ScopeRequestID, &v); e != nil {
		return e
	}
	var t domain.Task
	if e := tx.Get("task", v.TaskID, &t); e != nil {
		return e
	}
	var old domain.Run
	if e := tx.Get("run", v.RunID, &old); e != nil {
		return e
	}
	if v.State != "queued" || v.AutomationVersion != settings.Version || !settings.Enabled || r.AutomationVersion != settings.Version || v.PolicyVersion != p.PolicyVersion || p.Paused || v.TaskVersion != t.Version || v.Generation != t.Generation || old.Generation != v.Generation || old.AuthorizationVersion != v.AuthorizationVersion || !old.Quiescent || (old.State != "stopped" && old.State != "lost") || t.AuthorizationVersion != v.AuthorizationVersion || t.GoalVersion != v.GoalVersion || t.GoalAuthorizationVersion != v.GoalAuthorizationVersion || t.HoldReason != "scope_requested" || t.CurrentRunID != "" || t.CurrentCandidateID != "" || t.State != "executing" {
		return domain.Fail("conflict", "scope request authority changed")
	}
	if e := goalGate(tx, &t, false); e != nil {
		return e
	}
	if e := requireQuiescence(tx, t.ID); e != nil {
		return e
	}
	if len(result.Tasks) != 0 {
		return domain.Fail("scope", "scope assessment cannot create new work or goals")
	}
	if len(result.MajorDecisions) > 0 || !result.Ready || len(result.MissingItems) > 0 || len(result.Clarifications) > 0 {
		v.State = "blocked"
		v.Version++
		if e := tx.Put("scope_request", v.ID, v); e != nil {
			return e
		}
		return s.decision(tx, "task", t.ID, t.Version, d, "scope", "Leader cannot admit ordinary scope: "+result.Reply, []string{"edit_scope", "edit_goal", "dismiss"})
	}
	if len(result.Scopes) != 1 {
		return domain.Fail("scope", "scope assessment requires exactly its original task")
	}
	q := result.Scopes[0]
	if q.TaskID != t.ID || q.ExpectedVersion != t.Version || strings.TrimSpace(q.Reason) == "" || !containsPaths(q.Paths, t.AllowedPaths) || !containsPaths(q.Paths, v.Paths) {
		return domain.Fail("scope", "scope must extend the exact request and preserve previous authority")
	}
	// No additional permission beyond the old paths and exact requested paths.
	permitted := append(append([]string{}, t.AllowedPaths...), v.Paths...)
	if !containsPaths(permitted, q.Paths) {
		return domain.Fail("scope", "assessment invented additional permissions")
	}
	if e := literalScope(q.Paths, d.AllowedRoots); e != nil {
		return e
	}
	if protectedOverlap(q.Paths, p.ProtectedPaths) {
		return domain.Fail("protected_scope", "ordinary scope intersects protected paths")
	}
	if t.UsedSeconds+t.ReservedSeconds >= t.BudgetSeconds || t.Retries >= t.MaxRetries || d.UsedSeconds+d.ReservedSeconds >= d.BudgetSeconds {
		return domain.Fail("budget", "cumulative budget or retries exhausted")
	}
	for _, hot := range t.HotResources {
		var res domain.Resource
		e := tx.Get("resource", hot, &res)
		if e == nil && res.TaskID != t.ID {
			return domain.Fail("conflict", "resource owned by another task")
		}
		if e != nil && e != domain.ErrNotFound {
			return e
		}
	}
	t.AllowedPaths = append([]string{}, q.Paths...)
	t.AuthorizationVersion++
	t.Version++
	t.HoldReason = ""
	t.State = "ready"
	t.ApprovedVersion = t.Version
	v.State = "consumed"
	v.Version++
	if e := tx.Put("task", t.ID, t); e != nil {
		return e
	}
	if e := tx.Put("scope_request", v.ID, v); e != nil {
		return e
	}
	decisions, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, decision := range decisions {
		if decision.Status == "open" && decision.SourceKind == "scope_request" && decision.SourceID == v.ID && decision.Kind == "scope_wait_stop" {
			decision.Status = "resolved"
			decision.Version++
			decision.Resolution = "actual old stop received; ordinary scope consumed"
			if e = tx.Put("decision", decision.ID, decision); e != nil {
				return e
			}
		}
	}
	return s.audit(tx, Actor{ID: "leader"}, "task.scope", t.ID, v.ID+": "+q.Reason)
}

func (s *Service) blockScopeAssessment(tx *store.Tx, r domain.PlanningRun, d domain.PlanningDocument, detail string) error {
	if r.ScopeRequestID == "" {
		return nil
	}
	var v domain.ScopeAssessment
	if e := tx.Get("scope_request", r.ScopeRequestID, &v); e != nil {
		return e
	}
	if v.State == "consumed" || v.State == "blocked" {
		return nil
	}
	v.State = "blocked"
	v.Version++
	if e := tx.Put("scope_request", v.ID, v); e != nil {
		return e
	}
	return s.decision(tx, "planning_run", r.ID, r.Version, d, "scope", detail, []string{"edit_scope", "edit_goal", "edit_budget", "evaluate", "dismiss"})
}
