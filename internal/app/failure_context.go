package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func assignmentFailureContext(tx *store.Tx, task domain.Task) (*domain.FailureContext, error) {
	id := task.FailureAssessmentID
	if id == "" {
		id = task.FailureRecheckID
	}
	if id == "" {
		return nil, nil
	}
	var f domain.FailureAssessment
	if err := tx.Get("failure_assessment", id, &f); err != nil {
		return nil, err
	}
	recheck := task.FailureRecheckID == f.ID && f.SourceTask.ID == task.ID && f.State == "applied" &&
		f.RecheckAuthorizationVersion == task.AuthorizationVersion
	if task.FailureRecheckID == f.ID && !recheck {
		return nil, nil // A later Owner revision does not inherit this old repair context.
	}
	if f.Result == nil || !recheck && f.FollowupTaskID != task.ID {
		return nil, domain.Fail("conflict", "follow-up source context is missing")
	}
	result := &domain.FailureContext{Category: f.Result.Category, FollowupCommit: f.FollowupCommit, AssessmentID: f.ID, SourceTaskID: f.SourceTask.ID,
		SourceRunID: f.SourceRunID, SourceCommit: f.SourceCommit, SourceTitle: f.SourceTask.Title,
		SourceAcceptance: f.SourceTask.Acceptance, Reason: f.Result.Reason, Findings: []string{}}
	var run domain.Run
	if err := tx.Get("run", f.SourceRunID, &run); err != nil {
		return nil, err
	}
	result.ExecutionDetail = run.Detail
	result.CheckResults = run.CheckResults
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
			for _, issue := range candidate.Review.Issues {
				result.Findings = append(result.Findings, issue.Severity+" "+issue.Title+": "+issue.Evidence)
			}
		}
	}
	return result, nil
}
