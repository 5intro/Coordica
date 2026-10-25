package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/jsoninput"
	"coordica/internal/store"
)

const requirementPurpose = "requirement_change"

type requirementAdmission struct {
	ChangeID                 string `json:"change_id"`
	GoalVersion              int64  `json:"goal_version"`
	GoalAuthorizationVersion int64  `json:"goal_authorization_version"`
}

func admissionID(id string, d domain.PlanningDocument) string {
	return fmt.Sprintf("%s_g%d_a%d", id, d.Version, d.AuthorizationVersion)
}

func goalAllowsRequirement(tx *store.Tx, d domain.PlanningDocument, id string) bool {
	for _, allowed := range d.RequirementIDs {
		if id == allowed {
			return true
		}
	}
	var admission requirementAdmission
	var change domain.RequirementChange
	return tx.Get("requirement_admission", admissionID(id, d), &admission) == nil && admission.GoalVersion == d.Version && admission.GoalAuthorizationVersion == d.AuthorizationVersion && tx.Get("requirement_change", admission.ChangeID, &change) == nil && change.State == "applied" && change.Kind == "add" && change.RequirementID == id
}

// Preserve the authorized document, while resolving later admissions through
// the same gate used by task creation and execution. Proposals remain excluded.
func authorizedGoalRequirements(tx *store.Tx, d domain.PlanningDocument) ([]domain.Requirement, error) {
	requirements, e := store.List[domain.Requirement](tx, "requirement")
	if e != nil {
		return nil, e
	}
	byID := map[string]domain.Requirement{}
	for _, r := range requirements {
		byID[r.ID] = r
	}
	seen := map[string]bool{}
	out := []domain.Requirement{}
	for _, id := range d.RequirementIDs {
		r, exists := byID[id]
		if !exists {
			return nil, domain.ErrNotFound
		}
		if !seen[id] {
			out, seen[id] = append(out, r), true
		}
	}
	for _, r := range requirements {
		if !seen[r.ID] && goalAllowsRequirement(tx, d, r.ID) {
			out, seen[r.ID] = append(out, r), true
		}
	}
	return out, nil
}

func (s *Service) queueRequirementAddition(tx *store.Tx, r domain.Requirement) error {
	d, e := goalDocument(tx)
	if e != nil || d.Status != "admitted" {
		return e
	}
	change := domain.RequirementChange{ID: requirementRevisionID(r.ID, 1), Version: 1, RequirementID: r.ID, ContentRevision: 1, Kind: "add", State: "queued", Reason: "新增需求，判断与既有工作的关系", GoalVersion: d.Version, GoalAuthorizationVersion: d.AuthorizationVersion, Impacts: []domain.RequirementImpact{}, TaskIDs: []string{}, CreatedAt: s.Now()}
	return tx.Put("requirement_change", change.ID, change)
}

