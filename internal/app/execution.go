package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func available(caps []domain.Capability, tool string) bool {
	for _, c := range caps {
		if c.Tool == tool && c.Available && c.Structured && c.Cancel {
			return true
		}
	}
	return false
}
func (s *Service) Claim(ctx context.Context, a Actor, key string, req domain.ClaimRequest, base string) (json.RawMessage, error) {
	if a.Role != "node" {
		return nil, domain.Fail("forbidden", "只有Runner可领取执行")
	}
	if err := domain.ValidateCapabilities(req.Capabilities); err != nil {
		return nil, err
	}
	return s.mutateWithCheck(ctx, a, key, "run.claim", req, func(tx *store.Tx) error {
		if _, e := nodeConsoleActor(tx, a); e != nil {
			return e
		}
		return baselineGate(tx, nil, false)
	}, func(tx *store.Tx) (any, error) {
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		var n domain.Node
		if e = tx.Get("node", a.NodeID, &n); e != nil {
			return nil, e
		}
		if n.Revoked {
			return nil, domain.Fail("forbidden", "节点已撤销")
		}
		n.LastSeen = s.Now()
		n.Capabilities = req.Capabilities
		if e = tx.Put("node", n.ID, n); e != nil {
			return nil, e
		}
		blocked, e := unknownConsultation(tx)
		if e != nil {
			return nil, e
		}
		if blocked {
			return nil, nil
		}
		if p.Paused || n.Draining || n.DisabledNewWork {
			return nil, nil
		}
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return nil, e
		}
		count := 0
		for _, r := range runs {
			if r.NodeID == n.ID && (active(r) || (r.State == "lost" && !r.Quiescent)) {
				count++
			}
		}
		if count >= n.Capacity {
			return nil, nil
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return nil, e
		}
		for _, t := range tasks {
			if t.Verification != nil {
				continue
			} // program verification is claimed by the center verifier
			if t.ApprovedVersion == 0 || t.State == "cancelled" || t.State == "integrated" || t.CurrentRunID != "" || t.HoldReason != "" {
				continue
			}
			kind := "implement"
			tool := t.Tool
			runBase := base
			var candidate *domain.Candidate
			if t.State == "awaiting_integration" {
				var c domain.Candidate
				if t.CurrentCandidateID == "" {
					continue
				}
				if e = tx.Get("candidate", t.CurrentCandidateID, &c); e != nil {
					return nil, e
				}
				if c.State != "reviewing" || c.Review != nil || !candidateAuthority(tx, t, p, c) {
					continue
				}
				kind = "review"
				runBase = c.Commit
				candidate = &c
				tool = ""
				for _, capability := range req.Capabilities {
					_, _, optionsValid := nodeExecution(n, capability.Tool)
					if optionsValid && capability.Tool != t.Tool && nodeAllows(n, capability.Tool, "review") && capability.Review && available(req.Capabilities, capability.Tool) {
						tool = capability.Tool
						break
					}
				}
				if tool == "" {
					for _, capability := range req.Capabilities {
						_, _, optionsValid := nodeExecution(n, capability.Tool)
						if optionsValid && capability.Tool == t.Tool && nodeAllows(n, capability.Tool, "review") && capability.Review && available(req.Capabilities, t.Tool) {
							tool = t.Tool
							break
						}
					}
				}
				if tool == "" {
					continue
				}
			} else if t.State != "ready" {
				continue
			} else if t.Repairs > 0 {
				previous, err := store.List[domain.Candidate](tx, "candidate")
				if err != nil {
					return nil, err
				}
				for _, old := range previous {
					if old.TaskID == t.ID && old.State == "rejected" && old.AuthorizationVersion == t.AuthorizationVersion && (candidate == nil || old.CreatedAt.After(candidate.CreatedAt)) {
						copy := old
						candidate = &copy
					}
				}
				if candidate != nil && candidate.Commit != "" {
					runBase = candidate.Commit
				}
			}
			{
				if err := goalGate(tx, &t, false); err != nil {
					continue
				}
			}
			if !nodeAllows(n, tool, kind) || !available(req.Capabilities, tool) || runBase == "" {
				continue
			}
			model, effort, optionsValid := nodeExecution(n, tool)
			if !optionsValid {
				continue
			}
			ready := true
			for _, id := range t.Dependencies {
				var dep domain.Task
				if e = tx.Get("task", id, &dep); e != nil {
					return nil, e
				}
				if dep.State != "integrated" && dep.State != "verified" {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			for _, id := range t.HotResources {
				var resource domain.Resource
				e = tx.Get("resource", id, &resource)
				if e == nil && resource.TaskID != t.ID {
					ready = false
					break
				}
				if e != nil && !errors.Is(e, domain.ErrNotFound) {
					return nil, e
				}
			}
			if !ready {
				continue
			}
			grant := limit(t.MaxRunSeconds, t.BudgetSeconds-t.UsedSeconds-t.ReservedSeconds)
			if t.GoalVersion != 0 {
				var d domain.PlanningDocument
				if e = tx.Get("planning_document", "current", &d); e != nil {
					return nil, e
				}
				grant = limit(grant, d.BudgetSeconds-d.UsedSeconds-d.ReservedSeconds)
			}
			if grant <= 0 {
				t.HoldReason = "budget"
				t.Version++
				if e = tx.Put("task", t.ID, t); e != nil {
					return nil, e
				}
				continue
			}
			for _, id := range t.HotResources {
				if e = tx.Put("resource", id, domain.Resource{ID: id, TaskID: t.ID}); e != nil {
					return nil, e
				}
			}
			now := s.Now()
			t.Generation++
			r := domain.Run{BaselineVersion: t.BaselineVersion, BaselineDigest: t.BaselineDigest, ID: ID("run_"), Version: 1, TaskID: t.ID, NodeID: n.ID, Kind: kind, Tool: tool, State: "running", Generation: t.Generation, AuthorizationVersion: t.AuthorizationVersion, PolicyVersion: p.PolicyVersion, LeaseUntil: now.Add(s.Lease), Deadline: now.Add(time.Duration(grant) * time.Second), GrantSeconds: grant, Base: runBase, StartedAt: now}
			r.Model, r.ReasoningEffort = model, effort
			if r.Deadline.Before(r.LeaseUntil) {
				r.LeaseUntil = r.Deadline
			}
			if candidate != nil {
				r.CandidateID = candidate.ID
			}
			r.GoalVersion = t.GoalVersion
			r.GoalAuthorizationVersion = t.GoalAuthorizationVersion
			if e = reserveGoal(tx, t, grant); e != nil {
				return nil, e
			}
			t.ReservedSeconds += grant
			p.ReservedSeconds += grant
			t.CurrentRunID = r.ID
			t.Version++
			if kind == "implement" {
				t.State = "executing"
			}
			p.Version++
			if e = tx.Put("run", r.ID, r); e != nil {
				return nil, e
			}
			if e = tx.Put("task", t.ID, t); e != nil {
				return nil, e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return nil, e
			}
			if e = s.audit(tx, a, "run.claimed", r.ID, kind+":"+tool); e != nil {
				return nil, e
			}
			goalContext, e := assignedGoalContext(tx)
			if e != nil {
				return nil, e
			}
			v, e := initialization(tx)
			if e != nil {
				return nil, e
			}
			return domain.Assignment{Baseline: v.ApprovedBaseline, GoalContext: goalContext, Run: r, Task: t, Project: p, Candidate: candidate, InputURL: "/api/v1/runs/" + r.ID + "/input.bundle", HeartbeatSeconds: 10}, nil
		}
		return nil, nil
	})
}

// settle only debits the delta. A lost acknowledgement cannot double-charge.
func (s *Service) settle(p *domain.Project, t *domain.Task, r *domain.Run, elapsed int64, finish bool) {
	duration := s.Now().Sub(r.StartedAt)
	wall := int64(duration / time.Second)
	if duration%time.Second > 0 {
		wall++
	} // Sub-second activity is not free.
	if elapsed < wall {
		elapsed = wall
	}
	if elapsed < r.ElapsedSeconds {
		elapsed = r.ElapsedSeconds
	}
	if elapsed > r.GrantSeconds {
		elapsed = r.GrantSeconds
	}
	delta := elapsed - r.ElapsedSeconds
	t.UsedSeconds += delta
	p.UsedSeconds += delta
	t.ReservedSeconds -= delta
	p.ReservedSeconds -= delta
	r.ElapsedSeconds = elapsed
	if finish {
		remaining := r.GrantSeconds - r.ElapsedSeconds
		t.ReservedSeconds -= remaining
		p.ReservedSeconds -= remaining
	}
	t.Version++
	p.Version++
	r.Version++
}
func (s *Service) Heartbeat(ctx context.Context, a Actor, id string, req domain.HeartbeatRequest) (domain.HeartbeatResponse, error) {
	var out domain.HeartbeatResponse
	if a.Role != "node" {
		return out, domain.Fail("forbidden", "只有Runner可续约")
	}
	err := s.DB.Update(ctx, func(tx *store.Tx) error {
		var n domain.Node
		if e := tx.Get("node", a.NodeID, &n); e != nil {
			return e
		}
		if n.Revoked {
			return domain.Fail("forbidden", "节点已撤销")
		}
		var r domain.Run
		if e := tx.Get("run", id, &r); e != nil {
			return e
		}
		if r.NodeID != a.NodeID || r.Generation != req.Generation {
			return domain.Fail("stale_run", "执行归属不匹配")
		}
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		owns := t.CurrentRunID == id && t.Generation == r.Generation
		if owns && active(r) && s.Now().Before(r.LeaseUntil) {
			n.LastSeen = s.Now()
			if e = tx.Put("node", n.ID, n); e != nil {
				return e
			}
		}
		reason := ""
		if !active(r) || !s.Now().Before(r.LeaseUntil) || !owns {
			reason = "stale_run"
		} else if p.Paused || t.HoldReason != "" || t.State == "cancelled" || r.State == "cancel_requested" {
			reason = "paused"
		} else if baselineRun(tx, r) != nil || t.AuthorizationVersion != r.AuthorizationVersion || p.PolicyVersion != r.PolicyVersion {
			reason = "authorization_changed"
		}
		if active(r) && owns {
			before := r.ElapsedSeconds
			s.settle(&p, &t, &r, req.ElapsedSeconds, false)
			if e = settleGoal(tx, t, r.ElapsedSeconds-before, 0); e != nil {
				return e
			}
			if r.ElapsedSeconds >= r.GrantSeconds || t.UsedSeconds >= t.BudgetSeconds {
				reason = "budget"
			}
		}
		if reason != "" {
			out.Cancel = true
			out.Reason = reason
			if active(r) {
				r.State = "cancel_requested"
			}
		} else {
			r.LeaseUntil = s.Now().Add(s.Lease)
			if r.Deadline.Before(r.LeaseUntil) {
				r.LeaseUntil = r.Deadline
			}
		}
		r.Detail = req.Detail
		if len(r.Detail) > 4096 {
			r.Detail = r.Detail[:4096]
		}
		out.LeaseUntil = r.LeaseUntil
		if e = tx.Put("run", r.ID, r); e != nil {
			return e
		}
		if owns {
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return e
			}
		}
		return nil
	})
	return out, err
}
func (s *Service) Complete(ctx context.Context, a Actor, key, id string, req domain.CompleteRequest) (json.RawMessage, error) {
	if a.Role != "node" {
		return nil, domain.Fail("forbidden", "只有Runner可交接")
	}
	if req.IntentStatus != "" && (req.Status != "stopped" || (req.IntentStatus != "scope_requested" && req.IntentStatus != "blocked")) {
		return nil, domain.Fail("invalid_input", "native intent disposition must be a scope/block stopped handoff")
	}
	return s.mutate(ctx, a, key, "run.complete."+id, req, func(tx *store.Tx) (any, error) {
		var n domain.Node
		if e := tx.Get("node", a.NodeID, &n); e != nil {
			return nil, e
		}
		if n.Revoked {
			return nil, domain.Fail("forbidden", "节点已撤销")
		}
		var r domain.Run
		if e := tx.Get("run", id, &r); e != nil {
			return nil, e
		}
		if r.NodeID != a.NodeID || r.Generation != req.Generation {
			return nil, domain.Fail("stale_run", "执行归属不匹配")
		}
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return nil, e
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if !active(r) && r.State != "lost" {
			return r, nil
		}
		owns := t.CurrentRunID == r.ID && t.Generation == r.Generation
		if req.IntentStatus != "" && r.Kind != "implement" {
			return nil, domain.Fail("invalid_input", "native intent disposition requires an implementation Run")
		}
		if !owns {
			// Historical evidence can close its own execution, but never settle the
			// current task, create a candidate, or replace a newer execution's hold.
			r.Quiescent = r.Quiescent || req.Quiescent
			r.Detail = req.Detail
			r.SessionID = req.SessionID
			r.State = "lost"
			if r.Quiescent {
				r.State = "stopped"
				now := s.Now()
				r.FinishedAt = &now
			}
			r.Version++
			if e = tx.Put("run", r.ID, r); e != nil {
				return nil, e
			}
			return r, s.audit(tx, a, "run.completed", id, fmt.Sprintf("%s; accepted=false; historical=true", r.State))
		}
		valid := grantedGoalAuthority(tx, &t, r) && active(r) && r.State != "cancel_requested" && s.Now().Before(r.LeaseUntil) && s.Now().Before(r.Deadline) && r.GrantSeconds > 0 && req.ElapsedSeconds <= r.GrantSeconds && t.AuthorizationVersion == r.AuthorizationVersion && p.PolicyVersion == r.PolicyVersion && !p.Paused && t.HoldReason == "" && t.State != "cancelled"
		if r.State != "lost" {
			elapsed := req.ElapsedSeconds
			if !s.Now().Before(r.LeaseUntil) || !req.Quiescent {
				// Unknown execution spends its whole finite grant, whether Expire
				// or a delayed completion observes the timeout first.
				elapsed = r.GrantSeconds
			}
			before := r.ElapsedSeconds
			s.settle(&p, &t, &r, elapsed, true)
			if e = settleGoal(tx, t, r.ElapsedSeconds-before, r.GrantSeconds-r.ElapsedSeconds); e != nil {
				return nil, e
			}
		}
		r.Quiescent = req.Quiescent
		r.Detail = req.Detail
		r.SessionID = req.SessionID
		now := s.Now()
		r.FinishedAt = &now
		r.State = "stopped"
		if !req.Quiescent {
			if t.HoldReason == "" {
				t.HoldReason = "offline"
			}
			r.State = "lost"
			valid = false
		}
		if t.UsedSeconds >= t.BudgetSeconds {
			if t.HoldReason == "" || t.HoldReason == "offline" {
				t.HoldReason = "budget"
			}
			valid = false
		}
		if req.Status == "stopped" {
			valid = false
		}
		if valid && req.Status == "succeeded" {
			if r.Kind == "implement" {
				if req.Head == "" || req.ArtifactID == "" {
					return nil, domain.Fail("invalid_input", "实现结果需要固定提交和代码材料")
				}
				var ar domain.Artifact
				if e = tx.Get("artifact", req.ArtifactID, &ar); e != nil || ar.RunID != r.ID {
					return nil, domain.Fail("invalid_input", "代码材料不属于本Run")
				}
				if t.CurrentCandidateID != "" {
					var old domain.Candidate
					if e = tx.Get("candidate", t.CurrentCandidateID, &old); e != nil {
						return nil, e
					}
					if old.State == "landing" || old.State == "reconciling" {
						return nil, domain.Fail("conflict", "旧候选正在对账")
					}
					old.State = "invalidated"
					old.Version++
					if e = tx.Put("candidate", old.ID, old); e != nil {
						return nil, e
					}
				}
				base := r.Base
				if r.CandidateID != "" {
					var prior domain.Candidate
					if e = tx.Get("candidate", r.CandidateID, &prior); e != nil {
						return nil, e
					}
					// Repairs of a verified C retain the cumulative task diff M..H.
					if prior.Commit == r.Base && prior.Main != "" {
						base = prior.Main
					}
				}
				c := domain.Candidate{BaselineVersion: r.BaselineVersion, BaselineDigest: r.BaselineDigest, ID: ID("cand_"), Version: 1, TaskID: t.ID, SourceRunID: r.ID, AuthorizationVersion: t.AuthorizationVersion, PolicyVersion: p.PolicyVersion, Base: base, Head: req.Head, State: "accepted", CreatedAt: now}
				if e = tx.Put("candidate", c.ID, c); e != nil {
					return nil, e
				}
				if e = tx.Put("candidate_artifact", c.ID, ar); e != nil {
					return nil, e
				}
				t.CurrentCandidateID = c.ID
				t.State = "awaiting_integration"
				r.Head = req.Head
			} else if r.Kind == "review" {
				var c domain.Candidate
				if e = tx.Get("candidate", r.CandidateID, &c); e != nil {
					return nil, e
				}
				if c.ID != t.CurrentCandidateID || c.Commit != r.Base || req.Review == nil || req.Review.Candidate != c.Commit {
					return nil, domain.Fail("invalid_input", "评审对象不匹配")
				}
				review := *req.Review
				review.RunID = r.ID
				if strings.TrimSpace(review.Summary) == "" || review.Findings == nil {
					return nil, domain.Fail("invalid_input", "评审需要结论理由和明确的问题列表")
				}
				if review.Verdict != "approve" && review.Verdict != "changes_requested" && review.Verdict != "blocked" {
					return nil, domain.Fail("invalid_input", "未知评审结论")
				}
				c.Review = &review
				c.Version++
				if review.Verdict == "approve" {
					c.State = "validating"
					if c.CheckPlan != nil && c.CheckPlan.PolicyVersion == p.TestPolicyVersion && domain.ChecksPassed(candidateChecks(p, c), c.Checks) && c.CheckRunID != "" {
						c.State = "ready_to_land"
					}
				} else {
					c.State = "rejected"
					c.Detail = review.Summary
					queueRepair(&t, &c, review.Verdict == "changes_requested")
				}
				if e = tx.Put("candidate", c.ID, c); e != nil {
					return nil, e
				}
			}
			r.State = "succeeded"
		} else if valid && req.Status != "stopped" {
			r.State = "failed"
			if req.Status == "transient_failure" && t.Retries < t.MaxRetries {
				t.Retries++
				if r.Kind == "implement" {
					t.State = "ready"
				}
			} else if req.Status == "failed" && r.Kind == "implement" && t.Repairs < t.MaxRepairs {
				t.Repairs++
				t.State = "ready"
			} else {
				t.HoldReason = "human_decision"
			}
		} else if t.State != "cancelled" && t.HoldReason == "" {
			t.HoldReason = "offline"
		}
		if !valid || req.Status == "stopped" {
			// A native scope/block stop is not an offline retry. If its receipt
			// was unavailable, preserve finite spend and hold for inspection.
			// This flag creates no scope request, approval, or stop proof.
			if req.IntentStatus != "" && t.State != "cancelled" && (t.HoldReason == "" || t.HoldReason == "offline") {
				t.HoldReason = "human_decision"
			}
			if _, e = s.recoverStopped(tx, p, &t, r); e != nil {
				return nil, e
			}
		} else {
			t.CurrentRunID = ""
		}
		t.Version++
		r.Version++
		if e = tx.Put("run", id, r); e != nil {
			return nil, e
		}
		if t.State == "cancelled" && r.Quiescent {
			if e = releaseResources(tx, t.ID); e != nil {
				return nil, e
			}
		}
		if e = tx.Put("task", t.ID, t); e != nil {
			return nil, e
		}
		if e = tx.Put("project", p.ID, p); e != nil {
			return nil, e
		}
		if e = s.taskDecision(tx, t, r.Detail); e != nil {
			return nil, e
		}
		return r, s.audit(tx, a, "run.completed", id, fmt.Sprintf("%s; accepted=%t", r.State, valid))
	})
}
func queueRepair(t *domain.Task, c *domain.Candidate, repairable bool) {
	if t.State == "cancelled" || t.State == "integrated" || t.HoldReason != "" {
		return
	}
	if repairable && t.Repairs < t.MaxRepairs {
		t.Repairs++
		t.State = "ready"
		t.CurrentCandidateID = ""
		t.HoldReason = ""
	} else {
		t.HoldReason = "human_decision"
	}
	t.CurrentRunID = ""
}
func (s *Service) Expire(ctx context.Context) error {
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return e
		}
		for _, r := range runs {
			if !active(r) || s.Now().Before(r.LeaseUntil) {
				continue
			}
			var t domain.Task
			if e = tx.Get("task", r.TaskID, &t); e != nil {
				return e
			}
			owns := t.CurrentRunID == r.ID && t.Generation == r.Generation
			// Unknown execution conservatively spends its entire finite grant. A later
			// observation can confirm quiescence, but never silently refunds uncertainty.
			// Historical executions are already fenced and cannot debit a new Run.
			if owns {
				before := r.ElapsedSeconds
				s.settle(&p, &t, &r, r.GrantSeconds, true)
				if e = settleGoal(tx, t, r.ElapsedSeconds-before, 0); e != nil {
					return e
				}
			}
			r.State = "lost"
			r.Version++
			r.Detail = "租约已失效，等待进程停止确认"
			if owns && t.HoldReason == "" {
				t.HoldReason = "offline"
			}
			if e = tx.Put("run", r.ID, r); e != nil {
				return e
			}
			if owns {
				if e = tx.Put("task", t.ID, t); e != nil {
					return e
				}
			}
			if e = s.audit(tx, Actor{ID: "center"}, "run.expired", r.ID, ""); e != nil {
				return e
			}
		}
		// Stop confirmation may arrive while the project is paused. Keep the old
		// ownership as a durable recovery marker and advance it after resume.
		for _, r := range runs {
			if !r.Quiescent || active(r) {
				continue
			}
			var t domain.Task
			if e = tx.Get("task", r.TaskID, &t); e != nil {
				return e
			}
			changed, err := s.recoverStopped(tx, p, &t, r)
			if err != nil {
				return err
			}
			if changed {
				t.Version++
				if e = tx.Put("task", t.ID, t); e != nil {
					return e
				}
			}
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		for _, task := range tasks {
			if e = s.taskDecision(tx, task, ""); e != nil {
				return e
			}
		}
		if e = s.goalBudgetDecision(tx); e != nil {
			return e
		}
		return tx.Put("project", p.ID, p)
	})
}
func (s *Service) AssignmentRun(ctx context.Context, a Actor, id string) (domain.Run, error) {
	var r domain.Run
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("run", id, &r); e != nil {
			return e
		}
		if _, e := nodeConsoleActor(tx, a); e != nil {
			return e
		}
		if a.Role != "node" || r.NodeID != a.NodeID {
			return domain.Fail("forbidden", "无权读取此Run")
		}
		return nil
	})
	return r, e
}

// RunContext exposes only an execution's task context to its owning node.
func (s *Service) RunContext(ctx context.Context, a Actor, id string) (domain.Assignment, error) {
	var out domain.Assignment
	r, e := s.AssignmentRun(ctx, a, id)
	if e != nil {
		return out, e
	}
	out.Run = r
	e = s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("task", r.TaskID, &out.Task); e != nil {
			return e
		}
		var e error
		if e = baselineRun(tx, r); e != nil {
			return e
		}
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		out.Baseline = v.ApprovedBaseline
		out.GoalContext, e = assignedGoalContext(tx)
		if e != nil {
			return e
		}
		out.Project, e = getProject(tx)
		if e != nil {
			return e
		}
		if r.CandidateID != "" {
			var c domain.Candidate
			if e = tx.Get("candidate", r.CandidateID, &c); e != nil {
				return e
			}
			out.Candidate = &c
		}
		return nil
	})
	return out, e
}
