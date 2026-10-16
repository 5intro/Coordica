package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func automation(tx *store.Tx) (domain.AutomationSettings, error) {
	var v domain.AutomationSettings
	err := tx.Get("automation", "current", &v)
	if errors.Is(err, domain.ErrNotFound) {
		v = domain.DefaultAutomationSettings()
		err = tx.Put("automation", "current", v)
	}
	return v, err
}
func (s *Service) ConfigureAutomation(ctx context.Context, a Actor, key string, req domain.AutomationRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	v := req.Settings
	if v.DevelopmentTool != "codex" || v.LeaderTool != "codex" || v.DevelopmentModel != "gpt-6.1-sol" || v.LeaderModel != "gpt-6.1-sol" || v.PlanningTimeoutSeconds < 1 || v.PlanningTimeoutSeconds > domain.MaxRunDurationSeconds || v.PlanningBudgetSeconds < v.PlanningTimeoutSeconds || v.PlanningBudgetSeconds > domain.MaxTaskBudgetSeconds || v.PlanningMaxRetries < 0 || v.PlanningMaxRetries > 2 {
		return nil, domain.Fail("invalid_input", "finite Codex gpt-6.1-sol settings required")
	}
	return s.mutate(ctx, a, key, "automation.configure", req, func(tx *store.Tx) (any, error) {
		old, e := automation(tx)
		if e != nil {
			return nil, e
		}
		if e = version(old.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		v.Version = old.Version + 1
		if e = tx.Put("automation", "current", v); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "automation.configured", "current", "")
	})
}
func documentMissing(d domain.PlanningDocument) []string {
	out := []string{}
	for _, v := range []struct{ k, v string }{{"objective", d.Objective}, {"non_goals", d.NonGoals}, {"acceptance", d.Acceptance}, {"architecture", d.Architecture}, {"directory_outline", d.DirectoryOutline}} {
		if strings.TrimSpace(v.v) == "" {
			out = append(out, v.k)
		}
	}
	if len(d.RequirementIDs) == 0 {
		out = append(out, "requirement_ids")
	}
	if len(d.AllowedRoots) == 0 {
		out = append(out, "allowed_roots")
	}
	if d.BudgetSeconds <= 0 {
		out = append(out, "budget_seconds")
	}
	return out
}
func validateRoots(roots []string) error {
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r] || strings.ContainsAny(r, "*?[") || (r != "." && gitops.ValidatePath(r) != nil) {
			return domain.Fail("invalid_input", "goal roots must be distinct literal repository directories")
		}
		seen[r] = true
	}
	return nil
}

// Only literal prefixes followed by a terminal /** or / are supported for
// containment. A glob is never tested as though it were an actual Git path.
func literalScope(paths, roots []string) error {
	if len(paths) == 0 {
		return domain.Fail("scope", "empty scope")
	}
	for _, p := range paths {
		base := strings.TrimSuffix(strings.TrimSuffix(p, "/**"), "/")
		if strings.ContainsAny(base, "*?[") || gitops.ValidatePath(base) != nil {
			return domain.Fail("scope", "ambiguous or broad scope requires an owner decision")
		}
		ok := false
		for _, r := range roots {
			if r == "." || base == r || strings.HasPrefix(base, r+"/") {
				ok = true
			}
		}
		if !ok {
			return domain.Fail("scope", "task scope is outside authorized goal roots")
		}
	}
	return nil
}

// A literal prefix before the first wildcard bounds a protected pattern's
// possible intersection. Empty or ancestor prefixes conservatively overlap.
func protectedOverlap(paths, protected []string) bool {
	for _, p := range paths {
		b := strings.TrimSuffix(strings.TrimSuffix(p, "/**"), "/")
		for _, q := range protected {
			i := strings.IndexAny(q, "*?[")
			prefix := q
			if i >= 0 {
				prefix = q[:i]
				if j := strings.LastIndex(prefix, "/"); j >= 0 {
					prefix = prefix[:j]
				} else {
					prefix = ""
				}
			}
			prefix = strings.TrimSuffix(prefix, "/")
			if prefix == "" || b == prefix || strings.HasPrefix(b, prefix+"/") || strings.HasPrefix(prefix, b+"/") {
				return true
			}
		}
	}
	return false
}
func goalGate(tx *store.Tx, t *domain.Task, bind bool) error {
	if err := goalAuthority(tx, t, bind); err != nil {
		return err
	}
	var d domain.PlanningDocument
	err := tx.Get("planning_document", "current", &d)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.UsedSeconds+d.ReservedSeconds >= d.BudgetSeconds {
		return domain.Fail("budget", "goal budget exhausted")
	}
	return nil
}

