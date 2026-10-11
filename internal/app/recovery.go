package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"fmt"
	"strings"
)

// recoverStopped advances only the execution that still owns the task. The
// caller persists the changed task and its version in the same transaction as
// the stop evidence. Network delivery and selecting a new Runner remain Claim's
// responsibility; a stopped execution never regains its old lease.
func (s *Service) recoverStopped(tx *store.Tx, p domain.Project, t *domain.Task, r domain.Run) (bool, error) {
	if t.CurrentRunID != r.ID || t.Generation != r.Generation || !r.Quiescent || (r.State != "stopped" && r.State != "lost") {
		return false, nil
	}
	if t.State == "cancelled" || t.State == "integrated" || t.ApprovedVersion == 0 || (t.HoldReason != "" && t.HoldReason != "offline") {
		t.CurrentRunID = ""
		return true, nil
	}
	block := func(reason, detail string) (bool, error) {
		t.CurrentRunID = ""
		t.HoldReason = reason
		return true, s.audit(tx, Actor{ID: "center"}, "task.recovery_blocked", t.ID, detail)
	}
	if t.AuthorizationVersion != r.AuthorizationVersion || p.PolicyVersion != r.PolicyVersion {
		return block("human_decision", "旧执行授权或项目策略已变化，不能自动恢复")
	}
	// A different unknown process is also a fence. This matters after a restored
	// backup or when a historical stop confirmation arrives out of order.
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return false, e
	}
	for _, old := range runs {
		if old.ID != r.ID && old.TaskID == t.ID && !old.Quiescent && (active(old) || old.State == "lost") {
			changed := t.HoldReason != "offline"
			t.HoldReason = "offline"
			return changed, nil
		}
	}
	if p.Paused {
		changed := t.HoldReason != "offline"
		t.HoldReason = "offline"
		return changed, nil
	}
	if t.BudgetSeconds-t.UsedSeconds-t.ReservedSeconds <= 0 || p.TotalBudgetSeconds-p.UsedSeconds-p.ReservedSeconds <= 0 {
		return block("budget", "停止已确认，但累计剩余预算不足")
	}
	if r.Kind == "implement" && t.CurrentCandidateID != "" {
		var c domain.Candidate
		if e = tx.Get("candidate", t.CurrentCandidateID, &c); e != nil {
			return false, e
		}
		if t.State != "awaiting_integration" || c.AuthorizationVersion != t.AuthorizationVersion || c.PolicyVersion != p.PolicyVersion || (c.State != "accepted" && c.State != "reviewing" && c.State != "validating" && c.State != "ready_to_land") {
			return block("human_decision", "已受理候选状态不允许自动恢复")
		}
		// Already accepted material proceeds through its existing gates; the old
		// implementation is never dispatched again merely because delivery failed.
		t.CurrentRunID = ""
		t.HoldReason = ""
		return true, s.audit(tx, Actor{ID: "center"}, "task.automatically_recovered", t.ID, "保留已受理候选，继续集成流程")
	}
	switch r.Kind {
	case "implement":
		if t.State != "executing" {
			return block("human_decision", "任务状态不允许重新开发")
		}
	case "review", "validation":
		var c domain.Candidate
		if t.State != "awaiting_integration" || t.CurrentCandidateID != r.CandidateID || r.CandidateID == "" {
			return block("human_decision", "恢复对象已不再是当前候选")
		}
		if e = tx.Get("candidate", r.CandidateID, &c); e != nil {
			return false, e
		}
		if c.Commit != r.Base || c.AuthorizationVersion != t.AuthorizationVersion || c.PolicyVersion != p.PolicyVersion {
			return block("human_decision", "固定候选或其授权已变化")
		}
		if r.Kind == "review" && (c.State != "reviewing" || c.Review != nil) || r.Kind == "validation" && (c.State != "validating" || c.Review == nil || c.Review.Verdict != "approve" || c.Review.Candidate != c.Commit) {
			return block("human_decision", "固定候选不允许重试当前步骤")
		}
	default:
		return block("human_decision", "未知执行类型不能自动恢复")
	}
	if t.Retries >= t.MaxRetries {
		return block("human_decision", "自动恢复重试次数已用尽")
	}
	t.Retries++
	t.CurrentRunID = ""
	t.HoldReason = ""
	if r.Kind == "implement" {
		t.State = "ready"
	}
	return true, s.audit(tx, Actor{ID: "center"}, "task.automatically_recovered", t.ID, fmt.Sprintf("%s; prior_run=%s; retry=%d/%d", r.Kind, r.ID, t.Retries, t.MaxRetries))
}