func (w *PlanningWorker) queueRequirementChanges(ctx context.Context) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		plans, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return e
		}
		for _, r := range plans {
			if r.State == "queued" || r.State == "running" || r.State == "pending_result" || r.State == "unknown_live" || r.State == "cancel_requested" {
				return nil
			}
		}
		changes, e := store.List[domain.RequirementChange](tx, "requirement_change")
		if e != nil {
			return e
		}
		if len(changes) == 0 {
			return nil
		}
		if e = s.retireRequirementDecisions(tx); e != nil {
			return e
		}
		d, e := goalDocument(tx)
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
		for _, change := range changes {
			if change.State != "queued" && change.State != "waiting_stop" {
				continue
			}
			var req domain.Requirement
			if e = tx.Get("requirement", change.RequirementID, &req); e != nil {
				return e
			}
			if domain.RequirementRevisionNumber(req) != change.ContentRevision {
				change.State, change.Detail = "superseded", "已被后续需求版本取代"
				change.Version++
				if e = tx.Put("requirement_change", change.ID, change); e != nil {
					return e
				}
				continue
			}
			quiet := true
			for _, impact := range change.Impacts {
				if requireQuiescence(tx, impact.Task.ID) != nil {
					quiet = false
				}
			}
			if !quiet {
				continue
			}
			if change.State == "waiting_stop" {
				change.State = "queued"
				change.Version++
				if e = tx.Put("requirement_change", change.ID, change); e != nil {
					return e
				}
			}
			if p.Paused || !settings.Enabled || d.Status != "admitted" || baselineGate(tx, nil, false) != nil {
				continue
			}
			if change.GoalVersion != d.Version || change.GoalAuthorizationVersion != d.AuthorizationVersion {
				change.State, change.Detail = "blocked", "目标版本已变化，需要按新目标重新评估变更"
				change.Version++
				if e = tx.Put("requirement_change", change.ID, change); e != nil {
					return e
				}
				continue
			}
			if change.Kind != "add" && !goalAllowsRequirement(tx, d, change.RequirementID) {
				change.State, change.Detail = "blocked", "当前目标未包含该需求，需要 Owner 核对目标"
				change.Version++
				if e = tx.Put("requirement_change", change.ID, change); e != nil {
					return e
				}
				continue
			}
			baseline, e := initialization(tx)
			if e != nil {
				return e
			}
			r := domain.PlanningRun{ID: ID("plan_"), Version: 1, Purpose: requirementPurpose, RequirementChangeID: change.ID, BaselineVersion: baseline.ApprovedVersion, BaselineDigest: baseline.ApprovedDigest, DocumentVersion: d.Version, AuthorizationVersion: d.AuthorizationVersion, AutomationVersion: settings.Version, Generation: 1, State: "queued", MaxRetries: settings.PlanningMaxRetries}
			change.State, change.AutomationVersion = "assessing", settings.Version
			change.Version++
			if e = tx.Put("requirement_change", change.ID, change); e != nil {
				return e
			}
			if e = tx.Put("planning_run", r.ID, r); e != nil {
				return e
			}
			return s.audit(tx, Actor{ID: "center"}, "requirement.assessment_queued", change.ID, r.ID)
		}
		return nil
	})
}

// The impact set is fixed, but its task prose belongs in deferred material.
// Preserve every impact flag and exact task contract/accounting in the index.
type requirementImpactIndex struct {
	Task            planningTaskIndex `json:"task"`
	Shared          bool              `json:"shared"`
	NeedsStop       bool              `json:"needs_stop"`
	Integrated      bool              `json:"integrated"`
	PublicationBusy bool              `json:"publication_busy"`
	Direct          bool              `json:"direct"`
}
type requirementChangeContext struct {
	domain.RequirementChange
	Impacts []requirementImpactIndex `json:"impacts"`
}

func indexRequirementChange(change domain.RequirementChange) requirementChangeContext {
	out := requirementChangeContext{RequirementChange: change, Impacts: []requirementImpactIndex{}}
	out.RequirementChange.Impacts = nil
	for _, impact := range change.Impacts {
		out.Impacts = append(out.Impacts, requirementImpactIndex{Task: indexPlanningTasks([]domain.Task{impact.Task})[0], Shared: impact.Shared, NeedsStop: impact.NeedsStop, Integrated: impact.Integrated, PublicationBusy: impact.PublicationBusy, Direct: impact.Direct})
	}
	return out
}

