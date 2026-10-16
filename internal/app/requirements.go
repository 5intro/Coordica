package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func requirementRevisionID(id string, revision int64) string {
	return fmt.Sprintf("%s_r%012d", id, revision)
}

func (s *Service) saveRequirementRevision(tx *store.Tx, a Actor, r domain.Requirement, reason string) error {
	id := requirementRevisionID(r.ID, domain.RequirementRevisionNumber(r))
	var prior domain.RequirementRevision
	if e := tx.Get("requirement_revision", id, &prior); e == nil {
		return nil // Existing content history is immutable, including old sources.
	} else if !errors.Is(e, domain.ErrNotFound) {
		return e
	}
	return tx.Put("requirement_revision", id, domain.RequirementRevision{Requirement: r, ActorID: a.ID, Reason: reason, CreatedAt: s.Now()})
}

func requirementBindings(tx *store.Tx, ids []string) (map[string]int64, error) {
	bindings := map[string]int64{}
	for _, id := range ids {
		var r domain.Requirement
		if e := tx.Get("requirement", id, &r); e != nil {
			return nil, e
		}
		if !domain.RequirementActive(r) {
			return nil, domain.Fail("requirement_withdrawn", "不能为已撤销需求创建开发工作")
		}
		bindings[id] = domain.RequirementRevisionNumber(r)
	}
	return bindings, nil
}

