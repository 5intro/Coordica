package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"encoding/json"
	"strings"
)

func (s *Service) Block(ctx context.Context, a Actor, key, id string, generation int64, reason string) (json.RawMessage, error) {
	if a.Role != "node" || strings.TrimSpace(reason) == "" {
		return nil, domain.Fail("invalid_input", "需要节点凭据和阻塞理由")
	}
	input := struct {
		Generation int64
		Reason     string
	}{generation, reason}
	return s.mutateWithCheck(ctx, a, key, "run.block."+id, input, func(tx *store.Tx) error {
		_, err := nodeConsoleActor(tx, a)
		return err
	}, func(tx *store.Tx) (any, error) {
		var r domain.Run
		if e := tx.Get("run", id, &r); e != nil {
			return nil, e
		}
		if r.NodeID != a.NodeID || r.Generation != generation || !active(r) || !s.Now().Before(r.LeaseUntil) {
			return nil, domain.Fail("stale_run", "执行已失效")
		}
		var t domain.Task
		if e := tx.Get("task", r.TaskID, &t); e != nil {
			return nil, e
		}
		if t.CurrentRunID != r.ID {
			return nil, domain.Fail("stale_run", "执行归属已变化")
		}
		t.HoldReason = "human_decision"
		t.Version++
		r.State = "cancel_requested"
		r.Detail = reason
		r.Version++
		if e := tx.Put("task", t.ID, t); e != nil {
			return nil, e
		}
		if e := tx.Put("run", r.ID, r); e != nil {
			return nil, e
		}
		if e := s.taskDecision(tx, t, reason); e != nil {
			return nil, e
		}
		return t, s.audit(tx, a, "run.blocked", r.ID, reason)
	})
}
