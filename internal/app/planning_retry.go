package app

import (
	"errors"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// One retry sequence covers process failures and deterministic input-contract
// rejections. Each attempt retains its own receipt and cumulative accounting.
func queuePlanningRetry(tx *store.Tx, s *Service, r domain.PlanningRun, d domain.PlanningDocument) (bool, error) {
	settings, err := automation(tx)
	if err != nil || r.Attempt >= r.MaxRetries || planningGrant(settings, d) <= 0 {
		return false, err
	}
	retry := r
	retry.ID, retry.Version, retry.Generation, retry.Attempt = ID("plan_"), 1, r.Generation+1, r.Attempt+1
	retry.State, retry.Settled, retry.TreeStopped = "queued", false, false
	retry.GrantSeconds, retry.ElapsedSeconds, retry.ProcessID = 0, 0, 0
	retry.WorkingDirectory, retry.SessionID = "", ""
	retry.StartedAt, retry.Deadline, retry.LeaseUntil = time.Time{}, time.Time{}, time.Time{}
	retry.PendingResult, retry.RequirementResult, retry.FailureResult = nil, nil, nil
	retry.TaskIDs, retry.TaskMapping = nil, nil
	if err = tx.Put("planning_run", retry.ID, retry); err != nil {
		return false, err
	}
	if r.FailureAssessmentID != "" {
		var f domain.FailureAssessment
		if err = tx.Get("failure_assessment", r.FailureAssessmentID, &f); err != nil {
			return false, err
		}
		f.RunID = retry.ID
		if err = (FailureRecoveries{Service: s}).save(tx, &f); err != nil {
			return false, err
		}
	}
	return true, s.audit(tx, Actor{ID: "center"}, "planning.retry_queued", retry.ID, r.ID)
}

func retryPlanningInputRejection(tx *store.Tx, s *Service, r domain.PlanningRun, d domain.PlanningDocument, cause error) (bool, error) {
	var failure *domain.Error
	if !errors.As(cause, &failure) || failure.Code != "planning_inputs" ||
		r.Purpose != "" || r.ScopeRequestID != "" || r.RequirementChangeID != "" || !r.Settled || !r.TreeStopped {
		return false, nil
	}
	p, err := getProject(tx)
	if err != nil {
		return false, err
	}
	settings, err := automation(tx)
	if err != nil || p.Paused || !settings.Enabled || settings.Version != r.AutomationVersion ||
		d.Version != r.DocumentVersion || d.AuthorizationVersion != r.AuthorizationVersion ||
		d.ApprovedVersion != d.Version || d.ApprovedAuthorizationVersion != d.AuthorizationVersion || baselineGate(tx, nil, false) != nil {
		return false, err
	}
	return queuePlanningRetry(tx, s, r, d)
}
