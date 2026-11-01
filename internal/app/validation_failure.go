package app

import (
	"errors"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process/checkexec"
)

func validationFailureKind(err error) string {
	if err == nil {
		return ""
	}
	var outputs *gitops.UndeclaredCheckOutputs
	if errors.As(err, &outputs) {
		return "check_outputs"
	}
	var check *checkexec.Failure
	if errors.As(err, &check) {
		return check.Kind
	}
	if errors.Is(err, gitops.ErrDirty) {
		return "check_source_changed"
	}
	return "check_infrastructure"
}

func checkConfigurationFailure(kind string) bool {
	return kind == "check_outputs" || kind == "check_configuration" || kind == "check_start"
}

func validationStage(candidate domain.Candidate) string {
	if candidate.Review != nil && candidate.Review.Verdict == "approve" {
		return "validating"
	}
	return "prechecking"
}

// Preserve the rejected fixed code and evidence before diagnosis. Configuring
// trusted commands/outputs is Owner work, never a developer's code repair.
func routeValidationFailure(task *domain.Task, candidate *domain.Candidate, run domain.Run) {
	if checkConfigurationFailure(run.FailureKind) {
		task.HoldReason = "check_configuration"
		return
	}
	if task.GoalVersion == 0 {
		queueRepair(task, candidate, true) // Legacy tasks have no Leader recovery contract.
		return
	}
	if run.FailureKind == "check_infrastructure" && task.Retries < task.MaxRetries {
		task.Retries++
		candidate.State = validationStage(*candidate)
		candidate.CheckRunID = ""
		return
	}
	task.HoldReason = "failure_assessment"
}
