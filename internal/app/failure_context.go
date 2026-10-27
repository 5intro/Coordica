package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func assignmentFailureContext(tx *store.Tx, task domain.Task) (*domain.FailureContext, error) {
	if task.FailureAssessmentID == "" {
		return nil, nil
	}
	var f domain.FailureAssessment
	if err := tx.Get("failure_assessment", task.FailureAssessmentID, &f); err != nil {
		return nil, err
	}
	if f.Result == nil || f.FollowupTaskID != task.ID {
		return nil, domain.Fail("conflict", "follow-up source context is missing")
	}
	result := &domain.FailureContext{AssessmentID: f.ID, SourceTaskID: f.SourceTask.ID,
		SourceRunID: f.SourceRunID, SourceCommit: f.SourceCommit, SourceTitle: f.SourceTask.Title,
		SourceAcceptance: f.SourceTask.Acceptance, Reason: f.Result.Reason, Findings: []string{}}
	var run domain.Run
	if err := tx.Get("run", f.SourceRunID, &run); err != nil {
		return nil, err
	}
	result.ExecutionDetail = run.Detail
	if run.TestReport != nil {
		result.CheckResults = run.TestReport.Checks
	}
	if f.SourceTask.Verification != nil {
		result.SourceChecks = f.SourceTask.Verification.Suite.Checks
	}
	if f.SourceCandidateID != "" {
		var candidate domain.Candidate
		if err := tx.Get("candidate", f.SourceCandidateID, &candidate); err != nil {
			return nil, err
		}
		if result.CheckResults == nil {
			result.CheckResults = candidate.Checks
		}
		if result.SourceChecks == nil && candidate.CheckPlan != nil {
			result.SourceChecks = candidate.CheckPlan.Checks
		}
		if candidate.Review != nil {
			result.Findings = candidate.Review.Findings
		}
	}
	return result, nil
}
