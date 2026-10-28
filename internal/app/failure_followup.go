package app

import (
	"coordica/internal/domain"
	"coordica/internal/store"
	"errors"
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
	if requireQuiescence(tx, followup.ID) != nil || followup.CurrentRunID != "" {
		return nil
	}
	if handled, err := c.precheckFollowup(tx, f, followup); handled || err != nil {
		return err
	}
	if followup.State != "integrated" {
		failed, err := followupCannotContinue(tx, followup)
		if err != nil || !failed {
			return err
		}
		return c.stopFollowup(tx, f, "follow-up stopped without integration; original acceptance remains failed")
	}
	var source domain.Task
	if err := tx.Get("task", f.SourceTask.ID, &source); err != nil {
		return err
	}
	if taskTimeExhausted(tx, source) || source.FailureRecheckID != "" {
		return c.stopFollowup(tx, f, "follow-up completed; explicit allowance exhausted or bounded recheck already issued")
	}
	candidate, err := integratedFollowup(tx, followup, *f)
	if err != nil {
		var problem *domain.Error
		if errors.As(err, &problem) || errors.Is(err, domain.ErrNotFound) {
			return c.stopFollowup(tx, f, "follow-up integration evidence is invalid; recheck was not authorized: "+err.Error())
		}
		return err
	}
	if source.Verification != nil {
		var currentSuite domain.TestSuite
		if err := tx.Get("test_suite", source.Verification.Suite.ID, &currentSuite); err != nil {
			return err
		}
		if currentSuite.Version != source.Verification.Suite.Version || suiteDigest(currentSuite) != source.Verification.Digest {
			return c.stopFollowup(tx, f, "verification suite changed; a new explicit test binding is required")
		}
		source.Verification.Commit = candidate.Commit
		if err := store.PinTestCatalog(tx, source.ID, source.Verification.Suite.ID); err != nil {
			return err
		}
	}
	// Follow-up changed the program under test. A new source execution must use
	// the current main, never silently inherit the earlier rejected code tree.
	source.AuthorizationVersion++
	source.FailureRecheckID = f.ID
	source.Version++
	source.ApprovedVersion = source.Version
	source.State, source.HoldReason = "ready", ""
	source.CurrentCandidateID = ""
	if err := tx.Put("task", source.ID, source); err != nil {
		return err
	}
	f.RecheckAuthorizationVersion, f.FollowupCommit = source.AuthorizationVersion, candidate.Commit
	f.State, f.Detail = "applied", "follow-up integrated; one independent recheck queued without resetting original counters"
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
		(f.FollowupTaskID != task.ID && f.PrecheckTaskID != task.ID) || f.SourceTask.ID != task.FailureSourceTaskID ||
		f.SourceTask.FailureAssessmentID != "" ||
		f.State != "waiting_followup" || !failureCurrent(tx, f, true) {
		return domain.Fail("authorization_changed", "failure follow-up source authority changed")
	}
	return nil
}
