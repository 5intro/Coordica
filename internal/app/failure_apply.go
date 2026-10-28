package app

import (
	"strings"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (c FailureRecoveries) apply(tx *store.Tx, run *domain.PlanningRun, goal *domain.PlanningDocument) error {
	if run.FailureResult == nil || !failurePlanningCurrent(tx, *run) {
		return domain.Fail("stale_run", "failure recovery plan no longer owns its source")
	}
	var f domain.FailureAssessment
	if err := tx.Get("failure_assessment", run.FailureAssessmentID, &f); err != nil {
		return err
	}
	f.Result = run.FailureResult
	f.Detail = f.Result.Reason
	switch f.Result.Action {
	case "repair_current":
		if err := c.repairCurrent(tx, f); err != nil {
			return err
		}
		f.State = "applied"
	case "retry_review_input":
		var candidate domain.Candidate
		if err := tx.Get("candidate", f.SourceCandidateID, &candidate); err != nil {
			return err
		}
		if candidate.Review == nil {
			return domain.Fail("conflict", "fixed blocked review required")
		}
		request := domain.ReviewInputRetryRequest{ExpectedVersion: f.SourceTask.Version,
			ExpectedCandidateID: candidate.ID, ExpectedCandidateVersion: candidate.Version,
			ExpectedCommit: candidate.Commit, ReviewRunID: candidate.Review.RunID, Reason: f.Result.Reason}
		if _, err := retryReviewInput(tx, c.Service, Actor{ID: "center"}, f.SourceTask.ID, request,
			"", "Leader requested bounded material retry"); err != nil {
			return err
		}
		if err := retireFailureTaskDecision(tx, c.Service, f); err != nil {
			return err
		}
		f.State = "applied"
	case "follow_up":
		if err := c.followup(tx, &f, run, goal); err != nil {
			return err
		}
	case "needs_owner":
		f.State = "needs_owner"
		if err := c.needsOwner(tx, &f, *goal); err != nil {
			return err
		}
	default:
		return domain.Fail("invalid_result", "unknown failure recovery action")
	}
	if err := c.save(tx, &f); err != nil {
		return err
	}
	run.State = "succeeded"
	message := domain.LeaderMessage{ID: ID("msg_"), DocumentVersion: goal.Version, Role: "leader",
		Text: f.Result.Reason, CreatedAt: c.Service.Now()}
	if err := tx.Put("leader_message", message.ID, message); err != nil {
		return err
	}
	return c.Service.audit(tx, Actor{ID: "center"}, "failure.assessment_applied", f.ID, f.Result.Action)
}

func (c FailureRecoveries) repairCurrent(tx *store.Tx, f domain.FailureAssessment) error {
	task := f.SourceTask
	if taskTimeExhausted(tx, task) {
		return domain.Fail("budget", "source task allowance is exhausted")
	}
	if task.Verification != nil {
		if task.Retries >= task.MaxRetries {
			return domain.Fail("budget", "source verification retry limit is exhausted")
		}
		task.Retries++
		task.State, task.HoldReason = "ready", ""
	} else {
		if task.Repairs >= task.MaxRepairs {
			return domain.Fail("budget", "source task repair limit is exhausted")
		}
		task.HoldReason = ""
		if f.SourceCandidateID != "" {
			var candidate domain.Candidate
			if err := tx.Get("candidate", f.SourceCandidateID, &candidate); err != nil {
				return err
			}
			queueRepair(&task, &candidate, true)
		} else {
			task.Repairs++
			task.State = "ready"
		}
	}
	task.Version++
	if err := tx.Put("task", task.ID, task); err != nil {
		return err
	}
	return retireFailureTaskDecision(tx, c.Service, f)
}

func (c FailureRecoveries) followup(tx *store.Tx, f *domain.FailureAssessment,
	run *domain.PlanningRun, goal *domain.PlanningDocument,
) error {
	if len(f.Result.Tasks) != 1 {
		return domain.Fail("invalid_result", "one follow-up task required")
	}
	if taskTimeExhausted(tx, f.SourceTask) {
		return domain.Fail("budget", "source task's explicit time allowance cannot fund a recheck")
	}
	proposal := &f.Result.Tasks[0]
	if f.SourceTask.FailureSourceTaskID != "" || len(proposal.Dependencies) != 0 {
		return domain.Fail("conflict", "automatic follow-up must be one independent task, never a recursive replacement")
	}
	previous, err := store.List[domain.FailureAssessment](tx, "failure_assessment")
	if err != nil {
		return err
	}
	for _, old := range previous {
		if old.SourceTask.ID == f.SourceTask.ID && old.FollowupTaskID != "" {
			return domain.Fail("budget", "automatic follow-up has already been used for this source task")
		}
	}
	if f.SourceTask.Verification == nil && protectedOverlap(proposal.Paths, f.SourceTask.AllowedPaths) {
		return domain.Fail("conflict", "same-scope work must use the source task's remaining repair allowance")
	}
	if len(proposal.RequirementIDs) == 0 {
		return domain.Fail("scope", "follow-up requires source requirements")
	}
	for _, id := range proposal.RequirementIDs {
		if !containsPaths(f.SourceTask.RequirementIDs, []string{id}) {
			return domain.Fail("scope", "follow-up requirement is outside its source")
		}
	}
	if proposal.TestRunID != "" && (f.SourceTask.Verification == nil || proposal.TestRunID != f.SourceRunID) {
		return domain.Fail("invalid_result", "follow-up may only cite its actual failed verification")
	}
	q := *proposal
	if f.SourceTask.Verification != nil {
		q.TestRunID = f.SourceRunID
	}
	q.Key = f.ID
	q.Dependencies = []string{}
	result := domain.LeaderResult{Reply: f.Result.Reason, Ready: true, Tasks: []domain.TaskProposal{q},
		MissingItems: []string{}, Clarifications: []string{}, Scopes: []domain.ScopeProposal{}, MajorDecisions: []domain.MajorDecisionProposal{}}
	if err := c.Service.derivePlan(tx, run, goal, result); err != nil {
		return err
	}
	if len(run.TaskIDs) != 1 {
		return domain.Fail("invalid_result", "follow-up must create exactly one ordinary task")
	}
	var task domain.Task
	if err := tx.Get("task", run.TaskIDs[0], &task); err != nil {
		return err
	}
	task.FailureSourceTaskID, task.FailureAssessmentID = f.SourceTask.ID, f.ID
	if err := tx.Put("task", task.ID, task); err != nil {
		return err
	}
	source := f.SourceTask
	source.HoldReason = "failure_followup"
	source.Version++
	if err := tx.Put("task", source.ID, source); err != nil {
		return err
	}
	// The source is stopped and its rejected candidate cannot be integrated.
	// Suspend its resource reservation while related work runs; ordinary Claim
	// must reacquire resources before any later source execution.
	if err := releaseResources(tx, source.ID); err != nil {
		return err
	}
	f.State, f.FollowupTaskID = "waiting_followup", task.ID
	f.WaitingTaskVersion = source.Version
	return retireFailureTaskDecision(tx, c.Service, *f)
}

func (c FailureRecoveries) needsOwner(tx *store.Tx, f *domain.FailureAssessment, goal domain.PlanningDocument) error {
	var task domain.Task
	if err := tx.Get("task", f.SourceTask.ID, &task); err != nil {
		return err
	}
	task.HoldReason = "human_decision"
	task.Version++
	if err := tx.Put("task", task.ID, task); err != nil {
		return err
	}
	return c.Service.decision(tx, "task", task.ID, task.Version, goal, "human_decision",
		f.Detail, []string{"dismiss", "edit_task", "edit_budget"})
}

func retireFailureTaskDecision(tx *store.Tx, s *Service, f domain.FailureAssessment) error {
	decisions, err := store.List[domain.Decision](tx, "decision")
	if err != nil {
		return err
	}
	for _, d := range decisions {
		if d.Status != "open" || d.SourceKind != "task" || d.SourceID != f.SourceTask.ID ||
			d.SourceVersion > f.SourceTask.Version || d.DocumentVersion != f.SourceTask.GoalVersion ||
			!strings.Contains("|human_decision|budget|tests_failed|", "|"+d.Kind+"|") {
			continue
		}
		d.Status, d.Resolution = "superseded", "failure recovery under existing authority; no new Owner grant"
		d.Version++
		if err := tx.Put("decision", d.ID, d); err != nil {
			return err
		}
		if err := s.audit(tx, Actor{ID: "center"}, "failure.decision_superseded", d.ID, f.ID); err != nil {
			return err
		}
	}
	return nil
}
