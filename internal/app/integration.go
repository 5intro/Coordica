package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/process"
	"coordica/internal/store"
)

// Integrator is the single protected-branch executor. Git operations remain
// separate from transactions; every resulting fact is revalidated on commit.
type Integrator struct {
	Service *Service
	Git     *gitops.Client
	mu      sync.Mutex
}

func (w *Integrator) repo() string { return filepath.Join(w.Service.Dir, "repository") }
func (w *Integrator) Base(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, e := w.Service.Project(ctx)
	if e != nil {
		return "", e
	}
	if _, e = os.Stat(filepath.Join(w.repo(), ".git")); errors.Is(e, os.ErrNotExist) {
		if e = w.Git.Clone(ctx, p.RepositoryURL, w.repo()); e != nil {
			return "", e
		}
	}
	h, e := w.Git.Fetch(ctx, w.repo(), p.RepositoryURL, "refs/heads/"+p.TargetBranch)
	return string(h), e
}
func (w *Integrator) InputBundle(ctx context.Context, r domain.Run) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	path := filepath.Join(w.Service.Dir, "inputs", r.ID+".bundle")
	if _, e := os.Stat(path); e == nil {
		return path, nil
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return "", e
	}
	_, e := w.Git.CreateSnapshotBundle(ctx, w.repo(), gitops.OID(r.Base), path)
	return path, e
}
func (w *Integrator) Tick(ctx context.Context) error {
	s := w.Service
	var candidates []domain.Candidate
	var intents []domain.Intent
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		var e error
		candidates, e = store.List[domain.Candidate](tx, "candidate")
		if e != nil {
			return e
		}
		intents, e = store.List[domain.Intent](tx, "intent")
		return e
	})
	if e != nil {
		return e
	}
	for _, in := range intents {
		if in.State == "dispatching" || in.State == "unknown" {
			return w.reconcile(ctx, in)
		}
	}
	for _, c := range candidates {
		eligible, e := w.eligible(ctx, c)
		if e != nil {
			return e
		}
		if !eligible {
			continue
		}
		switch c.State {
		case "accepted":
			return w.prepare(ctx, c)
		case "validating":
			return w.validate(ctx, c)
		case "ready_to_land":
			return w.land(ctx, c)
		}
	}
	return nil
}
func (w *Integrator) eligible(ctx context.Context, c domain.Candidate) (bool, error) {
	var ok bool
	e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		ok = candidateAuthority(tx, t, p, c) && t.UsedSeconds < t.BudgetSeconds && p.UsedSeconds < p.TotalBudgetSeconds
		return nil
	})
	return ok, e
}
func (w *Integrator) prepare(ctx context.Context, c domain.Candidate) error {
	s := w.Service
	var t domain.Task
	var p domain.Project
	var material domain.Artifact
	var goalScope *gitops.Scope
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		var e error
		p, e = getProject(tx)
		if e != nil {
			return e
		}
		if e = goalGate(tx, &t, false); e != nil {
			return e
		}
		goalScope, e = currentGoalScope(tx)
		if e != nil {
			return e
		}
		return tx.Get("candidate_artifact", c.ID, &material)
	})
	if e != nil {
		return e
	}
	main, e := w.Base(ctx)
	if e != nil {
		return w.reject(ctx, c, e, false)
	}
	scope, e := gitops.NewScope(t.AllowedPaths, p.ProtectedPaths)
	if e != nil {
		return w.reject(ctx, c, e, false)
	}
	w.mu.Lock()
	imported, importErr := w.Git.ImportBundle(ctx, w.repo(), filepath.Join(s.Dir, "artifacts", material.ID), gitops.OID(c.Base), gitops.OID(c.Head))
	e = importErr
	if e == nil && (imported.SHA256 != material.SHA256 || imported.Bytes != material.Size) {
		e = errors.New("代码材料摘要与持久记录不一致")
	}
	if e == nil {
		var diff gitops.DiffResult
		diff, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(c.Base), gitops.OID(c.Head), scope)
		c.Protected = len(diff.Scope.Protected) > 0
		if e == nil && goalScope != nil {
			_, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(c.Base), gitops.OID(c.Head), goalScope)
		}
	}
	if e != nil {
		w.mu.Unlock()
		return w.reject(ctx, c, e, false)
	}
	workspace := filepath.Join(s.Dir, "candidates", c.ID, ID("build_"))
	candidate, e := w.Git.BuildCandidate(ctx, w.repo(), gitops.OID(main), gitops.OID(c.Head), workspace, gitops.Identity{})
	if e == nil {
		var diff gitops.DiffResult
		diff, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(main), candidate.Commit, scope)
		c.Protected = c.Protected || len(diff.Scope.Protected) > 0
		if e == nil && goalScope != nil {
			_, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(main), candidate.Commit, goalScope)
		}
	}
	w.mu.Unlock()
	if e != nil {
		return w.reject(ctx, c, e, errors.Is(e, gitops.ErrConflict))
	}
	c.Main = main
	c.Commit = string(candidate.Commit)
	c.State = "reviewing"
	c.Version++
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		if current.State != "accepted" {
			return nil
		}
		var task domain.Task
		if e := tx.Get("task", c.TaskID, &task); e != nil {
			return e
		}
		latestProject, e := getProject(tx)
		if e != nil {
			return e
		}
		if !candidateAuthority(tx, task, latestProject, c) {
			return nil
		}
		if e := tx.Put("candidate", c.ID, c); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.prepared", c.ID, c.Commit)
	})
}
func (w *Integrator) reject(ctx context.Context, c domain.Candidate, cause error, repairable bool) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		if t.CurrentCandidateID != c.ID {
			return nil
		}
		current.State = "rejected"
		current.Detail = errDetail(cause)
		current.Version++
		project, e := getProject(tx)
		if e != nil {
			return e
		}
		if !project.Paused && t.AuthorizationVersion == current.AuthorizationVersion && project.PolicyVersion == current.PolicyVersion {
			queueRepair(&t, &current, repairable)
		} else if project.Paused && t.HoldReason == "" && t.State != "cancelled" && t.State != "integrated" {
			t.HoldReason = "paused"
		}
		t.Version++
		if e := tx.Put("candidate", current.ID, current); e != nil {
			return e
		}
		if e := tx.Put("task", t.ID, t); e != nil {
			return e
		}
		if e := s.taskDecision(tx, t, current.Detail); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.rejected", c.ID, current.Detail)
	})
}
func (w *Integrator) validate(ctx context.Context, c domain.Candidate) error {
	s := w.Service
	var r domain.Run
	var p domain.Project
	e := s.DB.Update(ctx, func(tx *store.Tx) error {
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		var e error
		p, e = getProject(tx)
		if e != nil {
			return e
		}
		var latest domain.Candidate
		if e = tx.Get("candidate", c.ID, &latest); e != nil {
			return e
		}
		if goalGate(tx, &t, false) != nil || latest.State != "validating" || latest.Commit != c.Commit || t.State != "awaiting_integration" || t.AuthorizationVersion != latest.AuthorizationVersion || p.PolicyVersion != latest.PolicyVersion || p.Paused || t.HoldReason != "" || t.CurrentRunID != "" || t.CurrentCandidateID != c.ID {
			return domain.Fail("conflict", "候选暂不可验证")
		}
		c = latest
		if c.Review == nil || c.Review.Verdict != "approve" || c.Review.Candidate != c.Commit {
			return domain.Fail("conflict", "缺少固定候选独立评审")
		}
		grant := limit(t.MaxRunSeconds, t.BudgetSeconds-t.UsedSeconds-t.ReservedSeconds, p.TotalBudgetSeconds-p.UsedSeconds-p.ReservedSeconds)
		if t.GoalVersion != 0 {
			var d domain.PlanningDocument
			if e = tx.Get("planning_document", "current", &d); e != nil {
				return e
			}
			grant = limit(grant, d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds)
		}
		if grant <= 0 {
			return domain.Fail("budget", "验证预算不足")
		}
		now := s.Now()
		t.Generation++
		r = domain.Run{ID: ID("check_"), Version: 1, TaskID: t.ID, NodeID: "center", Kind: "validation", Tool: "program", State: "running", Generation: t.Generation, AuthorizationVersion: t.AuthorizationVersion, PolicyVersion: p.PolicyVersion, LeaseUntil: now.Add(time.Duration(grant) * time.Second), Deadline: now.Add(time.Duration(grant) * time.Second), GrantSeconds: grant, Base: c.Commit, CandidateID: c.ID, StartedAt: now}
		r.GoalVersion = t.GoalVersion
		r.GoalAuthorizationVersion = t.GoalAuthorizationVersion
		if e = reserveGoal(tx, t, grant); e != nil {
			return e
		}
		t.CurrentRunID = r.ID
		t.ReservedSeconds += grant
		p.ReservedSeconds += grant
		t.Version++
		p.Version++
		if e = tx.Put("run", r.ID, r); e != nil {
			return e
		}
		if e = tx.Put("task", t.ID, t); e != nil {
			return e
		}
		return tx.Put("project", p.ID, p)
	})
	if e != nil {
		return e
	}
	work := filepath.Join(s.Dir, "validation", r.ID)
	var results []domain.CheckResult
	quiescent := true
	runCtx, cancel := context.WithDeadline(ctx, r.Deadline)
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				var stop bool
				_ = s.DB.View(runCtx, func(tx *store.Tx) error {
					var current domain.Run
					if e := tx.Get("run", r.ID, &current); e != nil {
						stop = true
						return e
					}
					project, e := getProject(tx)
					stop = e != nil || project.Paused || current.State != "running"
					return nil
				})
				if stop {
					cancel()
					return
				}
			}
		}
	}()
	w.mu.Lock()
	e = w.Git.Clone(runCtx, w.repo(), work)
	if e == nil {
		e = w.Git.Checkout(runCtx, work, gitops.OID(c.Commit))
	}
	w.mu.Unlock()
	if e == nil {
		e = w.Git.VerifyClean(runCtx, work, gitops.OID(c.Commit))
	}
	if errors.Is(e, process.ErrTreeNotStopped) {
		quiescent = false
	}
	if e == nil {
		for _, check := range p.Checks {
			start := s.Now()
			checkCtx, checkCancel := context.WithTimeout(runCtx, time.Duration(check.TimeoutSeconds)*time.Second)
			result := domain.CheckResult{Name: check.Name, ExitCode: -1}
			session, execErr := process.Start(checkCtx, process.Spec{Executable: check.Command, Args: check.Args, Dir: work, Env: validationEnv(), GracePeriod: 2 * time.Second, DrainTimeout: 2 * time.Second})
			if execErr == nil {
				var output strings.Builder
				for event := range session.Output() {
					if output.Len() < 256<<10 {
						output.Write(event.Line)
						output.WriteByte('\n')
					}
				}
				pr, waitErr := session.Wait()
				quiescent = quiescent && pr.TreeStopped
				result.ExitCode = pr.ExitCode
				result.Output = output.String()
				result.Passed = waitErr == nil && pr.ExitCode == 0 && pr.TreeStopped
				execErr = waitErr
			} else {
				result.Output = errDetail(execErr)
			}
			checkCancel()
			result.DurationSeconds = int64(s.Now().Sub(start) / time.Second)
			results = append(results, result)
			if !result.Passed {
				e = fmt.Errorf("check %s failed: %v", check.Name, execErr)
				break
			}
		}
	}
	if e == nil {
		e = w.Git.VerifyClean(runCtx, work, gitops.OID(c.Commit))
		if errors.Is(e, process.ErrTreeNotStopped) {
			quiescent = false
		}
	}
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finalCancel()
	return w.finishValidation(finalCtx, r, c, results, e, quiescent, ctx.Err() != nil)
}

