package app

import (
	"errors"
	"fmt"
	"maps"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// captureTaskFailure runs in the source transition's transaction. It records
// stopped execution facts, not a diagnosis inferred from an error string.
func captureTaskFailure(tx *store.Tx, s *Service, task domain.Task, detail string) error {
	if task.GoalVersion == 0 || domain.TaskFinished(task) || task.CurrentRunID != "" ||
		(task.HoldReason != "failure_assessment" && task.HoldReason != "human_decision" &&
			task.HoldReason != "tests_failed" && task.HoldReason != "budget") {
		return nil
	}
	project, err := getProject(tx)
	if err != nil {
		return err
	}
	if requireQuiescence(tx, task.ID) != nil || goalAuthority(tx, &task, false) != nil {
		return nil
	}
	run, candidate, err := failureSource(tx, task)
	if err != nil || run.ID == "" {
		return err
	}
	key := fmt.Sprintf("%s:%d:%d:%s:%s", task.ID, task.Generation, task.AuthorizationVersion, run.ID, candidate.ID)
	id := "failure_" + Hash(key)[:32]
	var previous domain.FailureAssessment
	if err = tx.Get("failure_assessment", id, &previous); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	detail, _ = boundedFailureText(detail, 4096)
	sourceCommit := candidate.Commit
	if candidate.ID == "" {
		sourceCommit = run.Base
	}
	v := domain.FailureAssessment{
		ID: id, Version: 1, SourceTask: task, SourceRunID: run.ID, SourceRunVersion: run.Version,
		SourceCandidateID: candidate.ID, SourceCandidateVersion: candidate.Version, SourceCommit: sourceCommit,
		PolicyVersion: project.PolicyVersion, TestPolicyVersion: project.TestPolicyVersion,
		State: "pending", Detail: detail, CreatedAt: s.Now(), UpdatedAt: s.Now(),
	}
	if err = tx.Put("failure_assessment", id, v); err != nil {
		return err
	}
	return s.audit(tx, Actor{ID: "center"}, "failure.recorded", id, task.ID)
}

func failureSource(tx *store.Tx, task domain.Task) (domain.Run, domain.Candidate, error) {
	var candidate domain.Candidate
	if task.CurrentCandidateID != "" {
		if err := tx.Get("candidate", task.CurrentCandidateID, &candidate); err != nil {
			return domain.Run{}, candidate, err
		}
		if candidate.TaskID != task.ID || candidate.State != "rejected" || candidate.AuthorizationVersion != task.AuthorizationVersion ||
			candidate.BaselineVersion != task.BaselineVersion || candidate.BaselineDigest != task.BaselineDigest {
			return domain.Run{}, candidate, nil
		}
	}
	project, err := getProject(tx)
	if err != nil {
		return domain.Run{}, candidate, err
	}
	if candidate.ID != "" && candidate.PolicyVersion != project.PolicyVersion {
		return domain.Run{}, candidate, nil
	}
	runs, err := store.List[domain.Run](tx, "run")
	if err != nil {
		return domain.Run{}, candidate, err
	}
	for _, run := range runs {
		if run.TaskID != task.ID || run.Generation != task.Generation || !run.Quiescent || run.FinishedAt == nil ||
			run.AuthorizationVersion != task.AuthorizationVersion || run.GoalVersion != task.GoalVersion ||
			run.GoalAuthorizationVersion != task.GoalAuthorizationVersion || run.PolicyVersion != project.PolicyVersion ||
			run.BaselineVersion != task.BaselineVersion || run.BaselineDigest != task.BaselineDigest ||
			!maps.Equal(run.RequirementRevisions, task.RequirementRevisions) || active(run) {
			continue
		}
		if task.Verification != nil {
			spec := task.Verification
			if run.Kind != "verification" || run.State != "failed" || run.Base != spec.Commit ||
				run.TestReport == nil || run.TestReport.Commit != spec.Commit || run.TestReport.Digest != spec.Digest {
				continue
			}
		} else if run.Kind == "verification" {
			continue
		}
		if candidate.ID != "" {
			if run.CandidateID != candidate.ID && run.ID != candidate.SourceRunID {
				continue
			}
			if run.Kind == "validation" && run.State == "failed" {
				return run, candidate, nil
			}
			if candidate.Review != nil && (candidate.Review.RunID != run.ID || candidate.Review.Verdict == "approve") ||
				candidate.Review == nil && (!candidate.PreparationStopped || candidate.SourceRunID != run.ID) {
				continue
			}
		} else if run.State != "failed" && run.State != "stopped" {
			continue
		}
		return run, candidate, nil
	}
	return domain.Run{}, candidate, nil
}
