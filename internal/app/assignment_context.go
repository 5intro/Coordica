package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func assignmentContextCompatible(req domain.ClaimRequest, t domain.Task) bool {
	return len(t.RequirementIDs) == 0 || req.AssignmentContextVersion == domain.AssignmentContextVersion
}

func (s *Service) blockTaskContext(tx *store.Tx, a Actor, t *domain.Task, reason error) error {
	t.HoldReason = "human_decision"
	t.Version++
	if err := tx.Put("task", t.ID, t); err != nil {
		return err
	}
	if err := s.taskDecision(tx, *t, reason.Error()); err != nil {
		return err
	}
	return s.audit(tx, a, "task.context_blocked", t.ID, reason.Error())
}

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

// Construct and validate the complete fixed context before a claim reserves
// resources. The caller adds the new Run identity inside the same transaction.
func assignedTaskContext(tx *store.Tx, task domain.Task, run domain.Run, project domain.Project, candidate *domain.Candidate) (domain.Assignment, error) {
	result := domain.Assignment{Task: task, Run: run, Project: project, Candidate: candidate,
		FailureRecoveryVersion: 1, HeartbeatSeconds: 10}
	if task.GoalVersion == 0 {
		result.FailureRecoveryVersion = 0
	}
	var err error
	result.Requirements, err = assignedRequirements(tx, task, run)
	if err != nil {
		return result, err
	}
	result.GoalContext, err = assignedRunGoal(tx, run)
	if err != nil {
		return result, err
	}
	baseline, err := initialization(tx)
	if err != nil {
		return result, err
	}
	result.Baseline = baseline.ApprovedBaseline
	result.FailureContext, err = assignmentFailureContext(tx, task)
	if err != nil {
		return result, err
	}
	result.ReviewContext, err = assignedReviewContext(tx, task, run)
	if err != nil {
		return result, err
	}
	return result, domain.ValidateAssignmentContext(result)
}
