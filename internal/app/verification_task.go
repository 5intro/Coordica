package app

import (
	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

// Construct the normal finite task using the same admitted baseline, goal and
// requirement bindings as execution. Catalog pins are persisted by the caller
// in that transaction; they are not added to every runtime snapshot.
func verificationTask(tx *store.Tx, s *Service, in domain.VerificationRequest, suite domain.TestSuite) (domain.Task, error) {
	commit, err := gitops.ParseOID(in.Commit)
	if err != nil {
		return domain.Task{}, domain.Fail("invalid_input", "full fixed commit required")
	}
	t := domain.Task{ID: ID("task_"), Version: 1, Title: "验证 · " + suite.Name,
		Description: suite.Description, Acceptance: "Every configured check passes on the pinned commit with the process tree stopped",
		RequirementIDs: in.RequirementIDs, Tool: "program", State: "ready", ApprovedVersion: 1,
		AuthorizationVersion: 1, BudgetSeconds: in.BudgetSeconds, MaxRunSeconds: in.MaxRunSeconds,
		MaxRetries: in.MaxRetries, CreatedAt: s.Now(),
		Verification: &domain.VerificationSpec{Suite: suite, Commit: string(commit), Digest: suiteDigest(suite)}}
	if len(t.RequirementIDs) == 0 {
		t.RequirementIDs = suite.RequirementIDs
	}
	baseline, err := initialization(tx)
	if err != nil {
		return t, err
	}
	goal, err := goalDocument(tx)
	if err != nil {
		return t, err
	}
	roots := baseline.ApprovedBaseline.AllowedRoots
	if goal.Version != 0 {
		roots = goal.AllowedRoots
	}
	for _, root := range roots {
		path := root + "/**"
		if root == "." {
			path = "**"
		}
		t.AllowedPaths = append(t.AllowedPaths, path)
	}
	t.RequirementRevisions, err = requirementBindings(tx, t.RequirementIDs)
	if err != nil {
		return t, err
	}
	if t.Description == "" {
		t.Description = suite.Name
	}
	if err := domain.ValidateTask(t); err != nil {
		return t, err
	}
	return t, goalGate(tx, &t, true)
}
