package app

import (
	"context"
	"testing"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Existing execution tests start with an explicit approved normative baseline;
// production has no fixture flag, legacy auto-admission, or gate bypass.
func seedReadyBaseline(t *testing.T, s *Service) {
	t.Helper()
	if e := s.DB.Update(context.Background(), func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		b := domain.Baseline{Requirements: "Fixture requirements", Acceptance: "Fixture acceptance", Architecture: "Fixture architecture", Modules: "Fixture responsibilities", Conventions: "Fixture interfaces", Testing: "Fixture testing", Functionality: "Fixture functionality", Risks: "Fixture risks", AllowedRoots: []string{"."}, Checks: p.Checks, ProtectedPaths: p.ProtectedPaths}
		v := domain.Initialization{Version: 1, Status: "initialized", Kind: "new", PolicyDigest: initializationPolicy(p), AvailablePaths: []string{}, CoveredPaths: []string{}, Omissions: []string{}, Feedback: []domain.InitializationFeedback{}, Result: &domain.InitializationResult{InitializationVersion: 1, Ready: true, InspectedPaths: []string{}, Facts: []domain.AnalysisEvidence{}, Suggestions: []string{}, Questions: []domain.InitializationQuestion{}, Baseline: b}, ApprovedVersion: 1, ApprovedBaseline: &b}
		v.Digest = initializationDigest(v)
		v.ApprovedDigest = v.Digest
		if e = tx.Put("initialization", "current", v); e != nil {
			return e
		}
		p.Paused = false
		return tx.Put("project", p.ID, p)
	}); e != nil {
		t.Fatal(e)
	}
}

func seedCurrentPolicyBaseline(t *testing.T, s *Service) {
	t.Helper()
	seedReadyBaseline(t, s)
	if e := s.DB.Update(context.Background(), func(tx *store.Tx) error {
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		for _, task := range tasks {
			task.BaselineVersion = v.Version
			task.BaselineDigest = v.Digest
			if e = tx.Put("task", task.ID, task); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
