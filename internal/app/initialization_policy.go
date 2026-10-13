package app

import (
	"context"
	"encoding/json"
	"strings"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

func (s *Service) InitializationOperation(ctx context.Context, a Actor) (domain.InitializationOperation, error) {
	var out domain.InitializationOperation
	if e := owner(a); e != nil {
		return out, e
	}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		v, e := initialization(tx)
		if e != nil {
			return e
		}
		if v.OperationID == "" {
			return domain.ErrNotFound
		}
		return tx.Get("initialization_operation", v.OperationID, &out)
	})
	return out, e
}

// Owner policy changes are explicit and invalidate initialization. A Leader
// suggestion, ordinary chat answer or baseline confirmation never changes them.
func (s *Service) UpdateProjectPolicy(ctx context.Context, a Actor, key string, in domain.ProjectPolicyRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "project.policy", in, func(tx *store.Tx) (any, error) {
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if e = version(p.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if e = initializationQuiet(tx); e != nil {
			return nil, e
		}
		busy, e := initializationBusy(tx)
		if e != nil {
			return nil, e
		}
		if busy {
			return nil, domain.Fail("busy", "初始化请求尚未停止")
		}
		p.Checks = in.Checks
		p.ProtectedPaths = in.ProtectedPaths
		seen := map[string]bool{}
		for _, path := range p.ProtectedPaths {
			if seen[path] {
				return nil, domain.Fail("invalid_input", "duplicate protected paths")
			}
			seen[path] = true
		}
		for _, path := range authorityPaths {
			if !seen[path] {
				p.ProtectedPaths = append(p.ProtectedPaths, path)
				seen[path] = true
			}
		}
		if e = domain.ValidateProject(p); e != nil {
			return nil, e
		}
		if _, e = gitops.NewScope([]string{"**"}, p.ProtectedPaths); e != nil {
			return nil, e
		}
		p.PolicyVersion++
		p.Version++
		if e = tx.Put("project", p.ID, p); e != nil {
			return nil, e
		}
		v, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		v.Status = "draft"
		v.ApprovedVersion = 0
		v.ApprovedDigest = ""
		v.ApprovedBaseline = nil
		v.Detail = "Owner 政策已更新；需重新分析与确认共享基线"
		if e = tx.Put("initialization", "current", v); e != nil {
			return nil, e
		}
		if e = s.invalidateBaselineTasks(tx); e != nil {
			return nil, e
		}
		return p, s.audit(tx, a, "project.policy", p.ID, "initialization invalidated")
	})
}
func (s *Service) CancelInitialization(ctx context.Context, a Actor, key string, in domain.VersionRequest) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	return s.mutate(ctx, a, key, "initialization.cancel", in, func(tx *store.Tx) (any, error) {
		v, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		if e = version(v.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		var op domain.InitializationOperation
		if e = tx.Get("initialization_operation", v.OperationID, &op); e != nil {
			return nil, e
		}
		if op.Settled {
			return v, nil
		}
		if op.State == "queued" {
			op.State = "cancelled"
			op.Settled = true
			op.TreeStopped = true
			v.Status = "draft"
		} else {
			op.State = "cancel_requested"
		}
		op.Version++
		if e = tx.Put("initialization_operation", op.ID, op); e != nil {
			return nil, e
		}
		if e = tx.Put("initialization", "current", v); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "initialization.cancel_requested", op.ID, "")
	})
}

// As with the existing Run/Leader recovery interfaces, an Owner must supply
// an actual host process-tree inspection record. This never kills a PID or
// treats a deadline/model assertion as stop proof. Late real receipts remain fenced.
func (s *Service) ConfirmInitializationStopped(ctx context.Context, a Actor, key string, in domain.StopConfirmation) (json.RawMessage, error) {
	if e := owner(a); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Evidence) == "" {
		return nil, domain.Fail("invalid_input", "需要实际进程树停止检查记录")
	}
	return s.mutate(ctx, a, key, "initialization.confirm_stopped", in, func(tx *store.Tx) (any, error) {
		v, e := initialization(tx)
		if e != nil {
			return nil, e
		}
		var op domain.InitializationOperation
		if e = tx.Get("initialization_operation", v.OperationID, &op); e != nil {
			return nil, e
		}
		if e = version(op.Version, in.ExpectedVersion); e != nil {
			return nil, e
		}
		if op.State != "unknown_live" {
			return nil, domain.Fail("conflict", "仅恢复未知存活初始化；在线取消需等待管理进程回执")
		}
		p, e := getProject(tx)
		if e != nil {
			return nil, e
		}
		if !op.Settled {
			p.ReservedSeconds -= op.GrantSeconds
			p.UsedSeconds += op.GrantSeconds
			op.UsedSeconds += op.GrantSeconds
			p.Version++
			if e = tx.Put("project", p.ID, p); e != nil {
				return nil, e
			}
		}
		op.Settled = true
		op.TreeStopped = true
		op.State = "stopped"
		op.ElapsedSeconds = op.GrantSeconds
		op.Version++
		v.Status = "draft"
		v.Detail = "Owner 已提供实际停止核验，按完整有限预留记账；需显式重新分析"
		if e = tx.Put("initialization_operation", op.ID, op); e != nil {
			return nil, e
		}
		if e = tx.Put("initialization", "current", v); e != nil {
			return nil, e
		}
		return v, s.audit(tx, a, "initialization.owner_stop_proof", op.ID, in.Evidence)
	})
}
