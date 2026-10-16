package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
)

// Claim resolves a Git baseline only when implementable work is waiting. Polling
// an idle/paused node must not fetch the remote repository every second.
func (w *Integrator) Claim(ctx context.Context, a Actor, key string, req domain.ClaimRequest) (json.RawMessage, error) {
	if a.Role != "node" {
		return nil, domain.Fail("forbidden", "只有Runner可领取执行")
	}
	if err := domain.ValidateCapabilities(req.Capabilities); err != nil {
		return nil, err
	}
	needed := false
	e := w.Service.DB.View(ctx, func(tx *store.Tx) error {
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		blocked, e := unknownConsultation(tx)
		if e != nil {
			return e
		}
		if blocked {
			return nil
		}
		if p.Paused {
			return nil
		}
		var n domain.Node
		if e = tx.Get("node", a.NodeID, &n); e != nil {
			return e
		}
		if n.Draining || n.DisabledNewWork {
			return nil
		}
		if n.Revoked {
			return domain.Fail("forbidden", "节点已撤销")
		}
		n.Capabilities = req.Capabilities
		runs, e := store.List[domain.Run](tx, "run")
		if e != nil {
			return e
		}
		busy := 0
		for _, r := range runs {
			if r.NodeID == n.ID && (active(r) || r.State == "lost" && !r.Quiescent) {
				busy++
			}
		}
		if busy >= n.Capacity {
			return nil
		}
		tasks, e := store.List[domain.Task](tx, "task")
		if e != nil {
			return e
		}
		for _, t := range tasks {
			_, _, optionsValid := nodeExecution(n, t.Tool)
			if optionsValid && t.State == "ready" && t.ApprovedVersion > 0 && t.HoldReason == "" && t.CurrentRunID == "" && available(req.Capabilities, t.Tool) {
				if goalGate(tx, &t, false) != nil || !nodeAllows(n, t.Tool, "implement") {
					continue
				}
				dependenciesReady := true
				for _, id := range t.Dependencies {
					var dependency domain.Task
					if e := tx.Get("task", id, &dependency); e != nil {
						return e
					}
					if dependency.State != "integrated" && dependency.State != "verified" {
						dependenciesReady = false
						break
					}
				}
				if !dependenciesReady {
					continue
				}
				needed = true
				break
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	var base string
	if needed {
		base, e = w.Base(ctx)
		if e != nil {
			return nil, e
		}
	}
	// The transactional use case independently revalidates all authority. The
	// preceding read is an I/O optimization, never a reservation or permission.
	return w.Service.Claim(ctx, a, key, req, base)
}
