package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
	"reflect"
	"strings"
)

// Validate the whole proposal before writing any task or scope. All dependency
// references are local keys, and all tasks are derived in the same transaction.
func (s *Service) derivePlan(tx *store.Tx, r *domain.PlanningRun, d *domain.PlanningDocument, result domain.LeaderResult) error {
	if r.Purpose == domain.ConsultationPurpose {
		return domain.Fail("forbidden", "advisory requests cannot derive plans")
	}
	if pending, e := pendingGoalRequirementChange(tx, *d); e != nil {
		return e
	} else if pending {
		return domain.Fail("conflict", "需求变更尚未重排，不能派生替代任务")
	}
	baseline, e := initialization(tx)
	if e != nil {
		return e
	}
	if e = baselineGate(tx, nil, false); e != nil {
		return e
	}
	if r.BaselineVersion != baseline.ApprovedVersion || r.BaselineDigest != baseline.ApprovedDigest {
		return domain.Fail("baseline_changed", "planning result belongs to an older baseline")
	}
	settings, e := automation(tx)
	if e != nil {
		return e
	}
	p, e := getProject(tx)
	if e != nil {
		return e
	}
	if r.ScopeRequestID != "" {
		return s.deriveScope(tx, r, *d, result, settings, p)
	}
	mappings := map[string]string{}
	oldPlans, e := store.List[domain.PlanningRun](tx, "planning_run")
	if e != nil {
		return e
	}
	for _, old := range oldPlans {
		if old.DocumentVersion == d.Version && old.AuthorizationVersion == d.AuthorizationVersion && old.State == "succeeded" {
			for _, m := range old.TaskMapping {
				if prior := mappings[m.Key]; prior != "" && prior != m.TaskID {
					return domain.Fail("conflict", "ambiguous prior proposal mapping")
				}
				mappings[m.Key] = m.TaskID
			}
		}
	}
	reused := map[string]bool{}
	existingTasks, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return e
	}
	ids := map[string]string{}
	proposals := map[string]domain.TaskProposal{}
	total := int64(0)
	if len(result.Tasks) > 100 || len(result.Scopes) > 100 || len(result.MajorDecisions) > 100 {
		return domain.Fail("invalid_input", "proposal batch too large")
	}
	for _, q := range result.Tasks {
		if strings.TrimSpace(q.Key) == "" || ids[q.Key] != "" {
			return domain.Fail("invalid_input", "duplicate or empty local task key")
		}
		t := proposalTask(q, settings.DevelopmentTool)
		if e = validateTestRunLink(tx, t.TestRunID); e != nil {
			return e
		}
		if e = domain.ValidateTask(t); e != nil {
			return e
		}
		if len(q.RequirementIDs) == 0 {
			return domain.Fail("invalid_input", "task must map to requirements")
		}
		for _, id := range q.RequirementIDs {
			if !goalAllowsRequirement(tx, *d, id) {
				return domain.Fail("scope", "task requirement outside goal")
			}
			var req domain.Requirement
			if e = tx.Get("requirement", id, &req); e != nil {
				return e
			}
		}
		if e = literalScope(q.Paths, d.AllowedRoots); e != nil {
			return e
		}
		if protectedOverlap(q.Paths, p.ProtectedPaths) {
			return domain.Fail("protected_scope", "automatic proposal potentially intersects a protected path")
		}
		proposals[q.Key] = q
		if id := mappings[q.Key]; id != "" {
			var old domain.Task
			if e = tx.Get("task", id, &old); e != nil {
				return e
			}
			if old.GoalVersion != d.Version || old.GoalAuthorizationVersion != d.AuthorizationVersion || !sameTaskProposal(old, t) {
				return domain.Fail("conflict", "existing proposal key cannot change task authority, work or budget")
			}
			ids[q.Key] = id
			reused[q.Key] = true
		} else {
			for _, old := range existingTasks {
				if old.GoalVersion == d.Version && old.GoalAuthorizationVersion == d.AuthorizationVersion && (sameTaskProposal(old, t) || old.Description == t.Description && old.Acceptance == t.Acceptance && sameStrings(old.RequirementIDs, t.RequirementIDs) && sameStrings(old.AllowedPaths, t.AllowedPaths)) {
					return domain.Fail("conflict", "existing work must reuse its stable proposal key")
				}
			}
			ids[q.Key] = ID("task_")
			total += q.BudgetSeconds
		}
	}
	if total > d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds {
		return domain.Fail("budget", "whole batch exceeds remaining goal budget")
	}
	// Account for already admitted unspent task allowances as well as reservations.
	existing, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return e
	}
	allocated := int64(0)
	for _, t := range existing {
		if t.GoalVersion == d.Version && !domain.TaskFinished(t) {
			allocated += t.BudgetSeconds - t.UsedSeconds - t.ReservedSeconds
		}
	}
	if total+allocated > d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds {
		return domain.Fail("budget", "batch would overallocate the goal")
	}
	visited := map[string]int{}
	var visit func(string) error
	visit = func(key string) error {
		if visited[key] == 1 {
			return domain.Fail("invalid_input", "task dependency cycle")
		}
		if visited[key] == 2 {
			return nil
		}
		q, ok := proposals[key]
		if !ok {
			return domain.Fail("invalid_input", "missing local dependency")
		}
		visited[key] = 1
		for _, dep := range q.Dependencies {
			if e := visit(dep); e != nil {
				return e
			}
		}
		visited[key] = 2
		return nil
	}
	for key := range proposals {
		if e = visit(key); e != nil {
			return e
		}
	}
	for key, q := range proposals {
		if reused[key] {
			var old domain.Task
			if e = tx.Get("task", ids[key], &old); e != nil {
				return e
			}
			deps := []string{}
			for _, dep := range q.Dependencies {
				deps = append(deps, ids[dep])
			}
			if !sameStrings(old.Dependencies, deps) {
				return domain.Fail("conflict", "existing proposal dependencies changed")
			}
		}
	}
	scopeIDs := map[string]bool{}
	for _, q := range result.Scopes {
		if scopeIDs[q.TaskID] {
			return domain.Fail("invalid_input", "duplicate task scope proposal")
		}
		scopeIDs[q.TaskID] = true
		var t domain.Task
		if e = tx.Get("task", q.TaskID, &t); e != nil {
			return e
		}
		if e = version(t.Version, q.ExpectedVersion); e != nil {
			return e
		}
		if e = goalGate(tx, &t, false); e != nil {
			return e
		}
		if t.CurrentRunID != "" || t.CurrentCandidateID != "" || domain.TaskFinished(t) {
			return domain.Fail("conflict", "scope change requires a stopped task without a candidate")
		}
		if e = requireQuiescence(tx, t.ID); e != nil {
			return e
		}
		if !containsPaths(q.Paths, t.AllowedPaths) {
			return domain.Fail("scope", "ordinary scope must preserve previous allowed paths")
		}
		if t.HoldReason != "" {
			return domain.Fail("conflict", "held tasks require their exact scoped request assessment")
		}
		if t.UsedSeconds+t.ReservedSeconds >= t.BudgetSeconds || t.Retries >= t.MaxRetries {
			return domain.Fail("budget", "task scope budget or retries exhausted")
		}
		if strings.TrimSpace(q.Reason) == "" {
			return domain.Fail("invalid_input", "scope reason required")
		}
		if e = literalScope(q.Paths, d.AllowedRoots); e != nil {
			return e
		}
		if protectedOverlap(q.Paths, p.ProtectedPaths) {
			return domain.Fail("protected_scope", "scope proposal potentially intersects protected paths")
		}
		for _, hot := range t.HotResources {
			var res domain.Resource
			e = tx.Get("resource", hot, &res)
			if e == nil && res.TaskID != t.ID {
				return domain.Fail("conflict", "resource owned by another task")
			}
			if e != nil && e != domain.ErrNotFound {
				return e
			}
		}
	}
	ready := result.Ready && len(result.MissingItems) == 0 && len(result.Clarifications) == 0 && len(result.MajorDecisions) == 0 && len(documentMissing(*d)) == 0 && d.ApprovedVersion == d.Version && d.ApprovedAuthorizationVersion == d.AuthorizationVersion
	// A conversation cannot revoke existing admitted authority. New decisions
	// remain explicit; existing task/run admissions retain their exact versions.
	if d.Status != "admitted" {
		d.Status = "planning"
		d.AdmittedDocumentVersion = 0
		d.AdmittedAuthorizationVersion = 0
	}
	if ready {
		d.Status = "admitted"
		d.AdmittedDocumentVersion = d.Version
		d.AdmittedAuthorizationVersion = d.AuthorizationVersion
	}
	auto := ready && settings.Enabled && settings.Version == r.AutomationVersion && !p.Paused
	for _, q := range result.Tasks {
		if reused[q.Key] {
			r.TaskIDs = append(r.TaskIDs, ids[q.Key])
			r.TaskMapping = append(r.TaskMapping, domain.ProposalTaskMapping{Key: q.Key, TaskID: ids[q.Key]})
			continue
		}
		t := proposalTask(q, settings.DevelopmentTool)
		t.ID = ids[q.Key]
		t.RequirementRevisions, e = requirementBindings(tx, t.RequirementIDs)
		if e != nil {
			return e
		}
		t.Version = 1
		t.AuthorizationVersion = 1
		if e := baselineGate(tx, &t, true); e != nil {
			return e
		}
		t.GoalVersion = d.Version
		t.GoalAuthorizationVersion = d.AuthorizationVersion
		t.State = "draft"
		t.CreatedAt = s.Now()
		t.Dependencies = []string{}
		for _, key := range q.Dependencies {
			t.Dependencies = append(t.Dependencies, ids[key])
		}
		if auto {
			t.State = "ready"
			t.ApprovedVersion = 1
		}
		if e = tx.Put("task", t.ID, t); e != nil {
			return e
		}
		r.TaskIDs = append(r.TaskIDs, t.ID)
		r.TaskMapping = append(r.TaskMapping, domain.ProposalTaskMapping{Key: q.Key, TaskID: t.ID})
		if e = s.audit(tx, Actor{ID: "leader"}, "task.proposed", t.ID, q.Key); e != nil {
			return e
		}
	}
	for _, q := range result.Scopes {
		if auto {
			var t domain.Task
			if e = tx.Get("task", q.TaskID, &t); e != nil {
				return e
			}
			t.AllowedPaths = q.Paths
			t.AuthorizationVersion++
			t.Version++
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
			if e = s.audit(tx, Actor{ID: "leader"}, "task.scope", t.ID, q.Reason); e != nil {
				return e
			}
		} else {
			if e = s.decision(tx, "planning_run", r.ID, r.Version, *d, "scope", q.Reason, []string{"dismiss", "edit_scope"}); e != nil {
				return e
			}
		}
	}
	for _, q := range result.MajorDecisions {
		if strings.TrimSpace(q.Kind) == "" || strings.TrimSpace(q.Evidence) == "" || len(q.Actions) == 0 {
			return domain.Fail("invalid_input", "major decision requires kind, evidence and actions")
		}
		if e = s.decision(tx, "planning_run", r.ID, r.Version, *d, q.Kind, q.Evidence, q.Actions); e != nil {
			return e
		}
	}
	return nil
}
func proposalTask(q domain.TaskProposal, tool string) domain.Task {
	return domain.Task{TestRunID: q.TestRunID, Title: q.Title, Description: q.Objective, Acceptance: q.Acceptance, RequirementIDs: q.RequirementIDs, AllowedPaths: q.Paths, HotResources: q.HotResources, Tool: tool, BudgetSeconds: q.BudgetSeconds, MaxRunSeconds: q.MaxRunSeconds, MaxRepairs: q.MaxRepairs, MaxRetries: q.MaxRetries}
}

func sameStrings(a, b []string) bool {
	return len(a) == len(b) && reflect.DeepEqual(append([]string{}, a...), append([]string{}, b...))
}
func containsPaths(paths, required []string) bool {
	for _, want := range required {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func sameTaskProposal(a, b domain.Task) bool {
	return a.TestRunID == b.TestRunID && a.Title == b.Title && a.Description == b.Description && a.Acceptance == b.Acceptance && a.Tool == b.Tool && sameStrings(a.RequirementIDs, b.RequirementIDs) && sameStrings(a.AllowedPaths, b.AllowedPaths) && sameStrings(a.HotResources, b.HotResources) && a.BudgetSeconds == b.BudgetSeconds && a.MaxRunSeconds == b.MaxRunSeconds && a.MaxRepairs == b.MaxRepairs && a.MaxRetries == b.MaxRetries
}
