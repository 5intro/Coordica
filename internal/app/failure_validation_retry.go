package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

// Re-execute checks on exactly the rejected candidate. This cannot turn a
// verdict into approval, replace code or refresh any cumulative retry counter.
func (c FailureRecoveries) retryValidation(tx *store.Tx, f domain.FailureAssessment) error {
	task := f.SourceTask
	if f.Result == nil || f.Result.Category != "execution_failure" && f.Result.Category != "test_error" ||
		task.Verification != nil || f.SourceCandidateID == "" || task.Retries >= task.MaxRetries || taskTimeExhausted(tx, task) {
		return domain.Fail("conflict", "bounded candidate validation retry is unavailable")
	}
	var run domain.Run
	if err := tx.Get("run", f.SourceRunID, &run); err != nil {
		return err
	}
	var candidate domain.Candidate
	if err := tx.Get("candidate", f.SourceCandidateID, &candidate); err != nil {
		return err
	}
	project, err := getProject(tx)
	if err != nil {
		return err
	}
	if run.Kind != "validation" || run.State != "failed" || !run.Quiescent ||
		run.CandidateID != candidate.ID || run.Base != candidate.Commit || candidate.CheckPlan == nil ||
		candidate.CheckPlan.PolicyVersion != project.TestPolicyVersion || candidate.CheckPlan.Digest != run.CheckPlanDigest ||
		checkConfigurationFailure(run.FailureKind) || run.FailureKind == "check_source_changed" {
		return domain.Fail("conflict", "same stopped candidate and unchanged trusted check plan required")
	}
	candidate.State = validationStage(candidate)
	candidate.CheckRunID = ""
	candidate.Version++
	task.State, task.HoldReason, task.CurrentRunID = "awaiting_integration", "", ""
	task.Retries++
	task.Version++
	if err := tx.Put("candidate", candidate.ID, candidate); err != nil {
		return err
	}
	if err := tx.Put("task", task.ID, task); err != nil {
		return err
	}
	return retireFailureTaskDecision(tx, c.Service, f)
}