// Existing grants retain authority while their own reservation fills the goal.
// Admission of new work additionally requires availability through goalGate.
func goalAuthority(tx *store.Tx, t *domain.Task, bind bool) error {
	if e := requirementTaskGate(tx, *t); e != nil {
		return e
	}
	if e := baselineGate(tx, t, bind); e != nil {
		return e
	}
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if d.Status != "admitted" || d.ApprovedVersion != d.Version || d.ApprovedAuthorizationVersion != d.AuthorizationVersion || d.AdmittedDocumentVersion != d.Version || d.AdmittedAuthorizationVersion != d.AuthorizationVersion || len(documentMissing(d)) != 0 {
		return domain.Fail("planning_not_ready", "goal requires authorization and structured admission")
	}
	if e = literalScope(t.AllowedPaths, d.AllowedRoots); e != nil {
		return e
	}
	if len(t.RequirementIDs) == 0 {
		return domain.Fail("planning_not_ready", "task requires goal requirements")
	}
	for _, id := range t.RequirementIDs {
		if !goalAllowsRequirement(tx, d, id) {
			return domain.Fail("scope", "task requirement outside goal")
		}
	}
	if t.Tool != "codex" && !(t.Tool == "program" && t.Verification != nil) {
		return domain.Fail("invalid_input", "Phase B development requires native codex")
	}
	if bind {
		t.GoalVersion = d.Version
		t.GoalAuthorizationVersion = d.AuthorizationVersion
	} else if t.GoalVersion != d.Version || t.GoalAuthorizationVersion != d.AuthorizationVersion {
		return domain.Fail("authorization_changed", "task belongs to an older goal")
	}
	return nil
}

