package app

import (
	"context"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func candidateCheckPlan(ctx context.Context, w *Integrator, p domain.Project, c *domain.Candidate) (domain.CheckPlan, error) {
	if err := checkCandidateQuality(ctx, w, c); err != nil {
		return domain.CheckPlan{}, &qualityScanFailure{err}
	}
	var suites []domain.TestSuite
	err := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var err error
		suites, err = store.List[domain.TestSuite](tx, "test_suite")
		return err
	})
	if err != nil {
		return domain.CheckPlan{}, err
	}
	w.mu.Lock()
	changes, err := w.Git.DiffPaths(ctx, w.repo(), gitops.OID(c.Main), gitops.OID(c.Commit))
	w.mu.Unlock()
	if err != nil {
		return domain.CheckPlan{}, err
	}
	var paths []string
	for _, change := range changes {
		paths = append(paths, change.Paths()...)
	}
	plan, err := mergeCheckPlan(p, suites, paths, c.Protected)
	if err != nil {
		return plan, &checkPlanDefect{err}
	}
	return plan, nil
}

type qualityScanFailure struct{ error }
type checkPlanDefect struct{ error }

func (e *qualityScanFailure) Unwrap() error { return e.error }
func (e *checkPlanDefect) Unwrap() error    { return e.error }

func candidateCheckFailure(ctx context.Context, w *Integrator, c domain.Candidate, err error) error {
	var scan *qualityScanFailure
	if errors.As(err, &scan) {
		return candidateQualityFailure(ctx, w, c, scan.error)
	}
	var defect *checkPlanDefect
	if errors.As(err, &defect) {
		return w.reject(ctx, c, defect.error, true)
	}
	return err
}

// Keep the read snapshot separate from Git I/O; land rechecks this version in
// its final write transaction, after the expensive scope checks complete.
func landingScope(ctx context.Context, s *Service, taskID string) (*gitops.Scope, int64, error) {
	var scope *gitops.Scope
	var version int64
	err := s.DB.View(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", taskID, &task); err != nil {
			return err
		}
		if err := goalGate(tx, &task, false); err != nil {
			return err
		}
		var err error
		scope, err = currentGoalScope(tx)
		if err != nil {
			return err
		}
		var d domain.PlanningDocument
		if err = tx.Get("planning_document", "current", &d); err == nil {
			version = d.Version
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return nil
	})
	return scope, version, err
}
