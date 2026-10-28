package app

import (
	"runtime"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// A critical failed suite fences merges. Run that unchanged suite on the fixed
// repair candidate before landing, so recovery never needs to bypass the fence.
// The original source still gets its own post-integration acceptance execution.
func (c FailureRecoveries) precheckFollowup(tx *store.Tx, f *domain.FailureAssessment, child domain.Task) (bool, error) {
	spec := f.SourceTask.Verification
	if spec == nil || !spec.Suite.BlockMergesOnFailure || child.State == "integrated" {
		return false, nil
	}
	if child.State != "awaiting_integration" || child.CurrentCandidateID == "" {
		return false, nil
	}
	var candidate domain.Candidate
	if err := tx.Get("candidate", child.CurrentCandidateID, &candidate); err != nil {
		return true, err
	}
	if candidate.State != "ready_to_land" {
		return false, nil
	}
	if f.PrecheckTaskID != "" {
		var check domain.Task
		if err := tx.Get("task", f.PrecheckTaskID, &check); err != nil {
			return true, err
		}
		if check.Verification == nil || check.Verification.Commit != candidate.Commit || check.Verification.Digest != spec.Digest {
			return true, c.stopFollowup(tx, f, "repair candidate changed after its bounded critical precheck")
		}
		if check.CurrentRunID != "" || requireQuiescence(tx, check.ID) != nil {
			return true, nil
		}
		if check.State == "verified" {
			return false, nil
		}
		if check.State == "cancelled" || check.HoldReason != "" {
			return true, c.stopFollowup(tx, f, "critical candidate precheck failed or stopped; original acceptance remains failed")
		}
		return true, nil
	}
	var suite domain.TestSuite
	if err := tx.Get("test_suite", spec.Suite.ID, &suite); err != nil {
		return true, err
	}
	if suite.Version != spec.Suite.Version || suiteDigest(suite) != spec.Digest || !domain.TestPlatformAllowed(suite, runtime.GOOS) {
		return true, c.stopFollowup(tx, f, "critical precheck suite changed or requires an unavailable center platform")
	}
	project, err := getProject(tx)
	if err != nil {
		return true, err
	}
	if !candidateAuthority(tx, child, project, candidate) || candidate.TaskID != child.ID || candidate.Commit == "" {
		return true, c.stopFollowup(tx, f, "critical precheck requires the exact current repair candidate")
	}
	check, err := verificationTask(tx, c.Service, domain.VerificationRequest{Commit: candidate.Commit,
		RequirementIDs: f.SourceTask.RequirementIDs, BudgetSeconds: f.SourceTask.BudgetSeconds, MaxRunSeconds: f.SourceTask.MaxRunSeconds}, suite)
	if err != nil {
		return true, err
	}
	check.Title = "修复候选复验 · " + suite.Name
	check.FailureSourceTaskID, check.FailureAssessmentID = f.SourceTask.ID, f.ID
	if err := tx.Put("task", check.ID, check); err != nil {
		return true, err
	}
	if err := store.PinTestCatalog(tx, check.ID, suite.ID); err != nil {
		return true, err
	}
	f.PrecheckTaskID = check.ID
	if err := c.save(tx, f); err != nil {
		return true, err
	}
	return true, c.Service.audit(tx, Actor{ID: "center"}, "failure.candidate_precheck_queued", f.ID, check.ID)
}