func (w *PlanningWorker) requirementPrompt(ctx context.Context, r domain.PlanningRun, dir string) (string, error) {
	var input struct {
		Change       requirementChangeContext  `json:"change"`
		Document     domain.PlanningDocument   `json:"document"`
		Requirement  domain.Requirement        `json:"requirement"`
		Requirements []domain.Requirement      `json:"requirements"`
		Tasks        []planningTaskIndex       `json:"existing_tasks"`
		Baseline     leaderBaselineContext     `json:"shared_baseline"`
		Materials    leaderMaterials           `json:"materials"`
		Protected    []string                  `json:"protected_paths"`
		Settings     domain.AutomationSettings `json:"settings"`
	}
	var tasks []domain.Task
	var change domain.RequirementChange
	e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		input.Baseline, err = currentLeaderBaseline(tx, r)
		if err != nil {
			return err
		}
		if !requirementPlanCurrent(tx, r) {
			return domain.Fail("stale_run", "需求版本已变化")
		}
		if e := tx.Get("requirement_change", r.RequirementChangeID, &change); e != nil {
			return e
		}
		if e := tx.Get("requirement", change.RequirementID, &input.Requirement); e != nil {
			return e
		}
		var e error
		input.Document, e = goalDocument(tx)
		if e != nil {
			return e
		}
		tasks, e = store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		input.Requirements, e = store.List[domain.Requirement](tx, "requirement")
		if e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		input.Protected = p.ProtectedPaths
		input.Settings, e = automation(tx)
		return e
	})
	if e != nil {
		return "", e
	}
	input.Document.LeaderResult, input.Document.LeaderSession = nil, ""
	input.Change = indexRequirementChange(change)
	input.Tasks = indexPlanningTasks(tasks)
	input.Materials, e = writeLeaderMaterials(dir, r, tasks, input.Requirements, nil, nil, change.Impacts)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(input)
	if e != nil || len(raw) > 256<<10 {
		return "", domain.Fail("context_limit", "需求影响上下文超过单次分析上限")
	}
	prompt := requirementPlanPrompt(raw)
	if len(prompt) > leaderPromptLimit {
		return "", domain.Fail("context_limit", "完整需求影响约束超过输入上限，未裁剪影响集或权限")
	}
	return prompt, nil
}

func decodeRequirementPlan(text string) (domain.RequirementPlan, error) {
	var result domain.RequirementPlan
	if len(text) > 1<<20 {
		return result, domain.Fail("invalid_result", "需求计划输出超过上限")
	}
	if e := requireLeaderFields(json.RawMessage(text), reflect.TypeOf(result)); e != nil {
		return result, e
	}
	if e := jsoninput.Decode([]byte(text), &result, 1<<20); e != nil {
		return result, e
	}
	if !domain.ChatText(result.Reply, 16<<10, true) || result.Tasks == nil || result.Updates == nil || result.MajorDecisions == nil || len(result.Tasks) > 100 || len(result.MajorDecisions) > 20 {
		return result, domain.Fail("invalid_result", "需求变更计划字段无效")
	}
	return result, nil
}

func blockRequirementAssessment(tx *store.Tx, r domain.PlanningRun, detail string) error {
	if r.RequirementChangeID == "" {
		return nil
	}
	var change domain.RequirementChange
	if e := tx.Get("requirement_change", r.RequirementChangeID, &change); e != nil {
		return e
	}
	if change.State != "assessing" {
		return nil
	}
	change.State, change.Detail = "blocked", detail
	var req domain.Requirement
	if tx.Get("requirement", change.RequirementID, &req) == nil && domain.RequirementRevisionNumber(req) != change.ContentRevision {
		change.State, change.Detail = "superseded", "已被后续需求版本取代"
	}
	change.Version++
	return tx.Put("requirement_change", change.ID, change)
}

// The same application admission gate recognizes a requirement added by an
// explicitly versioned, policy-checked change; raw task input cannot mint one.
func requirementChangeBindings(tx *store.Tx, ids []string, change domain.RequirementChange) (map[string]int64, error) {
	bindings, e := requirementPlanBindings(tx, ids)
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		var req domain.Requirement
		if e = tx.Get("requirement", id, &req); e != nil {
			return nil, e
		}
		if !domain.RequirementActive(req) && !(change.Kind == "withdraw" && change.RequirementID == id) {
			return nil, domain.Fail("requirement_withdrawn", "不能关联其他已撤销需求")
		}
	}
	return bindings, nil
}

func requirementDecision(result domain.RequirementPlan) bool {
	return !result.Ready || len(result.MajorDecisions) != 0
}