// Every execution gate uses this same content fence. Legacy grants correspond
// to revision one; a metadata/link Version increment does not stale work.
func requirementTaskGate(tx *store.Tx, t domain.Task) error {
	if pending, e := pendingRequirementChange(tx, t.ID, ""); e != nil {
		return e
	} else if pending {
		return domain.Fail("requirement_changed", "任务仍有待处理的需求变更")
	}
	for _, id := range t.RequirementIDs {
		var r domain.Requirement
		if e := tx.Get("requirement", id, &r); e != nil {
			return e
		}
		var pending domain.RequirementChange
		if e := tx.Get("requirement_change", requirementRevisionID(id, domain.RequirementRevisionNumber(r)), &pending); e == nil {
			if pending.State != "applied" && pending.State != "superseded" {
				return domain.Fail("requirement_changed", "需求变更尚未完成规划")
			}
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		pinned := t.RequirementRevisions[id]
		if pinned == 0 {
			pinned = 1
		}
		allowed := domain.RequirementActive(r)
		if !allowed && t.RequirementChangeID != "" {
			var change domain.RequirementChange
			if tx.Get("requirement_change", t.RequirementChangeID, &change) == nil && change.State == "applied" && change.Kind == "withdraw" && change.RequirementID == id && change.ContentRevision == pinned && change.GoalVersion == t.GoalVersion && change.GoalAuthorizationVersion == t.GoalAuthorizationVersion {
				for _, taskID := range change.TaskIDs {
					allowed = allowed || taskID == t.ID
				}
			}
		}
		if !allowed || pinned != domain.RequirementRevisionNumber(r) {
			return domain.Fail("requirement_changed", "需求已修订或撤销，任务需要重新规划")
		}
	}
	return nil
}

func pendingRequirementChange(tx *store.Tx, taskID, except string) (bool, error) {
	changes, e := store.List[domain.RequirementChange](tx, "requirement_change")
	if e != nil {
		return false, e
	}
	for _, change := range changes {
		if change.ID == except || change.State == "applied" || change.State == "superseded" {
			continue
		}
		var r domain.Requirement
		if e = tx.Get("requirement", change.RequirementID, &r); e != nil {
			return false, e
		}
		if domain.RequirementRevisionNumber(r) != change.ContentRevision {
			continue
		}
		for _, impact := range change.Impacts {
			if impact.Task.ID == taskID && !domain.TaskFinished(impact.Task) {
				return true, nil
			}
		}
	}
	return false, nil
}

func requirementPlanBindings(tx *store.Tx, ids []string) (map[string]int64, error) {
	bindings := map[string]int64{}
	for _, id := range ids {
		var r domain.Requirement
		if e := tx.Get("requirement", id, &r); e != nil {
			return nil, e
		}
		bindings[id] = domain.RequirementRevisionNumber(r)
	}
	return bindings, nil
}

func requirementPlanCurrent(tx *store.Tx, r domain.PlanningRun) bool {
	if len(r.RequirementRevisions) == 0 && r.RequirementChangeID == "" && r.Purpose != domain.ConsultationPurpose {
		d, e := goalDocument(tx)
		if e != nil {
			return false
		}
		requirements, e := authorizedGoalRequirements(tx, d)
		if e != nil {
			return false
		}
		for _, req := range requirements {
			if domain.RequirementRevisionNumber(req) != 1 {
				return false
			}
		}
	}
	for id, revision := range r.RequirementRevisions {
		var requirement domain.Requirement
		if tx.Get("requirement", id, &requirement) != nil || domain.RequirementRevisionNumber(requirement) != revision {
			return false
		}
	}
	if r.RequirementChangeID != "" {
		var change domain.RequirementChange
		var requirement domain.Requirement
		return tx.Get("requirement_change", r.RequirementChangeID, &change) == nil && change.State == "assessing" && tx.Get("requirement", change.RequirementID, &requirement) == nil && domain.RequirementRevisionNumber(requirement) == change.ContentRevision
	}
	return true
}

func requirementImpacts(tx *store.Tx, id string) ([]domain.RequirementImpact, error) {
	tasks, e := store.List[domain.Task](tx, "task")
	if e != nil {
		return nil, e
	}
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return nil, e
	}
	candidates, e := store.List[domain.Candidate](tx, "candidate")
	if e != nil {
		return nil, e
	}
	direct, affected := map[string]bool{}, map[string]bool{}
	for _, t := range tasks {
		for _, requirement := range t.RequirementIDs {
			if requirement == id {
				direct[t.ID], affected[t.ID] = true, true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, t := range tasks {
			for _, dependency := range t.Dependencies {
				if affected[dependency] && !affected[t.ID] && t.State != "cancelled" {
					affected[t.ID], changed = true, true
				}
			}
		}
	}
	impacts := []domain.RequirementImpact{}
	for _, t := range tasks {
		if !affected[t.ID] {
			continue
		}
		impact := domain.RequirementImpact{Task: t, Direct: direct[t.ID], Integrated: t.State == "integrated" || t.State == "verified"}
		for _, other := range t.RequirementIDs {
			if other == id {
				continue
			}
			var r domain.Requirement
			if e := tx.Get("requirement", other, &r); e != nil {
				return nil, e
			}
			impact.Shared = impact.Shared || domain.RequirementActive(r)
		}
		for _, run := range runs {
			if run.TaskID == t.ID && !run.Quiescent && (active(run) || run.State == "lost") {
				impact.NeedsStop = true
			}
		}
		for _, c := range candidates {
			if c.TaskID == t.ID && (c.State == "landing" || c.State == "reconciling") {
				impact.PublicationBusy = true
			}
		}
		impacts = append(impacts, impact)
	}
	return impacts, nil
}

func (s *Service) RequirementDetail(ctx context.Context, a Actor, id string, before int64) (domain.RequirementDetail, error) {
	var out domain.RequirementDetail
	if e := owner(a); e != nil {
		return out, e
	}
	if !domain.ChatIdentifier.MatchString(id) || before < 0 {
		return out, domain.Fail("invalid_input", "需求标识或历史游标无效")
	}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("requirement", id, &out.Requirement); e != nil {
			return e
		}
		var e error
		out.Impact, e = requirementImpacts(tx, id)
		if e != nil {
			return e
		}
		revision := domain.RequirementRevisionNumber(out.Requirement)
		if before > 0 && before <= revision {
			revision = before - 1
		}
		out.Revisions, out.Changes = []domain.RequirementRevision{}, []domain.RequirementChange{}
		for count := 0; revision > 0 && count < 20; revision, count = revision-1, count+1 {
			key := requirementRevisionID(id, revision)
			var history domain.RequirementRevision
			if e := tx.Get("requirement_revision", key, &history); e == nil {
				out.Revisions = append(out.Revisions, history)
			} else if !errors.Is(e, domain.ErrNotFound) {
				return e
			} else if revision == 1 {
				out.Revisions = append(out.Revisions, domain.RequirementRevision{Requirement: out.Requirement, CreatedAt: out.Requirement.CreatedAt})
			}
			var change domain.RequirementChange
			if e := tx.Get("requirement_change", key, &change); e == nil {
				out.Changes = append(out.Changes, change)
			} else if !errors.Is(e, domain.ErrNotFound) {
				return e
			}
			out.Before = revision
			if revision == 1 {
				out.Before = 0
			}
		}
		return nil
	})
	return out, e
}

