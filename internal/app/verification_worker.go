package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
	"coordica/internal/process/checkexec"
	"coordica/internal/store"
)

// VerificationWorker consumes normal finite program tasks. Its execution loop
// is separate from integration so a long stability test cannot stall merges.
// This version executes on the center host, whose Owner owns suite commands.
type VerificationWorker struct{ Integration *Integrator }

func (w *VerificationWorker) claim(ctx context.Context) (*domain.Task, *domain.Run, error) {
	s := w.Integration.Service
	var task *domain.Task
	var run *domain.Run
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		if p.Paused {
			return nil
		}
		barrier, e := unknownConsultation(tx)
		if e != nil || barrier {
			return e
		}
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return e
		}
		for _, r := range runs {
			if r.Kind == "verification" && (active(r) || r.State == "lost" && !r.Quiescent) {
				return nil
			}
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		for _, t := range tasks {
			if t.Verification == nil || t.State != "ready" || t.ApprovedVersion == 0 || t.CurrentRunID != "" || t.HoldReason != "" {
				continue
			}
			if e := goalGate(tx, &t, false); e != nil {
				continue
			}
			if suiteDigest(t.Verification.Suite) != t.Verification.Digest || !domain.TestPlatformAllowed(t.Verification.Suite, runtime.GOOS) {
				continue
			}
			ready := true
			for _, id := range t.Dependencies {
				var dep domain.Task
				if e := tx.Get("task", id, &dep); e != nil {
					return e
				}
				if dep.State != "integrated" && dep.State != "verified" {
					ready = false
				}
			}
			if !ready {
				continue
			}
			grant := limit(t.MaxRunSeconds, t.BudgetSeconds-t.UsedSeconds-t.ReservedSeconds)
			if t.GoalVersion != 0 {
				var d domain.PlanningDocument
				if e = tx.Get("planning_document", "current", &d); e != nil {
					return e
				}
				grant = limit(grant, d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds)
			}
			if grant <= 0 {
				t.HoldReason = "budget"
				t.Version++
				if e = tx.Put("task", t.ID, t); e != nil {
					return e
				}
				continue
			}
			now := s.Now()
			t.Generation++
			r := domain.Run{ID: ID("test_"), Version: 1, TaskID: t.ID, NodeID: "center", Kind: "verification", Tool: "program", State: "running", Generation: t.Generation, AuthorizationVersion: t.AuthorizationVersion, PolicyVersion: p.PolicyVersion, BaselineVersion: t.BaselineVersion, BaselineDigest: t.BaselineDigest, GoalVersion: t.GoalVersion, GoalAuthorizationVersion: t.GoalAuthorizationVersion, GrantSeconds: grant, StartedAt: now, Deadline: now.Add(time.Duration(grant) * time.Second), LeaseUntil: now.Add(time.Duration(grant) * time.Second), Base: t.Verification.Commit, TestReport: &domain.TestReport{Commit: t.Verification.Commit, SuiteID: t.Verification.Suite.ID, SuiteVersion: t.Verification.Suite.Version, Digest: t.Verification.Digest, Platform: runtime.GOOS}}
			if e = reserveGoal(tx, t, grant); e != nil {
				return e
			}
			t.State, t.CurrentRunID = "executing", r.ID
			t.ReservedSeconds += grant
			t.Version++
			p.ReservedSeconds += grant
			p.Version++
			if e = tx.Put("run", r.ID, r); e != nil {
				return e
			}
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return e
			}
			if e = s.audit(tx, Actor{ID: "center"}, "run.claimed", r.ID, "verification:program"); e != nil {
				return e
			}
			task, run = &t, &r
			return nil
		}
		return nil
	})
	return task, run, e
}