func grantedGoalAuthority(tx *store.Tx, t *domain.Task, r domain.Run) bool {
	return r.BaselineVersion == t.BaselineVersion && r.BaselineDigest == t.BaselineDigest && r.GoalVersion == t.GoalVersion && r.GoalAuthorizationVersion == t.GoalAuthorizationVersion && goalAuthority(tx, t, false) == nil
}
func (s *Service) SavePlanning(ctx context.Context, a Actor, key string, req domain.PlanningSaveRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if e := validateRoots(req.Document.AllowedRoots); e != nil {
		return nil, e
	}
	if req.Document.BudgetSeconds <= 0 || req.Document.BudgetSeconds > domain.MaxTaskBudgetSeconds {
		return nil, domain.Fail("invalid_input", "finite goal budget required")
	}
	return s.mutate(ctx, a, key, "planning.save", req, func(tx *store.Tx) (any, error) {
		_, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		var old domain.PlanningDocument
		e = tx.Get("planning_document", "current", &old)
		if e != nil && !errors.Is(e, domain.ErrNotFound) {
			return nil, e
		}
		if e = version(old.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		for _, id := range req.Document.RequirementIDs {
			var r domain.Requirement
			if e = tx.Get("requirement", id, &r); e != nil {
				return nil, domain.Fail("invalid_input", "unknown requirement")
			}
			if !domain.RequirementActive(r) {
				return nil, domain.Fail("requirement_withdrawn", "新目标不能包含已撤回需求")
			}
		}
		d := req.Document
		d.Version = old.Version + 1
		d.AuthorizationVersion = old.AuthorizationVersion + 1
		d.ApprovedVersion = 0
		d.ApprovedAuthorizationVersion = 0
		d.Status = "planning"
		d.AdmittedDocumentVersion = 0
		d.AdmittedAuthorizationVersion = 0
		d.LeaderResult = nil
		d.LeaderSession = ""
		d.MissingItems = documentMissing(d)
		d.UsedSeconds = old.UsedSeconds
		d.ReservedSeconds = old.ReservedSeconds
		d.PlanningUsedSeconds = old.PlanningUsedSeconds
		d.PlanningReservedSeconds = old.PlanningReservedSeconds
		if d.BudgetSeconds < d.UsedSeconds+d.ReservedSeconds {
			return nil, domain.Fail("budget", "goal budget must cover cumulative accounting")
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return nil, e
		}
		for _, t := range tasks {
			if domain.TaskFinished(t) {
				continue
			}
			t.HoldReason = "goal_changed"
			t.Version++
			if e = requestStop(tx, t.ID); e != nil {
				return nil, e
			}
			if e = tx.Put("task", t.ID, t); e != nil {
				return nil, e
			}
		}

		if e = stopPlanning(tx); e != nil {
			return nil, e
		}
		if e = tx.Put("planning_document", "current", d); e != nil {
			return nil, e
		}
		if e = s.advancePlanningDecisions(tx, d); e != nil {
			return nil, e
		}
		return d, s.audit(tx, a, "planning.saved", "current", "")
	})
}
func (s *Service) AuthorizePlanning(ctx context.Context, a Actor, key string, req domain.VersionRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutateWithCheck(ctx, a, key, "planning.authorize", req, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		if e := baselineGate(tx, nil, false); e != nil {
			return nil, e
		}
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return nil, e
		}
		if e := version(d.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if len(documentMissing(d)) > 0 {
			return nil, domain.Fail("planning_not_ready", "mandatory goal inputs missing")
		}
		d.ApprovedVersion = d.Version
		d.ApprovedAuthorizationVersion = d.AuthorizationVersion
		if e := tx.Put("planning_document", "current", d); e != nil {
			return nil, e
		}
		return d, s.audit(tx, a, "planning.authorized", "current", "")
	})
}
func stopPlanning(tx *store.Tx) error {
	runs, e := store.List[domain.PlanningRun](tx, "planning_run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		if r.State == "queued" || r.State == "running" || r.State == "pending_result" {
			r.State = "cancel_requested"
			if r.GrantSeconds == 0 || r.Settled {
				r.State = "stopped"
				r.TreeStopped = true
				r.Settled = true
			}
			r.Version++
			if e = tx.Put("planning_run", r.ID, r); e != nil {
				return e
			}
			if e = syncConversation(tx, r); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) SchedulePlanning(ctx context.Context, a Actor, key string, req domain.PlanningMessageRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if len(req.Text) > 64<<10 {
		return nil, domain.Fail("invalid_input", "message too large")
	}
	return s.mutateWithCheck(ctx, a, key, "planning.evaluate", req, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return nil, e
		}
		if e := version(d.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		settings, e := automation(tx)
		if e != nil {
			return nil, e
		}
		runs, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return nil, e
		}
		for _, r := range runs {
			if r.State == "queued" || r.State == "running" || r.State == "cancel_requested" || r.State == "unknown_live" {
				return nil, domain.Fail("conflict", "planning run must stop before another evaluation")
			}
		}
		if strings.TrimSpace(req.Text) != "" {
			m := domain.LeaderMessage{ID: ID("msg_"), DocumentVersion: d.Version, Role: "owner", Text: req.Text, CreatedAt: s.Now()}
			if e = tx.Put("leader_message", m.ID, m); e != nil {
				return nil, e
			}
		}
		baseline, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		r := domain.PlanningRun{BaselineVersion: baseline.ApprovedVersion, BaselineDigest: baseline.ApprovedDigest, ID: ID("plan_"), Version: 1, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion, AutomationVersion: settings.Version, Generation: 1, State: "queued", MaxRetries: settings.PlanningMaxRetries}
		if e = tx.Put("planning_run", r.ID, r); e != nil {
			return nil, e
		}
		return r, s.audit(tx, a, "planning.scheduled", r.ID, "")
	})
}
func (s *Service) CancelPlanning(ctx context.Context, a Actor, key string, req domain.VersionRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "planning.cancel", req, func(tx *store.Tx) (any, error) {
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return nil, e
		}
		if e := version(d.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if e := stopPlanning(tx); e != nil {
			return nil, e
		}
		return d, s.audit(tx, a, "planning.cancelled", "current", "")
	})
}
func decodeLeader(text string) (domain.LeaderResult, error) {
	var v domain.LeaderResult
	if len(text) > 1<<20 {
		return v, domain.Fail("invalid_input", "Leader result too large")
	}
	if e := requireLeaderFields(json.RawMessage(text), reflect.TypeOf(v)); e != nil {
		return v, e
	}
	d := json.NewDecoder(bytes.NewBufferString(text))
	d.DisallowUnknownFields()
	if e := d.Decode(&v); e != nil {
		return v, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return v, domain.Fail("invalid_input", "exactly one JSON result required")
	}
	if strings.TrimSpace(v.Reply) == "" || v.MissingItems == nil || v.Clarifications == nil || v.Tasks == nil || v.Scopes == nil || v.MajorDecisions == nil {
		return v, domain.Fail("invalid_input", "structured Leader fields required")
	}
	return v, nil
}

