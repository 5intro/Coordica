package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
)

func (c FailureRecoveries) finishFollowup(tx *store.Tx, f *domain.FailureAssessment) error {
	if !failureCurrent(tx, *f, true) {
		f.State, f.Detail = "stale", "source task, execution, policy or goal changed while follow-up ran"
		return c.save(tx, f)
	}
	var followup domain.Task
	if err := tx.Get("task", f.FollowupTaskID, &followup); err != nil {
		return err
	}
	if followup.State == "cancelled" && requireQuiescence(tx, followup.ID) == nil {
		f.State, f.Detail = "needs_owner", "follow-up was cancelled; original failure remains unresolved"
		goal, err := goalDocument(tx)
		if err != nil {
			return err
		}
		if err := c.needsOwner(tx, f, goal); err != nil {
			return err
		}
		return c.save(tx, f)
	}
	if followup.State != "integrated" && followup.State != "verified" {
		return nil
	}
	if requireQuiescence(tx, followup.ID) != nil {
		return nil
	}
	var source domain.Task
	if err := tx.Get("task", f.SourceTask.ID, &source); err != nil {
		return err
	}
	remaining := source.BudgetSeconds - source.UsedSeconds - source.ReservedSeconds
	canRetry := source.Repairs < source.MaxRepairs
	if source.Verification != nil {
		canRetry = source.Retries < source.MaxRetries
	}
	if remaining <= 0 || !canRetry {
		f.State, f.Detail = "needs_owner", "follow-up completed; original task allowance or retry/repair limit remains exhausted"
		goal, err := goalDocument(tx)
		if err != nil {
			return err
		}
		if err = c.needsOwner(tx, f, goal); err != nil {
			return err
		}
		return c.save(tx, f)
	}
	if source.Verification != nil {
		var candidate domain.Candidate
		if followup.CurrentCandidateID == "" || tx.Get("candidate", followup.CurrentCandidateID, &candidate) != nil ||
			candidate.TaskID != followup.ID || candidate.State != "landed" || candidate.Commit == "" {
			return domain.Fail("conflict", "verification follow-up requires its exact integrated code candidate")
		}
		var currentSuite domain.TestSuite
		if err := tx.Get("test_suite", source.Verification.Suite.ID, &currentSuite); err != nil {
			return err
		}
		if currentSuite.Version != source.Verification.Suite.Version || suiteDigest(currentSuite) != source.Verification.Digest {
			f.State, f.Detail = "needs_owner", "verification suite changed; a new explicit test binding is required"
			goal, err := goalDocument(tx)
			if err != nil {
				return err
			}
			if err := c.needsOwner(tx, f, goal); err != nil {
				return err
			}
			return c.save(tx, f)
		}
		source.Verification.Commit = candidate.Commit
		source.Retries++
		if err := store.PinTestCatalog(tx, source.ID, source.Verification.Suite.ID); err != nil {
			return err
		}
	} else {
		source.Repairs++
	}
	// Follow-up changed the program under test. A new source execution must use
	// the current main, never silently inherit the earlier rejected code tree.
	source.AuthorizationVersion++
	source.Version++
	source.ApprovedVersion = source.Version
	source.State, source.HoldReason = "ready", ""
	source.CurrentCandidateID = ""
	if err := tx.Put("task", source.ID, source); err != nil {
		return err
	}
	f.State, f.Detail = "applied", "follow-up integrated; original task queued for a fresh bounded execution"
	if err := c.save(tx, f); err != nil {
		return err
	}
	return c.Service.audit(tx, Actor{ID: "center"}, "failure.source_requeued", source.ID, f.ID)
}

// Origin changes revoke new/live follow-up work. Historical completed evidence
// stays historical; the parent being requeued cannot rewrite a child's result.
func failureTaskAuthority(tx *store.Tx, task domain.Task) error {
	if task.FailureAssessmentID == "" || domain.TaskFinished(task) {
		return nil
	}
	var f domain.FailureAssessment
	if tx.Get("failure_assessment", task.FailureAssessmentID, &f) != nil ||
		f.FollowupTaskID != task.ID || f.SourceTask.ID != task.FailureSourceTaskID ||
		f.SourceTask.FailureAssessmentID != "" ||
		f.State != "waiting_followup" || !failureCurrent(tx, f, true) {
		return domain.Fail("authorization_changed", "failure follow-up source authority changed")
	}
	return nil
}