// RecoverCenter fences validation processes from a previous center lifetime.
// It never assumes that an orphaned process disappeared with its parent.
func (s *Service) RecoverCenter(ctx context.Context) error {
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return e
		}
		for _, r := range runs {
			if r.NodeID != "center" || !active(r) {
				continue
			}
			var t domain.Task
			if e = tx.Get("task", r.TaskID, &t); e != nil {
				return e
			}
			p, e := getProject(tx)
			if e != nil {
				return e
			}
			owns := t.CurrentRunID == r.ID && t.Generation == r.Generation
			if owns {
				s.settle(&p, &t, &r, r.GrantSeconds, true)
			}
			r.State = "lost"
			r.Version++
			r.Detail = "中心已重启；旧验证进程需确认停止"
			r.LeaseUntil = s.Now()
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
				if e = tx.Put("project", p.ID, p); e != nil {
					return e
				}
			}
			if e = s.audit(tx, Actor{ID: "center"}, "run.recovery_required", r.ID, r.Detail); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s *Service) ConfirmStopped(ctx context.Context, a Actor, key, id string, req domain.StopConfirmation) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if strings.TrimSpace(req.Evidence) == "" {
		return nil, domain.Fail("invalid_input", "需要实际进程检查记录")
	}
	return s.mutate(ctx, a, key, "run.confirm-stopped."+id, req, func(tx *store.Tx) (any, error) {
		var r domain.Run
		if e := tx.Get("run", id, &r); e != nil {
			return nil, e
		}
		if e := version(r.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if r.State != "lost" || r.Quiescent {
			return nil, domain.Fail("conflict", "仅允许确认失联且待核对的Run")
		}
		r.Quiescent = true
		r.State = "stopped"
		r.Version++
		r.Detail = req.Evidence
		now := s.Now()
		r.FinishedAt = &now
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return nil, e
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		changed, e := s.recoverStopped(tx, p, &t, r)
		if e != nil {
			return nil, e
		}
		if changed {
			t.Version++
		}
		if e := tx.Put("run", id, r); e != nil {
			return nil, e
		}
		if changed {
			if e := tx.Put("task", t.ID, t); e != nil {
				return nil, e
			}
		}
		if t.State == "cancelled" {
			if e := releaseResources(tx, t.ID); e != nil {
				return nil, e
			}
		}
		return r, s.audit(tx, a, "run.stop_confirmed", id, req.Evidence)
	})
}

func (s *Service) TaskBudget(ctx context.Context, a Actor, key, id string, req domain.BudgetRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "task.budget."+id, req, func(tx *store.Tx) (any, error) {
		var t domain.Task
		if e := tx.Get("task", id, &t); e != nil {
			return nil, e
		}
		if e := version(t.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if e := requireQuiescence(tx, t.ID); e != nil {
			return nil, e
		}
		if t.State == "cancelled" || t.State == "integrated" || t.CurrentRunID != "" || req.BudgetSeconds < t.UsedSeconds+t.ReservedSeconds || req.BudgetSeconds <= 0 || req.BudgetSeconds > 31536000 || req.MaxRepairs < t.Repairs || req.MaxRetries < t.Retries || req.MaxRepairs > 100 || req.MaxRetries > 100 || strings.TrimSpace(req.Reason) == "" {
			return nil, domain.Fail("invalid_input", "停止执行后才能批准新的有限预算，已使用额度不得减少")
		}
		t.BudgetSeconds = req.BudgetSeconds
		t.MaxRepairs = req.MaxRepairs
		t.MaxRetries = req.MaxRetries
		t.Version++
		if e := tx.Put("task", id, t); e != nil {
			return nil, e
		}
		return t, s.audit(tx, a, "task.budget", id, req.Reason)
	})
}

func (s *Service) ProjectBudget(ctx context.Context, a Actor, key string, req domain.BudgetRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "project.budget", req, func(tx *store.Tx) (any, error) {
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if e = version(p.Version, req.ExpectedVersion); e != nil {
			return nil, e
		}
		if !p.Paused || req.BudgetSeconds < p.UsedSeconds+p.ReservedSeconds || req.BudgetSeconds <= 0 || req.BudgetSeconds > 315360000 || req.MaxRepairs != 0 || req.MaxRetries != 0 || strings.TrimSpace(req.Reason) == "" {
			return nil, domain.Fail("invalid_input", "暂停项目后批准新的有限总预算，已用与预留额度不能减少")
		}
		p.TotalBudgetSeconds = req.BudgetSeconds
		p.Version++
		if e = tx.Put("project", p.ID, p); e != nil {
			return nil, e
		}
		return p, s.audit(tx, a, "project.budget", p.ID, req.Reason)
	})
}

func requireQuiescence(tx *store.Tx, taskID string) error {
	runs, e := store.List[domain.Run](tx, "run")
	if e != nil {
		return e
	}
	for _, r := range runs {
		if r.TaskID == taskID && !r.Quiescent && (active(r) || r.State == "lost") {
			return domain.Fail("conflict", "旧执行尚未确认停止")
		}
	}
	return nil
}