func (s *Service) EditRequirement(ctx context.Context, a Actor, key, id, kind string, in domain.RequirementEdit) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if !domain.ChatIdentifier.MatchString(id) || in.ExpectedVersion < 1 || !domain.ChatText(in.Reason, 4096, true) || kind != "revise" && kind != "withdraw" {
		return nil, domain.Fail("invalid_input", "需要需求版本、变更类型和修改原因")
	}
	if kind == "revise" {
		if e := domain.ValidateRequirementProposal(domain.RequirementProposal{Title: in.Title, Body: in.Body}); e != nil {
			return nil, e
		}
	} else if in.Title != "" || in.Body != "" {
		return nil, domain.Fail("invalid_input", "撤销不能同时修改需求内容")
	}
	return s.mutate(ctx, a, key, "requirement."+id+"."+kind, in, func(tx *store.Tx) (any, error) {
		var r domain.Requirement
		if e := tx.Get("requirement", id, &r); e != nil {
			return nil, e
		}
		if e := version(r.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if !domain.RequirementActive(r) {
			return nil, domain.Fail("conflict", "需求已经撤销，历史记录保留")
		}
		if kind == "revise" && strings.TrimSpace(in.Title) == r.Title && in.Body == r.Body {
			return nil, domain.Fail("invalid_input", "需求内容没有变化")
		}
		impacts, e := requirementImpacts(tx, id)
		if e != nil {
			return nil, e
		}
		for _, impact := range impacts {
			if impact.PublicationBusy {
				return nil, domain.Fail("publication_in_progress", "相关代码正在发布对账，完成后再修订需求")
			}
		}
		if e = s.saveRequirementRevision(tx, Actor{ID: "legacy"}, r, ""); e != nil {
			return nil, e
		}
		r.ContentRevision = domain.RequirementRevisionNumber(r) + 1
		r.Version++
		r.UpdatedAt = s.Now()
		if kind == "withdraw" {
			r.State = "withdrawn"
		} else {
			r.Title, r.Body = strings.TrimSpace(in.Title), in.Body
		}
		change := domain.RequirementChange{ID: requirementRevisionID(id, r.ContentRevision), Version: 1, RequirementID: id, ContentRevision: r.ContentRevision, Kind: kind, State: "queued", Reason: in.Reason, Impacts: impacts, TaskIDs: []string{}, CreatedAt: s.Now()}
		d, e := goalDocument(tx)
		if e != nil {
			return nil, e
		}
		change.GoalVersion, change.GoalAuthorizationVersion = d.Version, d.AuthorizationVersion
		var prior domain.RequirementChange
		priorErr := tx.Get("requirement_change", requirementRevisionID(id, r.ContentRevision-1), &prior)
		if priorErr != nil && !errors.Is(priorErr, domain.ErrNotFound) {
			return nil, priorErr
		}
		if kind == "revise" && !goalAllowsRequirement(tx, d, id) && priorErr == nil && prior.Kind == "add" && prior.State != "applied" && prior.State != "superseded" && prior.GoalVersion == d.Version && prior.GoalAuthorizationVersion == d.AuthorizationVersion {
			change.Kind = "add"
		}
		previous, e := store.List[domain.RequirementChange](tx, "requirement_change")
		if e != nil {
			return nil, e
		}
		for _, old := range previous {
			if old.RequirementID == id && old.ContentRevision < r.ContentRevision && old.State != "applied" && old.State != "superseded" {
				old.State, old.Detail = "superseded", "已被后续需求版本取代"
				old.Version++
				if e = tx.Put("requirement_change", old.ID, old); e != nil {
					return nil, e
				}
			}
		}
		if len(impacts) == 0 && (d.Status != "admitted" || kind == "withdraw" && !goalAllowsRequirement(tx, d, id)) {
			change.State, change.Detail = "applied", "需求已更新，后续随项目规划"
		}
		for _, impact := range impacts {
			t := impact.Task
			if domain.TaskFinished(t) {
				continue
			}
			if e = requestStop(tx, t.ID); e != nil {
				return nil, e
			}
			if e = invalidateRequirementCandidate(tx, &t); e != nil {
				return nil, e
			}
			t.AuthorizationVersion++
			if t.HoldReason != "requirement_changed" {
				t.RequirementHoldReason = t.HoldReason
			}
			t.HoldReason = "requirement_changed"
			if t.Verification != nil || kind == "withdraw" && impact.Direct && !impact.Shared {
				t.State, t.HoldReason = "cancelled", ""
			}
			t.Version++
			if e = tx.Put("task", t.ID, t); e != nil {
				return nil, e
			}
			if e = releaseResources(tx, t.ID); e != nil {
				return nil, e
			}
			if impact.NeedsStop {
				change.State = "waiting_stop"
			}
		}
		if e = tx.Put("requirement", id, r); e != nil {
			return nil, e
		}
		if e = s.saveRequirementRevision(tx, a, r, in.Reason); e != nil {
			return nil, e
		}
		if e = tx.Put("requirement_change", change.ID, change); e != nil {
			return nil, e
		}
		if e = s.retireRequirementDecisions(tx); e != nil {
			return nil, e
		}
		return change, s.audit(tx, a, "requirement."+kind, id, change.ID)
	})
}