// validateRequirementWork shares the existing task, scope and test-link rules;
// no transport or model result supplies execution authority by itself.
func validateRequirementWork(tx *store.Tx, t domain.Task, d domain.PlanningDocument, p domain.Project, change domain.RequirementChange, existingRequirementIDs []string) error {
	if e := domain.ValidateTask(t); e != nil {
		return e
	}
	if e := literalScope(t.AllowedPaths, d.AllowedRoots); e != nil {
		return e
	}
	if protectedOverlap(t.AllowedPaths, p.ProtectedPaths) {
		return domain.Fail("protected_scope", "需求变更涉及受保护路径，需要Owner处理")
	}
	if e := validateTestRunLink(tx, t.TestRunID); e != nil {
		return e
	}
	if len(t.RequirementIDs) == 0 {
		return domain.Fail("invalid_plan", "任务必须关联需求")
	}
	for _, id := range t.RequirementIDs {
		if id != change.RequirementID && (!containsPaths(existingRequirementIDs, []string{id}) || !goalAllowsRequirement(tx, d, id)) {
			return domain.Fail("scope", "变更不能引入无关需求")
		}
	}
	return nil
}

func (s *Service) applyRequirementPlan(tx *store.Tx, r *domain.PlanningRun, d domain.PlanningDocument, result domain.RequirementPlan) error {
	if !requirementPlanCurrent(tx, *r) {
		return domain.Fail("stale_run", "需求变更已被替代")
	}
	var change domain.RequirementChange
	if e := tx.Get("requirement_change", r.RequirementChangeID, &change); e != nil {
		return e
	}
	if len(result.Tasks) > 100 || len(result.Updates) > max(100, len(change.Impacts)) {
		return domain.Fail("invalid_plan", "需求变更计划超过任务或固定影响集上限")
	}
	if change.Kind != "add" && !goalAllowsRequirement(tx, d, change.RequirementID) {
		return domain.Fail("scope", "当前目标未包含该需求")
	}
	settings, e := automation(tx)
	if e != nil {
		return e
	}
	p, e := getProject(tx)
	if e != nil {
		return e
	}
	if p.Paused || !settings.Enabled || settings.Version != r.AutomationVersion || change.AutomationVersion != settings.Version || d.Status != "admitted" {
		return domain.Fail("authorization_changed", "需求规划授权或自动化设置已变化")
	}
	if requirementDecision(result) {
		if len(result.Tasks)+len(result.Updates) > 0 {
			return domain.Fail("invalid_plan", "重大决定未解决时不能同时更改开发任务")
		}
		if len(result.MajorDecisions) == 0 {
			result.MajorDecisions = []domain.MajorDecisionProposal{{Kind: "requirement", Evidence: result.Reply, Actions: []string{"edit_goal", "evaluate", "dismiss"}}}
		}
		for _, decision := range result.MajorDecisions {
			if strings.TrimSpace(decision.Kind) == "" || len(decision.Actions) == 0 || !domain.ChatText(decision.Evidence, 16<<10, true) {
				return domain.Fail("invalid_plan", "重大决定需要明确证据")
			}
			if e = s.decision(tx, "planning_run", r.ID, r.Version, d, decision.Kind, decision.Evidence, decision.Actions); e != nil {
				return e
			}
		}
		change.State, change.Detail = "blocked", result.Reply
		change.Version++
		return tx.Put("requirement_change", change.ID, change)
	}
	all, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return e
	}
	proposed := map[string]domain.Task{}
	keys := map[string]string{}
	existingIDs := map[string]bool{}
	for _, task := range all {
		existingIDs[task.ID] = true
	}
	for _, next := range result.Tasks {
		q := next.Proposal
		if !domain.ChatIdentifier.MatchString(q.Key) || keys[q.Key] != "" || existingIDs[q.Key] {
			return domain.Fail("invalid_plan", "新任务局部标识不能重复或与已有任务冲突")
		}
		keys[q.Key] = ID("task_")
	}
	resolveDeps := func(values []string) ([]string, error) {
		out := []string{}
		for _, id := range values {
			if mapped := keys[id]; mapped != "" {
				out = append(out, mapped)
				continue
			}
			var existing domain.Task
			if e := tx.Get("task", id, &existing); e != nil {
				return nil, domain.Fail("invalid_plan", "依赖任务不存在")
			}
			if existing.State == "cancelled" {
				return nil, domain.Fail("invalid_plan", "不能依赖已取消任务")
			}
			out = append(out, id)
		}
		return out, nil
	}
	for _, update := range result.Updates {
		var old domain.Task
		if e = tx.Get("task", update.TaskID, &old); e != nil {
			return e
		}
		if proposed[old.ID].ID != "" || old.Version != update.ExpectedVersion || update.Proposal.Key != old.ID || domain.TaskFinished(old) || old.Verification != nil || requireQuiescence(tx, old.ID) != nil {
			return domain.Fail("conflict", "只能修改当前版本且已确认停止的未完成任务")
		}
		eligible := change.Kind == "add" && old.State == "ready" && old.HoldReason == "" && old.Generation == 0
		for _, impact := range change.Impacts {
			eligible = eligible || impact.Task.ID == old.ID
		}
		if change.Kind == "add" && !containsPaths(update.Proposal.RequirementIDs, []string{change.RequirementID}) {
			return domain.Fail("scope", "合并任务必须关联新增需求")
		}
		if !eligible || change.Kind == "add" && (old.GoalVersion != d.Version || old.GoalAuthorizationVersion != d.AuthorizationVersion) {
			return domain.Fail("scope", "任务不属于本次变更范围")
		}
		q := update.Proposal
		if q.BudgetSeconds != old.BudgetSeconds || q.MaxRunSeconds != old.MaxRunSeconds || q.MaxRetries != old.MaxRetries || q.MaxRepairs != old.MaxRepairs || !sameStrings(q.HotResources, old.HotResources) || q.TestRunID != old.TestRunID {
			return domain.Fail("budget", "需求修订不能重置或扩大任务执行限制")
		}
		t := old
		t.Title, t.Description, t.Acceptance, t.RequirementIDs, t.AllowedPaths = q.Title, q.Objective, q.Acceptance, q.RequirementIDs, q.Paths
		for _, id := range old.RequirementIDs {
			var req domain.Requirement
			if e = tx.Get("requirement", id, &req); e != nil {
				return e
			}
			if domain.RequirementActive(req) && !containsPaths(t.RequirementIDs, []string{id}) {
				return domain.Fail("invalid_plan", "共享任务不能丢失其他有效需求")
			}
		}
		t.Dependencies, e = resolveDeps(q.Dependencies)
		if e != nil {
			return e
		}
		t.RequirementRevisions, e = requirementBindings(tx, t.RequirementIDs)
		if e != nil {
			return e
		}
		if e = validateRequirementWork(tx, t, d, p, change, old.RequirementIDs); e != nil {
			return e
		}
		t.Version++
		t.AuthorizationVersion++
		if e = invalidateRequirementCandidate(tx, &t); e != nil {
			return e
		}
		t.CurrentRunID, t.CurrentCandidateID, t.HoldReason, t.State = "", "", "", "ready"
		t.HoldReason = old.HoldReason
		if t.HoldReason == "requirement_changed" {
			t.HoldReason = old.RequirementHoldReason
		}
		t.RequirementHoldReason = ""
		t.GoalVersion, t.GoalAuthorizationVersion = d.Version, d.AuthorizationVersion
		if e = baselineGate(tx, &t, true); e != nil {
			return e
		}
		if pending, err := pendingRequirementChange(tx, t.ID, change.ID); err != nil {
			return err
		} else if pending {
			t.RequirementHoldReason = t.HoldReason
			t.HoldReason = "requirement_changed"
		}
		t.ApprovedVersion = t.Version
		if old.ApprovedVersion == 0 || old.State == "draft" {
			t.State, t.ApprovedVersion = "draft", 0
		}
		proposed[t.ID] = t
	}
	for _, impact := range change.Impacts {
		var current domain.Task
		if e = tx.Get("task", impact.Task.ID, &current); e != nil {
			return e
		}
		if !domain.TaskFinished(current) && proposed[current.ID].ID == "" {
			return domain.Fail("invalid_plan", "受影响的未完成任务必须明确重新规划")
		}
	}
	for _, next := range result.Tasks {
		q := next.Proposal
		relatedTasks := []domain.Task{}
		existingRequirementIDs := []string{}
		compensationAnchor := false
		for _, relatedID := range next.RelatedTaskIDs {
			var related domain.Task
			if e = tx.Get("task", relatedID, &related); e != nil {
				return e
			}
			if change.Kind == "add" && (related.GoalVersion != d.Version || related.GoalAuthorizationVersion != d.AuthorizationVersion) || related.State == "cancelled" {
				return domain.Fail("scope", "后续任务关联对象无效")
			}
			if change.Kind != "add" {
				allowed := false
				for _, impact := range change.Impacts {
					if impact.Task.ID == relatedID {
						allowed = true
						compensationAnchor = compensationAnchor || impact.Direct && impact.Integrated && impact.Task.Verification == nil
						existingRequirementIDs = append(existingRequirementIDs, impact.Task.RequirementIDs...)
					}
				}
				if !allowed {
					return domain.Fail("scope", "补偿只能关联受影响任务")
				}
			}
			if change.Kind == "add" {
				existingRequirementIDs = append(existingRequirementIDs, related.RequirementIDs...)
			}
			relatedTasks = append(relatedTasks, related)
		}
		if change.Kind == "withdraw" && !compensationAnchor {
			return domain.Fail("invalid_plan", "撤销后的新任务必须补偿已集成的直接代码影响")
		}
		t := proposalTask(q, settings.DevelopmentTool)
		t.ID, t.Version, t.AuthorizationVersion = keys[q.Key], 1, 1
		t.RequirementChangeID = change.ID
		t.RequirementRevisions, e = requirementChangeBindings(tx, t.RequirementIDs, change)
		if e != nil {
			return e
		}
		if !containsPaths(t.RequirementIDs, []string{change.RequirementID}) {
			return domain.Fail("scope", "后续任务必须关联本次变更需求")
		}
		if e = validateRequirementWork(tx, t, d, p, change, existingRequirementIDs); e != nil {
			return e
		}
		if e = baselineGate(tx, &t, true); e != nil {
			return e
		}
		t.GoalVersion, t.GoalAuthorizationVersion = d.Version, d.AuthorizationVersion
		t.State, t.ApprovedVersion, t.CreatedAt = "ready", 1, s.Now()
		t.Dependencies, e = resolveDeps(q.Dependencies)
		if e != nil {
			return e
		}
		for _, related := range relatedTasks {
			if !domain.TaskFinished(related) && !containsPaths(t.Dependencies, []string{related.ID}) {
				return domain.Fail("invalid_plan", "后续工作必须等待关联的在途任务")
			}
		}
		proposed[t.ID] = t
		r.TaskMapping = append(r.TaskMapping, domain.ProposalTaskMapping{Key: change.ID + "_" + q.Key, TaskID: t.ID})
	}
	for _, impact := range change.Impacts {
		if !impact.Integrated || impact.Task.Verification != nil {
			continue
		}
		covered := false
		for _, next := range result.Tasks {
			covered = covered || containsPaths(next.RelatedTaskIDs, []string{impact.Task.ID})
		}
		if !covered {
			return domain.Fail("invalid_plan", "已集成内容的变更需要明确的补偿任务")
		}
	}
	if change.Kind == "add" && len(proposed) == 0 {
		return domain.Fail("invalid_plan", "新增需求需要关联开发任务或提出决定")
	}
	// Validate the whole resulting graph and allocation before writing any task.
	graph := map[string]domain.Task{}
	for _, t := range all {
		graph[t.ID] = t
	}
	for id, t := range proposed {
		graph[id] = t
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return domain.Fail("invalid_plan", "需求变更导致任务依赖循环")
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range graph[id].Dependencies {
			if e := visit(dep); e != nil {
				return e
			}
		}
		visiting[id], visited[id] = false, true
		return nil
	}
	allocated := int64(0)
	for id, t := range graph {
		if e = visit(id); e != nil {
			return e
		}
		if t.GoalVersion == d.Version && !domain.TaskFinished(t) {
			allocated += t.BudgetSeconds - t.UsedSeconds - t.ReservedSeconds
		}
	}
	if allocated > d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds {
		return domain.Fail("budget", "变更计划超过目标剩余额度")
	}
	for id, t := range proposed {
		if e = tx.Put("task", id, t); e != nil {
			return e
		}
		change.TaskIDs = append(change.TaskIDs, id)
		r.TaskIDs = append(r.TaskIDs, id)
	}
	if change.Kind == "add" {
		admission := requirementAdmission{ChangeID: change.ID, GoalVersion: d.Version, GoalAuthorizationVersion: d.AuthorizationVersion}
		if e = tx.Put("requirement_admission", admissionID(change.RequirementID, d), admission); e != nil {
			return e
		}
	}
	change.State, change.Detail = "applied", result.Reply
	change.Version++
	if e = tx.Put("requirement_change", change.ID, change); e != nil {
		return e
	}
	if e = s.retireRequirementDecisions(tx); e != nil {
		return e
	}
	return s.audit(tx, Actor{ID: "leader"}, "requirement.plan_applied", change.ID, result.Reply)
}

