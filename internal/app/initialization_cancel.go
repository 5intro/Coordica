package app

import (
	"context"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// Cancellation is driven by committed use-case mutations and the operation's
// finite context deadline. There is no chat progress poller or runtime mailbox.
func (s *Service) registerInitializationCancel(id string, cancel context.CancelFunc) func() {
	s.initMu.Lock()
	if s.initCancels == nil {
		s.initCancels = map[string]context.CancelFunc{}
	}
	s.initCancels[id] = cancel
	s.initMu.Unlock()
	return func() { s.initMu.Lock(); delete(s.initCancels, id); s.initMu.Unlock() }
}
func (s *Service) cancelInvalidInitializations(ctx context.Context) {
	s.initMu.Lock()
	pending := map[string]context.CancelFunc{}
	for id, cancel := range s.initCancels {
		pending[id] = cancel
	}
	s.initMu.Unlock()
	for id, cancel := range pending {
		invalid := false
		e := s.DB.View(ctx, func(tx *store.Tx) error {
			var op domain.InitializationOperation
			if e := tx.Get("initialization_operation", id, &op); e != nil {
				return e
			}
			p, e := getProject(tx)
			if e != nil {
				return e
			}
			v, e := initialization(tx)
			if e != nil {
				return e
			}
			invalid = op.State != "running" || initializationIssuer(tx, op) != nil || !p.Paused || initializationPolicy(p) != v.PolicyDigest || v.OperationID != id || !s.Now().Before(op.Deadline)
			return nil
		})
		if e != nil || invalid {
			cancel()
		}
	}
}