// finishValidation records a process outcome only for its current execution.
// A late validator cannot settle a replacement run or change its fixed candidate.
func (w *Integrator) finishValidation(ctx context.Context, r domain.Run, c domain.Candidate, results []domain.CheckResult, validationErr error, quiescent, interrupted bool) error {
	s := w.Service
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Run
		if err := tx.Get("run", r.ID, &current); err != nil {
			return err
		}
		if !active(current) && current.State != "lost" {
			return nil
		}
		var t domain.Task
		if err := tx.Get("task", r.TaskID, &t); err != nil {
			return err
		}
		project, err := getProject(tx)
		if err != nil {
			return err
		}
		owns := t.CurrentRunID == current.ID && t.Generation == current.Generation
		valid := grantedGoalAuthority(tx, &t, current) && owns && current.State == "running" && s.Now().Before(current.LeaseUntil) && s.Now().Before(current.Deadline) && current.GrantSeconds > 0 && !interrupted && !project.Paused && t.HoldReason == "" && t.State == "awaiting_integration" && t.CurrentCandidateID == c.ID && current.AuthorizationVersion == t.AuthorizationVersion && current.PolicyVersion == project.PolicyVersion
		var cand domain.Candidate
		if owns {
			if err = tx.Get("candidate", c.ID, &cand); err != nil {
				return err
			}
			valid = valid && quiescent && current.CandidateID == c.ID && c.Commit == current.Base && cand.State == "validating" && cand.Commit == current.Base && cand.AuthorizationVersion == t.AuthorizationVersion && cand.PolicyVersion == project.PolicyVersion
		}
		if owns && active(current) {
			elapsed := int64(0)
			if !quiescent || !s.Now().Before(current.LeaseUntil) {
				elapsed = current.GrantSeconds
			}
			before := current.ElapsedSeconds
			s.settle(&project, &t, &current, elapsed, true)
			if err = settleGoal(tx, t, current.ElapsedSeconds-before, current.GrantSeconds-current.ElapsedSeconds); err != nil {
				return err
			}
		}
		current.Quiescent = quiescent
		current.Version++
		now := s.Now()
		current.FinishedAt = &now
		current.State = "succeeded"
		if validationErr != nil {
			current.State = "failed"
			current.Detail = errDetail(validationErr)
		}
		if !valid {
			current.State = "stopped"
		}
		if !quiescent {
			current.State = "lost"
		}
		if err = tx.Put("run", current.ID, current); err != nil {
			return err
		}
		if !owns {
			return s.audit(tx, Actor{ID: "center"}, "run.late_validation", current.ID, current.State)
		}
		if valid {
			t.CurrentRunID = ""
			cand.Checks = results
			cand.Version++
			if validationErr == nil && len(results) == len(project.Checks) && len(results) > 0 {
				cand.State = "ready_to_land"
			} else {
				cand.State = "rejected"
				cand.Detail = errDetail(validationErr)
				queueRepair(&t, &cand, true)
			}
			if err = tx.Put("candidate", cand.ID, cand); err != nil {
				return err
			}
		} else {
			if t.HoldReason == "" && t.State != "cancelled" && t.State != "integrated" {
				t.HoldReason = "offline"
			}
			if _, err = s.recoverStopped(tx, project, &t, current); err != nil {
				return err
			}
		}
		if (t.UsedSeconds >= t.BudgetSeconds || project.UsedSeconds >= project.TotalBudgetSeconds) && (t.HoldReason == "" || t.HoldReason == "offline") && t.State != "cancelled" && t.State != "integrated" {
			t.HoldReason = "budget"
		}
		t.Version++
		if err = tx.Put("run", current.ID, current); err != nil {
			return err
		}
		if err = tx.Put("task", t.ID, t); err != nil {
			return err
		}
		if err = tx.Put("project", project.ID, project); err != nil {
			return err
		}
		if t.State == "cancelled" && quiescent {
			if err = releaseResources(tx, t.ID); err != nil {
				return err
			}
		}
		if err = s.taskDecision(tx, t, current.Detail); err != nil {
			return err
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.validated", c.ID, fmt.Sprintf("%s; accepted=%t", current.State, valid))
	})
}
func validationEnv() []string {
	var out []string
	for _, v := range os.Environ() {
		name := strings.SplitN(v, "=", 2)[0]
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "DEEPSEEK_API_KEY", "SSH_AUTH_SOCK":
			continue
		}
		if strings.HasPrefix(name, "COORDICA_") {
			continue
		}
		out = append(out, v)
	}
	return out
}
func (w *Integrator) land(ctx context.Context, c domain.Candidate) error {
	s := w.Service
	p, e := s.Project(ctx)
	if e != nil {
		return e
	}
	actual, e := w.Git.Query(ctx, p.RepositoryURL, "refs/heads/"+p.TargetBranch)
	if e != nil {
		return e
	}
	if string(actual) != c.Main {
		return w.invalidate(ctx, c, "主线已变化，必须重建候选")
	}
	var goalScope *gitops.Scope
	var goalVersion int64
	e = s.DB.View(ctx, func(tx *store.Tx) error {
		var task domain.Task
		if err := tx.Get("task", c.TaskID, &task); err != nil {
			return err
		}
		if err := goalGate(tx, &task, false); err != nil {
			return err
		}
		var err error
		goalScope, err = currentGoalScope(tx)
		if err != nil {
			return err
		}
		var d domain.PlanningDocument
		if err = tx.Get("planning_document", "current", &d); err == nil {
			goalVersion = d.Version
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return nil
	})
	if e != nil {
		return e
	}
	if goalScope != nil {
		w.mu.Lock()
		_, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(c.Base), gitops.OID(c.Head), goalScope)
		if e == nil {
			_, e = w.Git.CheckScope(ctx, w.repo(), gitops.OID(c.Main), gitops.OID(c.Commit), goalScope)
		}
		w.mu.Unlock()
		if e != nil {
			return w.reject(ctx, c, e, false)
		}
	}
	var in domain.Intent
	e = s.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		project, e := getProject(tx)
		if e != nil {
			return e
		}
		if goalVersion != 0 {
			var d domain.PlanningDocument
			if e := tx.Get("planning_document", "current", &d); e != nil {
				return e
			}
			if d.Version != goalVersion {
				return domain.Fail("version_conflict", "goal changed during final scope check")
			}
		}
		if goalGate(tx, &t, false) != nil || current.State != "ready_to_land" || t.CurrentCandidateID != c.ID || project.Paused || t.HoldReason != "" || t.State != "awaiting_integration" || t.AuthorizationVersion != c.AuthorizationVersion || project.PolicyVersion != c.PolicyVersion || t.UsedSeconds >= t.BudgetSeconds || project.UsedSeconds >= project.TotalBudgetSeconds {
			return domain.Fail("conflict", "放行条件已变化")
		}
		if current.Protected && current.ApprovedCommit != current.Commit {
			return nil
		}
		if current.Review == nil || current.Review.Candidate != current.Commit || current.Review.Verdict != "approve" || len(current.Checks) != len(project.Checks) {
			return domain.Fail("conflict", "缺少完整固定候选证据")
		}
		for _, ch := range current.Checks {
			if !ch.Passed {
				return domain.Fail("conflict", "检查未通过")
			}
		}
		in = domain.Intent{ID: ID("op_"), CandidateID: c.ID, Expected: c.Main, Desired: c.Commit, State: "dispatching", CreatedAt: s.Now()}
		current.State = "landing"
		current.Version++
		if e = tx.Put("intent", in.ID, in); e != nil {
			return e
		}
		if e = tx.Put("candidate", c.ID, current); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.dispatching", c.ID, in.ID)
	})
	if e != nil || in.ID == "" {
		return e
	}
	w.mu.Lock()
	_, e = w.Git.Publish(ctx, w.repo(), p.RepositoryURL, "refs/heads/"+p.TargetBranch, gitops.OID(in.Expected), gitops.OID(in.Desired))
	w.mu.Unlock()
	if e != nil {
		_ = s.DB.Update(ctx, func(tx *store.Tx) error {
			in.State = "unknown"
			in.Detail = errDetail(e)
			return tx.Put("intent", in.ID, in)
		})
	}
	return w.reconcile(ctx, in)
}
func (w *Integrator) reconcile(ctx context.Context, in domain.Intent) error {
	s := w.Service
	p, e := s.Project(ctx)
	if e != nil {
		return e
	}
	actual, e := w.Git.Query(ctx, p.RepositoryURL, "refs/heads/"+p.TargetBranch)
	if e != nil {
		return e
	}
	contains := string(actual) == in.Desired
	if !contains && string(actual) != in.Expected {
		// Querying a descendant is evidence of this exact C being present,
		// not merely an equivalent tree. Fetch again to bind that evidence.
		latest, err := w.Base(ctx)
		if err != nil {
			return err
		}
		actual = gitops.OID(latest)
		contains, e = w.Git.IsAncestor(ctx, w.repo(), gitops.OID(in.Desired), actual)
		if e != nil {
			return e
		}
	}
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		var c domain.Candidate
		if e := tx.Get("candidate", in.CandidateID, &c); e != nil {
			return e
		}
		var t domain.Task
		if e := tx.Get("task", c.TaskID, &t); e != nil {
			return e
		}
		if contains {
			in.State = "completed"
			c.State = "landed"
			t.State = "integrated"
			t.HoldReason = ""
			if e := releaseResources(tx, t.ID); e != nil {
				return e
			}
		} else if string(actual) == in.Expected && t.Retries < t.MaxRetries {
			in.State = "not_applied"
			c.State = "ready_to_land"
			c.Detail = "已核对远端仍为预期旧提交；有限重试待派发"
			t.Retries++
		} else if string(actual) != in.Expected {
			in.State = "superseded"
			c.State = "accepted"
			c.Main, c.Commit, c.ApprovedCommit = "", "", ""
			c.Review, c.Checks = nil, nil
			c.Detail = "远端已变化且未包含候选；重新构建评审"
		} else {
			in.State = "needs_attention"
			c.State = "reconciling"
			c.Detail = "远端未确认目标提交；需要核对后重试"
			t.HoldReason = "human_decision"
		}
		c.Version++
		t.Version++
		if e := tx.Put("intent", in.ID, in); e != nil {
			return e
		}
		if e := tx.Put("candidate", c.ID, c); e != nil {
			return e
		}
		if e := tx.Put("task", t.ID, t); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "integration.reconciled", c.ID, string(actual))
	})
}
func (w *Integrator) invalidate(ctx context.Context, c domain.Candidate, reason string) error {
	return w.Service.DB.Update(ctx, func(tx *store.Tx) error {
		var current domain.Candidate
		if e := tx.Get("candidate", c.ID, &current); e != nil {
			return e
		}
		if current.State == "landing" || current.State == "landed" {
			return nil
		}
		current.State = "accepted"
		current.Commit = ""
		current.Main = ""
		current.Review = nil
		current.Checks = nil
		current.ApprovedCommit = ""
		current.Detail = reason
		current.Version++
		if e := tx.Put("candidate", current.ID, current); e != nil {
			return e
		}
		return w.Service.audit(tx, Actor{ID: "center"}, "candidate.invalidated", current.ID, reason)
	})
}
func (s *Service) ApproveCandidate(ctx context.Context, a Actor, key, id string, req domain.VersionRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "candidate.approve."+id, req, func(tx *store.Tx) (any, error) {
		var c domain.Candidate
		if e := tx.Get("candidate", id, &c); e != nil {
			return nil, e
		}
		if e := version(c.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if c.State != "ready_to_land" || c.Commit == "" {
			return nil, domain.Fail("conflict", "候选尚未完成评审与检查")
		}
		c.ApprovedCommit = c.Commit
		c.Version++
		if e := tx.Put("candidate", c.ID, c); e != nil {
			return nil, e
		}
		return c, s.audit(tx, a, "candidate.approved", c.ID, c.Commit)
	})
}