func (w *VerificationWorker) Tick(ctx context.Context) error {
	t, r, e := w.claim(ctx)
	if e != nil || r == nil {
		return e
	}
	i := w.Integration
	s := i.Service
	runCtx, cancel := context.WithDeadline(ctx, r.Deadline)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				allowed := false
				err := s.DB.View(runCtx, func(tx *store.Tx) error {
					var current domain.Run
					var task domain.Task
					if e := tx.Get("run", r.ID, &current); e != nil {
						return e
					}
					if e := tx.Get("task", r.TaskID, &task); e != nil {
						return e
					}
					p, e := getProject(tx)
					allowed = e == nil && !p.Paused && current.State == "running" && task.CurrentRunID == r.ID && task.Generation == r.Generation && task.HoldReason == "" && p.PolicyVersion == r.PolicyVersion && grantedGoalAuthority(tx, &task, current)
					return e
				})
				if err != nil || !allowed {
					cancel()
					return
				}
			}
		}
	}()
	_, e = i.Base(runCtx)
	work := filepath.Join(s.Dir, "verification", r.ID)
	quiet := !errors.Is(e, process.ErrTreeNotStopped)
	if e == nil {
		i.mu.Lock()
		e = i.Git.Clone(runCtx, i.repo(), work)
		i.mu.Unlock()
		if e == nil {
			e = i.Git.Checkout(runCtx, work, gitops.OID(r.Base))
		}
		quiet = quiet && !errors.Is(e, process.ErrTreeNotStopped)
	}
	var results []domain.CheckResult
	if e == nil {
		e = i.Git.VerifyClean(runCtx, work, gitops.OID(r.Base))
		quiet = quiet && !errors.Is(e, process.ErrTreeNotStopped)
	}
	if e == nil {
		results, quiet, e = checkexec.Run(runCtx, work, t.Verification.Suite.Checks, validationEnv())
	}
	if e == nil {
		e = i.Git.VerifyClean(runCtx, work, gitops.OID(r.Base))
		quiet = quiet && !errors.Is(e, process.ErrTreeNotStopped)
	}
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finalCancel()
	return w.finish(finalCtx, *r, results, e, quiet, ctx.Err() != nil)
}

func (w *VerificationWorker) finish(ctx context.Context, r domain.Run, results []domain.CheckResult, runErr error, quiet, interrupted bool) error {
	s := w.Integration.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Run
		if e := tx.Get("run", r.ID, &current); e != nil {
			return e
		}
		if !active(current) && current.State != "lost" {
			return nil
		}
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		owns := t.CurrentRunID == r.ID && t.Generation == r.Generation
		valid := owns && current.State == "running" && quiet && !interrupted && !p.Paused && t.State == "executing" && t.HoldReason == "" && t.AuthorizationVersion == r.AuthorizationVersion && p.PolicyVersion == r.PolicyVersion && s.Now().Before(current.LeaseUntil) && s.Now().Before(current.Deadline) && grantedGoalAuthority(tx, &t, current) && t.Verification != nil && t.Verification.Commit == r.Base && t.Verification.Digest == r.TestReport.Digest
		if owns && active(current) {
			elapsed := int64(0)
			if !quiet || !s.Now().Before(current.LeaseUntil) {
				elapsed = current.GrantSeconds
			}
			before := current.ElapsedSeconds
			s.settle(&p, &t, &current, elapsed, true)
			if e = settleGoal(tx, t, current.ElapsedSeconds-before, current.GrantSeconds-current.ElapsedSeconds); e != nil {
				return e
			}
		}
		current.TestReport = r.TestReport
		current.TestReport.Checks = results
		current.Quiescent, current.State = quiet, "stopped"
		current.Version++
		now := s.Now()
		current.FinishedAt = &now
		if runErr != nil {
			current.Detail = errDetail(runErr)
		}
		if !quiet {
			current.State = "lost"
		}
		if owns && valid {
			t.CurrentRunID = ""
			if runErr == nil && domain.ChecksPassed(t.Verification.Suite.Checks, results) {
				current.State, t.State = "succeeded", "verified"
			} else {
				current.State, t.HoldReason = "failed", "tests_failed"
			}
		} else if owns {
			if t.HoldReason == "" && !domain.TaskFinished(t) {
				t.HoldReason = "offline"
			}
			if _, e = s.recoverStopped(tx, p, &t, current); e != nil {
				return e
			}
		}
		if e = tx.Put("run", current.ID, current); e != nil {
			return e
		}
		if owns {
			t.Version++
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return e
			}
			if domain.TaskFinished(t) && quiet {
				if e = releaseResources(tx, t.ID); e != nil {
					return e
				}
			}
		}
		return s.audit(tx, Actor{ID: "center"}, "tests.completed", r.ID, fmt.Sprintf("%s; accepted=%t; historical=%t", current.State, valid, !owns))
	})
}
