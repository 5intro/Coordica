package app

import (
	"context"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestProgressRuntimeProjectionKeepsNodeScope(t *testing.T) {
	f, assignment := progressFixture(t)
	ctx := context.Background()
	f.now = f.now.Add(30 * time.Minute)
	if _, err := f.s.Heartbeat(ctx, f.node, assignment.Run.ID, domain.HeartbeatRequest{
		Generation: assignment.Run.Generation, Progress: &domain.ProgressObservation{Sequence: 1, ToolCompleted: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		foreign := assignment.Run
		foreign.ID, foreign.NodeID = "foreign-run", "foreign-node"
		return tx.Put("run", foreign.ID, foreign)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.View(ctx, func(tx *store.Tx) error {
		runs, err := chatRuns(tx, domain.Credential{ChatScope: domain.ChatScopeNode, NodeID: f.node.NodeID})
		if err != nil {
			return err
		}
		if len(runs) != 1 || runs[0].ID != assignment.Run.ID || runs[0].ProgressVersion != 1 ||
			runs[0].ProgressCheckSeconds != 1800 || runs[0].Progress == nil || runs[0].Progress.Observation.ToolCompleted != 2 {
			t.Fatal("safe progress lost or crossed node scope", runs)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
