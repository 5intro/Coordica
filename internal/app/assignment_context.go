package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func assignedRunGoal(tx *store.Tx, r domain.Run) (*domain.GoalContext, error) {
	if r.GoalVersion == 0 {
		return nil, nil
	}
	goal, err := assignedGoalContext(tx)
	if err != nil {
		return nil, err
	}
	if goal == nil || goal.Version != r.GoalVersion || goal.AuthorizationVersion != r.GoalAuthorizationVersion {
		return nil, domain.Fail("goal_changed", "执行所属目标版本已变化")
	}
	return goal, nil
}

func assignedRequirements(tx *store.Tx, t domain.Task, r domain.Run) ([]domain.RequirementContext, error) {
	out := []domain.RequirementContext{}
	seen := map[string]bool{}
	for _, id := range t.RequirementIDs {
		if seen[id] {
			return nil, domain.Fail("requirement_changed", "任务需求绑定重复")
		}
		seen[id] = true
		revision := r.RequirementRevisions[id]
		if revision == 0 {
			revision = 1
		} // Existing legacy grants represent revision one.
		var req domain.Requirement
		if err := tx.Get("requirement", id, &req); err != nil {
			return nil, err
		}
		if domain.RequirementRevisionNumber(req) != revision {
			return nil, domain.Fail("requirement_changed", "执行绑定的需求正文版本已变化")
		}
		out = append(out, domain.RequirementContext{ID: id, ContentRevision: revision, State: req.State, Title: req.Title, Body: req.Body})
	}
	for id := range r.RequirementRevisions {
		if !seen[id] {
			return nil, domain.Fail("requirement_changed", "执行需求绑定与任务不匹配")
		}
	}
	return out, nil
}
