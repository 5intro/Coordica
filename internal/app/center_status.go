package app

import (
	"context"
	"errors"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func (s *Service) PublicHealth(ctx context.Context) (domain.CenterHealth, error) {
	out := domain.CenterHealth{Status: "ok", APIVersion: domain.APIVersion}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		var id domain.CenterIdentity
		e := tx.Get("center_identity", "current", &id)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		out.CenterInstanceID = id.InstanceID
		out.ConfigDigest = id.ConfigDigest
		return nil
	})
	return out, e
}

func (s *Service) CenterStatus(ctx context.Context, a Actor) (domain.CenterRuntimeStatus, error) {
	var out domain.CenterRuntimeStatus
	if e := owner(a); e != nil {
		return out, e
	}
	e := s.DB.View(ctx, func(tx *store.Tx) error {
		if e := tx.Get("center_identity", "current", &out.Identity); e != nil {
			return e
		}
		p, e := getProject(tx)
		if e != nil {
			return e
		}
		out.ProjectPaused = p.Paused
		settings, e := automation(tx)
		if e != nil {
			return e
		}
		out.AutomationEnabled = settings.Enabled
		nodes, e := store.List[domain.Node](tx, "node")
		if e != nil {
			return e
		}
		for _, node := range nodes {
			if node.Revoked {
				continue
			}
			out.RegisteredRunners++
			if !node.LastSeen.IsZero() && (out.LastRunnerHeartbeat == nil || node.LastSeen.After(*out.LastRunnerHeartbeat)) {
				last := node.LastSeen
				out.LastRunnerHeartbeat = &last
			}
		}
		return nil
	})
	return out, e
}
