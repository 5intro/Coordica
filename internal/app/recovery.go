package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"strings"
)

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
			s.settle(&p, &t, &r, r.GrantSeconds, true)
			r.State = "lost"
			r.Detail = "中心已重启；旧验证进程需确认停止"
			r.LeaseUntil = s.Now()
			t.HoldReason = "offline"
			if e = tx.Put("run", r.ID, r); e != nil {
				return e
			}
			if e = tx.Put("task", t.ID, t); e != nil {
				return e
			}
			if e = tx.Put("project", p.ID, p); e != nil {
				return e
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
		if t.CurrentRunID == r.ID {
			t.CurrentRunID = ""
		}
		t.Version++
		if e := tx.Put("run", id, r); e != nil {
			return nil, e
		}
		if e := tx.Put("task", t.ID, t); e != nil {
			return nil, e
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