// Obsolete assessments remain auditable history, without permanently showing
// an already replaced problem as a current human action. Unknown stop evidence
// must still use the original Run's confirmation path.
func (s *Service) retireRequirementDecisions(tx *store.Tx) error {
	decisions, e := store.List[domain.Decision](tx, "decision")
	if e != nil {
		return e
	}
	for _, decision := range decisions {
		if decision.Status != "open" || decision.SourceKind != "planning_run" || decision.Kind == "unknown_live" {
			continue
		}
		var plan domain.PlanningRun
		if e = tx.Get("planning_run", decision.SourceID, &plan); e != nil {
			return e
		}
		if plan.RequirementChangeID == "" || !plan.TreeStopped || !plan.Settled && plan.GrantSeconds > 0 {
			continue
		}
		var change domain.RequirementChange
		if e = tx.Get("requirement_change", plan.RequirementChangeID, &change); e != nil {
			return e
		}
		if change.State != "applied" && change.State != "superseded" {
			continue
		}
		decision.Status = "superseded"
		decision.Version++
		if e = tx.Put("decision", decision.ID, decision); e != nil {
			return e
		}
		if e = s.audit(tx, Actor{ID: "center"}, "requirement.decision_superseded", decision.ID, change.ID); e != nil {
			return e
		}
	}
	return nil
}

// A same-goal content change has its own fixed impact set and accounting.
// Ordinary planning must not derive replacement tasks outside that plan.
func pendingGoalRequirementChange(tx *store.Tx, d domain.PlanningDocument) (bool, error) {
	changes, e := store.List[domain.RequirementChange](tx, "requirement_change")
	if e != nil {
		return false, e
	}
	for _, change := range changes {
		if change.GoalVersion == d.Version && change.GoalAuthorizationVersion == d.AuthorizationVersion && change.State != "applied" && change.State != "superseded" && goalAllowsRequirement(tx, d, change.RequirementID) {
			var requirement domain.Requirement
			if e = tx.Get("requirement", change.RequirementID, &requirement); e != nil {
				return false, e
			}
			if domain.RequirementRevisionNumber(requirement) == change.ContentRevision {
				return true, nil
			}
		}
	}
	return false, nil
}
