package app

import (
	"context"

	"coordica/internal/domain"
	"coordica/internal/store"
)

// ReportNodePresence records telemetry independently of execution admission.
// It cannot claim work, extend a Run lease or provide proof of process exit.
// Like Run heartbeats it uses receipt time, without a replayable work result.
func (s *Service) ReportNodePresence(ctx context.Context, a Actor, in domain.ClaimRequest) error {
	if err := domain.ValidateCapabilities(in.Capabilities); err != nil {
		return err
	}
	return s.DB.Update(ctx, func(tx *store.Tx) error {
		if _, err := nodeConsoleActor(tx, a); err != nil {
			return err
		}
		// nodeConsoleActor returns a redacted view; retain the stored identity
		// and all Owner scheduling settings when updating telemetry.
		var node domain.Node
		if err := tx.Get("node", a.NodeID, &node); err != nil {
			return err
		}
		node.LastSeen = s.Now()
		node.Capabilities = in.Capabilities
		return tx.Put("node", node.ID, node)
	})
}
