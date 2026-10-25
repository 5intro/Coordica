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
	"coordica/internal/process/checkexec"
	"coordica/internal/store"
)

// Integrator is the single protected-branch executor. Git operations remain
// separate from transactions; every resulting fact is revalidated on commit.
type Integrator struct {
	Service      *Service
	Git          *gitops.Client
	PullRequests PullRequestHost
	mu           sync.Mutex
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
	// Revoking a stale remote approval is a safety action, including while
	// paused or draining. It precedes all new publication operations.
	if pending, e := w.reconcilePullReviewRevocations(ctx); e != nil || pending {
		return e
	}
	var barrier bool
	if e := s.DB.View(ctx, func(tx *store.Tx) error { var e error; barrier, e = unknownConsultation(tx); return e }); e != nil {
		return e
	}
	if barrier {
		return nil
	}
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
			if e = w.observePullRequest(ctx, c); e != nil {
				return e
			}
			continue
		}
		switch c.State {
		case "accepted":
			return w.prepare(ctx, c)
		case "prechecking", "validating":
			return w.validate(ctx, c)
		case "reviewing":
			if _, err := w.syncPullRequest(ctx, c, false); err != nil {
				return err
			}
		case "ready_to_land":
			p, err := s.Project(ctx)
			if err != nil {
				return err
			}
			if domain.GitHubPublication(p) {
				c, err = w.syncPullRequest(ctx, c, false)
				if err != nil {
					return err
				}
				if c.PullRequest == nil || (!c.PullRequest.MergeReady && c.PullRequest.State != "merged") {
					continue
				}
			}
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
		ok = candidateAuthority(tx, t, p, c) && t.UsedSeconds < t.BudgetSeconds
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
		if !candidateAuthority(tx, t, p, c) {
			return domain.Fail("authorization_changed", "candidate baseline or authority changed")
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
	plan, e := candidateCheckPlan(ctx, w, p, &c)
	if e != nil {
		return candidateCheckFailure(ctx, w, c, e)
	}
	c.CheckPlan = &plan
	c.State = "prechecking"
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
		if latestProject.TestPolicyVersion != c.CheckPlan.PolicyVersion {
			return nil
		}
		if e := tx.Put("candidate", c.ID, c); e != nil {
			return e
		}
		return s.audit(tx, Actor{ID: "center"}, "candidate.prepared", c.ID, c.Commit)
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
		if latest.BaselineVersion != t.BaselineVersion || latest.BaselineDigest != t.BaselineDigest || goalGate(tx, &t, false) != nil || (latest.State != "validating" && latest.State != "prechecking") || latest.Commit != c.Commit || t.State != "awaiting_integration" || t.AuthorizationVersion != latest.AuthorizationVersion || p.PolicyVersion != latest.PolicyVersion || p.Paused || t.HoldReason != "" || t.CurrentRunID != "" || t.CurrentCandidateID != c.ID {
			return domain.Fail("conflict", "候选暂不可验证")
		}
		c = latest
		if c.State != "prechecking" && (c.Review == nil || c.Review.Verdict != "approve" || c.Review.Candidate != c.Commit) {
			return domain.Fail("conflict", "缺少固定候选独立评审")
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
			return domain.Fail("budget", "验证预算不足")
		}
		now := s.Now()
		t.Generation++
		r = domain.Run{BaselineVersion: t.BaselineVersion, BaselineDigest: t.BaselineDigest, ID: ID("check_"), Version: 1, TaskID: t.ID, NodeID: "center", Kind: "validation", Tool: "program", State: "running", Generation: t.Generation, AuthorizationVersion: t.AuthorizationVersion, PolicyVersion: p.PolicyVersion, LeaseUntil: now.Add(time.Duration(grant) * time.Second), Deadline: now.Add(time.Duration(grant) * time.Second), GrantSeconds: grant, Base: c.Commit, CandidateID: c.ID, StartedAt: now}
		r.GoalVersion = t.GoalVersion
		if c.CheckPlan != nil {
			r.CheckPlanDigest = c.CheckPlan.Digest
		}
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
				err := s.DB.View(runCtx, func(tx *store.Tx) error {
					var current domain.Run
					if e := tx.Get("run", r.ID, &current); e != nil {
						stop = true
						return e
					}
					project, e := getProject(tx)
					stop = e != nil || project.Paused || current.State != "running"
					return nil
				})
				if err != nil || stop {
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
		results, quiescent, e = checkexec.Run(runCtx, work, candidateChecks(p, c), validationEnv())
	}
	if e == nil {
		outputs := []string{}
		if c.CheckPlan != nil {
			outputs = c.CheckPlan.OutputPaths
		}
		e = w.Git.VerifyCheckOutputs(runCtx, work, gitops.OID(c.Commit), outputs)
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
			valid = valid && cand.BaselineVersion == current.BaselineVersion && cand.BaselineDigest == current.BaselineDigest && quiescent && current.CandidateID == c.ID && c.Commit == current.Base && (cand.State == "validating" || cand.State == "prechecking") && cand.State == c.State && cand.Commit == current.Base && cand.AuthorizationVersion == t.AuthorizationVersion && cand.PolicyVersion == project.PolicyVersion
			if cand.CheckPlan != nil {
				valid = valid && cand.CheckPlan.PolicyVersion == project.TestPolicyVersion && cand.CheckPlan.Digest == current.CheckPlanDigest
			}
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
			if validationErr == nil && domain.ChecksPassed(candidateChecks(project, cand), results) {
				cand.CheckRunID = current.ID
				cand.State = "ready_to_land"
				if c.State == "prechecking" {
					cand.State = "reviewing"
				}
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
		if (t.UsedSeconds >= t.BudgetSeconds) && (t.HoldReason == "" || t.HoldReason == "offline") && t.State != "cancelled" && t.State != "integrated" {
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
	var task domain.Task
	var binding pullRequestBinding
	if e = s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("task", c.TaskID, &task); e != nil {
			return e
		}
		if e := goalGate(tx, &task, false); e != nil {
			return e
		}
		if !candidateAuthority(tx, task, p, c) {
			return domain.Fail("authorization_changed", "candidate baseline or authority changed")
		}
		if domain.GitHubPublication(p) {
			return tx.Get("pull_request", task.ID, &binding)
		}
		return nil
	}); e != nil {
		return e
	}
	// Check authority before any new I/O, then gate legacy ready candidates
	// too. Metrics are advisory; source integrity and execution authority remain mandatory.
	if err := checkCandidateQuality(ctx, w, &c); err != nil {
		return candidateQualityFailure(ctx, w, c, err)
	}
	actual, e := w.Git.Query(ctx, p.RepositoryURL, "refs/heads/"+p.TargetBranch)
	if e != nil {
		return e
	}
	alreadyMerged := domain.GitHubPublication(p) && c.PullRequest != nil && c.PullRequest.State == "merged" && c.PullRequest.Head == c.Commit && c.PullRequest.MergeCommit != ""
	var observed *domain.PullRequestStatus
	if string(actual) != c.Main && !alreadyMerged && domain.GitHubPublication(p) {
		// A remote merge can move main inside the normal sync backoff. Only
		// inspect the fixed PR here: no branch push, create or approval replay.
		if w.PullRequests == nil || c.PullRequest == nil || binding.Number != c.PullRequest.Number || binding.CandidateID != c.ID || binding.Desired != c.Commit {
			return w.recordPullError(ctx, c, "等待原 GitHub PR 的主线变化核对")
		}
		bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
		facts, err := w.PullRequests.Inspect(bounded, pullSpec(p, task, c, binding))
		cancel()
		if err != nil || facts.Number != binding.Number || facts.Branch != binding.Branch || facts.Head != c.Commit || facts.State == "merged" && facts.MergeCommit == "" {
			return w.recordPullError(ctx, c, "PR 主线变化核对失败；保留固定候选等待重试")
		}
		if facts.State == "merged" {
			facts.SyncedAt = s.Now()
			observed, alreadyMerged = &facts, true
		}
	}
	if string(actual) != c.Main && !alreadyMerged {
		return w.invalidate(ctx, c, "主线已变化，必须重建候选")
	}
	goalScope, goalVersion, e := landingScope(ctx, s, c.TaskID)
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
		if c.BaselineVersion != t.BaselineVersion || c.BaselineDigest != t.BaselineDigest || goalGate(tx, &t, false) != nil || current.State != "ready_to_land" || t.CurrentCandidateID != c.ID || project.Paused || t.HoldReason != "" || t.State != "awaiting_integration" || t.AuthorizationVersion != c.AuthorizationVersion || project.PolicyVersion != c.PolicyVersion || t.UsedSeconds >= t.BudgetSeconds {
			return domain.Fail("conflict", "放行条件已变化")
		}
		if current.Protected && current.ApprovedCommit != current.Commit {
			return nil
		}
		if domain.GitHubPublication(project) {
			if observed != nil {
				var bound pullRequestBinding
				if e := tx.Get("pull_request", t.ID, &bound); e != nil {
					return e
				}
				if current.Commit != c.Commit || bound.Number != observed.Number || bound.Branch != observed.Branch || bound.CandidateID != current.ID || bound.Desired != current.Commit {
					return domain.Fail("conflict", "PR 绑定在核对期间已变化")
				}
				current.PullRequest = observed
			}
			if current.PullRequest == nil || current.PullRequest.Head != current.Commit || current.PullRequest.State != "merged" && current.PullRequest.Base != current.Main || (!current.PullRequest.MergeReady && current.PullRequest.State != "merged") || w.PullRequests == nil {
				return nil
			}
		}
		blockingTests, e := blockingTestFailures(tx, current.Main, current.Commit)
		if e != nil {
			return e
		}
		if len(blockingTests) != 0 {
			return nil
		}
		if current.Review == nil || current.Review.Candidate != current.Commit || current.Review.Verdict != "approve" || !domain.ChecksPassed(candidateChecks(project, current), current.Checks) {
			return domain.Fail("conflict", "缺少完整固定候选证据")
		}
		if current.CheckPlan != nil {
			var checkRun domain.Run
			if current.CheckPlan.PolicyVersion != project.TestPolicyVersion || tx.Get("run", current.CheckRunID, &checkRun) != nil || checkRun.Kind != "validation" || checkRun.State != "succeeded" || !checkRun.Quiescent || checkRun.Base != current.Commit || checkRun.CandidateID != current.ID || checkRun.CheckPlanDigest != current.CheckPlan.Digest {
				return domain.Fail("conflict", "候选检查版本或证据已失效")
			}
		}
		for _, ch := range current.Checks {
			if !ch.Passed {
				return domain.Fail("conflict", "检查未通过")
			}
		}
		in = domain.Intent{ID: ID("op_"), CandidateID: c.ID, Expected: c.Main, Desired: c.Commit, State: "dispatching", CreatedAt: s.Now()}
		if domain.GitHubPublication(project) {
			in.PublicationMode = "github"
			in.PullRequestNumber = current.PullRequest.Number
		}
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
	if in.PublicationMode == "github" {
		if !alreadyMerged {
			var t domain.Task
			if e = s.DB.View(ctx, func(tx *store.Tx) error { return tx.Get("task", c.TaskID, &t) }); e == nil {
				spec := pullSpec(p, t, c, pullRequestBinding{Branch: c.PullRequest.Branch, Number: in.PullRequestNumber})
				_, e = w.PullRequests.Merge(ctx, spec)
			}
		}
	} else {
		w.mu.Lock()
		_, e = w.Git.Publish(ctx, w.repo(), p.RepositoryURL, "refs/heads/"+p.TargetBranch, gitops.OID(in.Expected), gitops.OID(in.Desired))
		w.mu.Unlock()
	}
	if errors.Is(e, domain.ErrPublicationNotReady) {
		return w.deferHostedMerge(ctx, in)
	}
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
	var hosted *domain.PullRequestStatus
	if contains && in.PublicationMode == "github" && w.PullRequests != nil && domain.GitHubPublication(p) {
		var c domain.Candidate
		var t domain.Task
		if e = s.DB.View(ctx, func(tx *store.Tx) error {
			if err := tx.Get("candidate", in.CandidateID, &c); err != nil {
				return err
			}
			return tx.Get("task", c.TaskID, &t)
		}); e != nil {
			return e
		}
		if c.PullRequest != nil {
			if facts, err := w.PullRequests.Inspect(ctx, pullSpec(p, t, c, pullRequestBinding{Branch: c.PullRequest.Branch, Number: in.PullRequestNumber})); err == nil && facts.State == "merged" && facts.Head == in.Desired && facts.Number == in.PullRequestNumber {
				facts.SyncedAt = s.Now()
				hosted = &facts
			}
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
		if contains && in.PublicationMode == "github" && hosted == nil {
			// Ancestry alone cannot acknowledge a hosted merge: another actor
			// may have landed the same commit through a different PR.
			in.State = "unknown"
			in.Detail = "等待原 GitHub PR 的已合并凭据；主线包含代码不能替代 PR 确认"
			c.State = "reconciling"
			c.Detail = in.Detail
		} else if contains {
			if hosted != nil {
				c.PullRequest = hosted
			}
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
		var task domain.Task
		if e := tx.Get("task", current.TaskID, &task); e != nil {
			return e
		}
		project, e := getProject(tx)
		if e != nil {
			return e
		}
		if current.State != "reviewing" && current.State != "ready_to_land" || current.Commit != c.Commit || !candidateAuthority(tx, task, project, current) {
			return nil
		}
		if e := queuePullReviewRevocation(tx, current); e != nil {
			return e
		}
		current.State = "accepted"
		current.Commit = ""
		current.Main = ""
		current.Review = nil
		current.Checks = nil
		current.CheckPlan = nil
		current.CheckRunID = ""
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
	return s.mutateWithCheck(ctx, a, key, "candidate.approve."+id, req, func(tx *store.Tx) error { return baselineGate(tx, nil, false) }, func(tx *store.Tx) (any, error) {
		var c domain.Candidate
		if e := tx.Get("candidate", id, &c); e != nil {
			return nil, e
		}
		if e := version(c.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		var task domain.Task
		if e := tx.Get("task", c.TaskID, &task); e != nil {
			return nil, e
		}
		if e := baselineGate(tx, &task, false); e != nil {
			return nil, e
		}
		if c.BaselineVersion != task.BaselineVersion || c.BaselineDigest != task.BaselineDigest {
			return nil, domain.Fail("baseline_changed", "候选基线已失效")
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
