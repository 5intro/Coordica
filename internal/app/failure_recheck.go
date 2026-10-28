package app

import (
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func integratedFollowup(tx *store.Tx, task domain.Task, f domain.FailureAssessment) (domain.Candidate, error) {
	var candidate domain.Candidate
	if task.CurrentCandidateID == "" {
		return candidate, domain.Fail("conflict", "follow-up requires its exact integrated candidate")
	}
	if err := tx.Get("candidate", task.CurrentCandidateID, &candidate); err != nil {
		return candidate, err
	}
	if candidate.TaskID != task.ID || candidate.State != "landed" || candidate.AuthorizationVersion != task.AuthorizationVersion ||
		candidate.PolicyVersion != f.PolicyVersion || candidate.BaselineVersion != task.BaselineVersion || candidate.BaselineDigest != task.BaselineDigest {
		return candidate, domain.Fail("conflict", "follow-up integrated candidate authority mismatch")
	}
	if _, err := gitops.ParseOID(candidate.Commit); err != nil {
		return candidate, domain.Fail("conflict", "follow-up integrated commit is invalid")
	}
	return candidate, nil
}

func followupCannotContinue(tx *store.Tx, task domain.Task) (bool, error) {
	if domain.TaskFinished(task) {
		return true, nil
	}
	switch task.HoldReason {
	case "human_decision", "budget", "tests_failed", "failure_assessment":
		// Let the child's own finite same-scope recovery finish first. It may
		// never create a recursive follow-up or reset its original counters.
		failures, err := store.List[domain.FailureAssessment](tx, "failure_assessment")
		if err != nil {
			return false, err
		}
		for _, child := range failures {
			if child.SourceTask.ID == task.ID && (child.State == "pending" || child.State == "assessing") && failureCurrent(tx, child, false) {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func (c FailureRecoveries) stopFollowup(tx *store.Tx, f *domain.FailureAssessment, detail string) error {
	f.State, f.Detail = "needs_owner", detail
	goal, err := goalDocument(tx)
	if err != nil {
		return err
	}
	if err := c.needsOwner(tx, f, goal); err != nil {
		return err
	}
	return c.save(tx, f)
}
