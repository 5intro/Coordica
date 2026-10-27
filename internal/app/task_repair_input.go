package app

import (
	"errors"
	"maps"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func rejectedRepairSource(tx *store.Tx, task domain.Task, project domain.Project, c domain.Candidate) error {
	if c.TaskID != task.ID || c.State != "rejected" || c.AuthorizationVersion != task.AuthorizationVersion ||
		c.PolicyVersion != project.PolicyVersion || c.BaselineVersion != task.BaselineVersion || c.BaselineDigest != task.BaselineDigest ||
		c.Review == nil || c.Review.Candidate != c.Commit || (c.Review.Verdict != "blocked" && c.Review.Verdict != "changes_requested") {
		return domain.Fail("authorization_changed", "rejected repair input does not match the exact task authority")
	}
	for _, oid := range []string{c.Commit, c.Main, c.Head} {
		if _, err := gitops.ParseOID(oid); err != nil {
			return domain.Fail("invalid_input", "repair input must retain fixed Git object identities")
		}
	}
	var source domain.Run
	if err := tx.Get("run", c.SourceRunID, &source); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Fail("invalid_input", "fixed repair source Run is missing")
		}
		return err
	}
	if source.TaskID != task.ID || source.Kind != "implement" || source.State != "succeeded" || !source.Quiescent || source.FinishedAt == nil ||
		source.AuthorizationVersion != c.AuthorizationVersion || source.PolicyVersion != c.PolicyVersion || source.Head != c.Head ||
		source.GoalVersion != task.GoalVersion || source.GoalAuthorizationVersion != task.GoalAuthorizationVersion ||
		source.BaselineVersion != task.BaselineVersion || source.BaselineDigest != task.BaselineDigest ||
		!maps.Equal(source.RequirementRevisions, task.RequirementRevisions) {
		return domain.Fail("conflict", "repair input requires an authentic successful stopped source Run")
	}
	return nil
}

func implementationRepairCandidate(tx *store.Tx, task domain.Task, project domain.Project) (*domain.Candidate, error) {
	previous, err := store.List[domain.Candidate](tx, "candidate")
	if err != nil {
		return nil, err
	}
	var latest *domain.Candidate
	for _, old := range previous {
		if old.TaskID == task.ID && old.State == "rejected" && old.AuthorizationVersion == task.AuthorizationVersion &&
			(latest == nil || old.CreatedAt.After(latest.CreatedAt)) {
			copy := old
			latest = &copy
		}
	}
	if latest != nil {
		return latest, nil
	}
	var revision taskAcceptanceRevision
	if err = tx.Get("task_acceptance_input", task.ID, &revision); errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if revision.AuthorizationVersion != task.AuthorizationVersion {
		return nil, nil // A later scope/authority change does not inherit this input grant.
	}
	before := revision.Before
	if before.ID != task.ID || before.GoalVersion != task.GoalVersion || before.GoalAuthorizationVersion != task.GoalAuthorizationVersion ||
		before.BaselineVersion != task.BaselineVersion || before.BaselineDigest != task.BaselineDigest ||
		!sameStrings(before.AllowedPaths, task.AllowedPaths) || !sameStrings(before.RequirementIDs, task.RequirementIDs) ||
		!maps.Equal(before.RequirementRevisions, task.RequirementRevisions) || revision.Acceptance != task.Acceptance {
		return nil, domain.Fail("authorization_changed", "fixed repair input belongs to another task contract")
	}
	var candidate domain.Candidate
	if err = tx.Get("candidate", revision.CandidateID, &candidate); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Fail("invalid_input", "fixed repair candidate is missing")
		}
		return nil, err
	}
	if candidate.Commit != revision.Commit || candidate.Main != revision.Main || candidate.Head != revision.Head ||
		candidate.SourceRunID != revision.SourceRunID || candidate.PolicyVersion != revision.PolicyVersion {
		return nil, domain.Fail("authorization_changed", "fixed repair source changed after the Owner revision")
	}
	if err = rejectedRepairSource(tx, before, project, candidate); err != nil {
		return nil, err
	}
	return &candidate, nil
}

// A bad fixed input holds this task visibly without starving unrelated work.
func repairInputForClaim(s *Service, tx *store.Tx, actor Actor, task *domain.Task, project domain.Project) (*domain.Candidate, bool, error) {
	candidate, err := implementationRepairCandidate(tx, *task, project)
	if err == nil {
		return candidate, false, nil
	}
	var problem *domain.Error
	if !errors.As(err, &problem) {
		return nil, false, err
	}
	if err = s.blockTaskContext(tx, actor, task, err); err != nil {
		return nil, false, err
	}
	return nil, true, nil
}