// Go's JSON decoder accepts null scalars and omitted nested fields. Reject
// those before decoding, deriving required fields from the domain contract.
func requireLeaderFields(raw json.RawMessage, typ reflect.Type) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || len(raw) == 0 {
		return domain.Fail("invalid_input", "non-null structured Leader field required")
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			value, ok := fields[name]
			if !ok {
				// Optional attribution added to v1. Legacy durable Leader receipts
				// omit it; no authorization or scope is derived from this field.
				if typ == reflect.TypeOf(domain.TaskProposal{}) && name == "test_run_id" {
					continue
				}
				return domain.Fail("invalid_input", "structured Leader field missing: "+name)
			}
			if err := requireLeaderFields(value, field.Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for _, item := range items {
			if err := requireLeaderFields(item, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) decision(tx *store.Tx, source, id string, ver int64, d domain.PlanningDocument, kind, evidence string, actions []string) error {
	v := domain.Decision{ID: ID("decision_"), Version: 1, SourceKind: source, SourceID: id, SourceVersion: ver, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion, Kind: kind, Evidence: evidence, Actions: actions, Status: "open"}
	if e := tx.Put("decision", v.ID, v); e != nil {
		return e
	}
	return s.audit(tx, Actor{ID: "center"}, "decision.created", v.ID, evidence)
}
func (s *Service) ResolveDecision(ctx context.Context, a Actor, key, id string, req domain.DecisionResolveRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "decision.resolve."+id, req, func(tx *store.Tx) (any, error) {
		var v domain.Decision
		if e := tx.Get("decision", id, &v); e != nil {
			return nil, e
		}
		if e := version(v.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if e := version(v.SourceVersion, req.ExpectedSourceVersion); e != nil {
			return nil, e
		}
		var d domain.PlanningDocument
		if e := tx.Get("planning_document", "current", &d); e != nil {
			return nil, e
		}
		if d.Version != v.DocumentVersion || d.AuthorizationVersion != v.AuthorizationVersion {
			return nil, domain.Fail("version_conflict", "decision source goal changed")
		}
		switch v.SourceKind {
		case "scope_request":
			var scope domain.ScopeAssessment
			if e := tx.Get("scope_request", v.SourceID, &scope); e != nil {
				return nil, e
			}
			if e := version(scope.Version, v.SourceVersion); e != nil {
				return nil, e
			}
			if v.Kind == "scope_wait_stop" {
				return nil, domain.Fail("conflict", "actual stop proof must use the original Run confirm-stopped interface; assessment resumes automatically after confirmation")
			}
		case "planning_run":
			var r domain.PlanningRun
			if e := tx.Get("planning_run", v.SourceID, &r); e != nil {
				return nil, e
			}
			if e := version(r.Version, v.SourceVersion); e != nil {
				return nil, e
			}
		case "task":
			var t domain.Task
			if e := tx.Get("task", v.SourceID, &t); e != nil {
				return nil, e
			}
			if e := version(t.Version, v.SourceVersion); e != nil {
				return nil, e
			}
		}
		ok := false
		for _, action := range v.Actions {
			if action == req.Action {
				ok = true
			}
		}
		if !ok || strings.TrimSpace(req.Evidence) == "" || v.Status != "open" {
			return nil, domain.Fail("invalid_input", "an offered action and evidence are required")
		}
		// Resolution records an owner decision; changing architecture or authority
		// still requires saving and authorizing the exact new document.
		if req.Action == "confirm_stopped" && v.Kind == "unknown_live" && v.SourceKind == "planning_run" {
			var r domain.PlanningRun
			if e := tx.Get("planning_run", v.SourceID, &r); e != nil {
				return nil, e
			}
			r.TreeStopped = true
			r.State = "stopped"
			r.Version++
			r.Detail = "owner stop confirmation: " + req.Evidence
			if e := tx.Put("planning_run", r.ID, r); e != nil {
				return nil, e
			}
		}
		v.Status = "resolved"
		v.Resolution = req.Action + ": " + req.Evidence
		v.Version++
		if e := tx.Put("decision", id, v); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "decision.resolved", id, v.Resolution)
	})
}
func (s *Service) ConfigureNode(ctx context.Context, a Actor, key, id string, req domain.NodeConfigureRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if strings.TrimSpace(req.Name) == "" || req.Capacity < 1 || req.Capacity > 2 || len(req.Tools) > 64 {
		return nil, domain.Fail("invalid_input", "name and capacity 1..2 required")
	}
	seen := map[string]bool{}
	for _, v := range req.Tools {
		if e := domain.ValidateToolID(v.Tool); e != nil {
			return nil, e
		}
		if seen[v.Tool] {
			return nil, domain.Fail("invalid_input", "duplicate tool")
		}
		seen[v.Tool] = true
	}
	return s.mutate(ctx, a, key, "node.configure."+id, req, func(tx *store.Tx) (any, error) {
		var n domain.Node
		if e := tx.Get("node", id, &n); e != nil {
			return nil, e
		}
		if e := version(n.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		for _, preference := range req.Tools {
			// Retaining an unchanged preference while draining is safe; Claim
			// independently checks the current report before allocating a Run.
			unchanged := false
			for _, old := range n.Tools {
				if old == preference {
					unchanged = true
					break
				}
			}
			if !unchanged {
				if _, _, err := configuredExecution(n.Capabilities, preference); err != nil {
					return nil, err
				}
			}
		}
		n.Name = req.Name
		n.Capacity = req.Capacity
		n.Draining = req.Draining
		n.DisabledNewWork = req.DisabledNewWork
		n.Tools = req.Tools
		n.SchedulingConfigured = true
		n.Version++
		if e := tx.Put("node", id, n); e != nil {
			return nil, e
		}
		return n, s.audit(tx, a, "node.configured", id, "")
	})
}
func nodeAllows(n domain.Node, tool, kind string) bool {
	if n.Draining || n.DisabledNewWork || n.Revoked {
		return false
	}
	if !n.SchedulingConfigured {
		return true
	}
	for _, p := range n.Tools {
		if p.Tool == tool && p.Enabled {
			return kind == "implement" && p.Implement || kind == "review" && p.Review
		}
	}
	return false
}

// Persist actionable terminal task problems after bounded Phase A recovery.
// Scheduling waits and ordinary repairable failures do not become decisions.
func (s *Service) taskDecision(tx *store.Tx, t domain.Task, evidence string) error {
	if t.HoldReason != "human_decision" && t.HoldReason != "budget" {
		return nil
	}
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	all, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, v := range all {
		if v.SourceKind == "task" && v.SourceID == t.ID && v.Status == "open" && v.DocumentVersion == d.Version {
			if v.SourceVersion != t.Version {
				v.SourceVersion = t.Version
				v.Version++
				if evidence != "" {
					v.Evidence = evidence
				}
				return tx.Put("decision", v.ID, v)
			}
			return nil
		}
	}
	if evidence == "" {
		evidence = t.HoldReason
	}
	return s.decision(tx, "task", t.ID, t.Version, d, t.HoldReason, evidence, []string{"dismiss", "edit_task", "edit_budget"})
}

func (s *Service) advancePlanningDecisions(tx *store.Tx, d domain.PlanningDocument) error {
	all, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, v := range all {
		if v.Status != "open" || v.DocumentVersion == d.Version {
			continue
		}
		// Stop confirmation remains an operational need across goal edits. Rebind
		// its owner action to the current goal, preserving exact Run source fencing.
		unknown := false
		if v.Kind == "unknown_live" && v.SourceKind == "planning_run" {
			var r domain.PlanningRun
			if e = tx.Get("planning_run", v.SourceID, &r); e != nil {
				return e
			}
			unknown = r.State == "unknown_live" && !r.TreeStopped
		}
		v.Version++
		if unknown {
			v.DocumentVersion = d.Version
			v.AuthorizationVersion = d.AuthorizationVersion
		} else {
			v.Status = "superseded"
		}
		if e = tx.Put("decision", v.ID, v); e != nil {
			return e
		}
		if e = s.audit(tx, Actor{ID: "center"}, "decision.goal_changed", v.ID, v.Status); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) goalBudgetDecision(tx *store.Tx) error {
	var d domain.PlanningDocument
	e := tx.Get("planning_document", "current", &d)
	if errors.Is(e, domain.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if d.UsedSeconds < d.BudgetSeconds {
		return nil
	}
	all, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, v := range all {
		if v.SourceKind == "planning_document" && v.DocumentVersion == d.Version && v.Kind == "budget" && v.Status == "open" {
			return nil
		}
	}
	return s.decision(tx, "planning_document", "current", d.Version, d, "budget", "cumulative goal budget exhausted", []string{"edit_budget", "edit_goal", "dismiss"})
}