func copyRequirementBindings(t domain.Task) map[string]int64 {
	return maps.Clone(t.RequirementRevisions)
}

// Reassessment is an explicit Owner retry. It keeps every old Run and charge,
// and cannot release a stop barrier or authorize a different goal.
func (s *Service) ReassessRequirement(ctx context.Context, a Actor, key, id string, in domain.VersionRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "requirement.reassess."+id, in, func(tx *store.Tx) (any, error) {
		var req domain.Requirement
		if e := tx.Get("requirement", id, &req); e != nil {
			return nil, e
		}
		var change domain.RequirementChange
		if e := tx.Get("requirement_change", requirementRevisionID(id, domain.RequirementRevisionNumber(req)), &change); e != nil {
			return nil, e
		}
		if e := version(change.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if change.State != "blocked" {
			return nil, domain.Fail("conflict", "只能重新评估被阻断的变更")
		}
		plans, e := store.List[domain.PlanningRun](tx, "planning_run")
		if e != nil {
			return nil, e
		}
		for _, plan := range plans {
			if plan.RequirementChangeID == change.ID && !plan.TreeStopped && plan.GrantSeconds > 0 {
				return nil, domain.Fail("conflict", "旧规划执行尚未确认停止")
			}
		}
		d, e := goalDocument(tx)
		if e != nil {
			return nil, e
		}
		if d.Status != "admitted" {
			return nil, domain.Fail("conflict", "先完成目标批准")
		}
		if change.Kind != "add" && !goalAllowsRequirement(tx, d, change.RequirementID) {
			return nil, domain.Fail("scope", "当前目标未包含该需求，不能重新绑定变更")
		}
		change.GoalVersion, change.GoalAuthorizationVersion = d.Version, d.AuthorizationVersion
		change.State, change.Detail = "queued", ""
		change.Version++
		if e = tx.Put("requirement_change", change.ID, change); e != nil {
			return nil, e
		}
		return change, s.audit(tx, a, "requirement.reassessed", id, change.ID)
	})
}

func invalidateRequirementCandidate(tx *store.Tx, t *domain.Task) error {
	if t.CurrentCandidateID == "" {
		return nil
	}
	var c domain.Candidate
	if e := tx.Get("candidate", t.CurrentCandidateID, &c); e != nil {
		return e
	}
	if c.TaskID != t.ID {
		return domain.Fail("conflict", "候选与任务不匹配")
	}
	if c.State != "landed" && c.State != "rejected" && c.State != "failed" && c.State != "invalidated" {
		c.State, c.Detail = "invalidated", "需求已修订或撤销，固定候选不再适用"
		c.Version++
		if e := tx.Put("candidate", c.ID, c); e != nil {
			return e
		}
	}
	t.CurrentCandidateID = ""
	return nil
}
