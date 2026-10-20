package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"coordica/internal/domain"
	"coordica/internal/store"
)

func TestNodePresenceDoesNotRequireOrApproveBaseline(t *testing.T) {
	for _, state := range []string{"draft", "analyzing", "initialized"} {
		t.Run(state, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
				v, err := initialization(tx)
				if err != nil {
					return err
				}
				v.Status = state
				if state != "initialized" {
					v.ApprovedVersion = 0
				}
				return tx.Put("initialization", "current", v)
			}); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			baseline, err := f.s.Initialization(ctx, f.owner)
			if err != nil {
				t.Fatal(err)
			}
			f.now = f.now.Add(time.Hour)
			caps := domain.ClaimRequest{Capabilities: recoveryCapabilities("codex")}
			if err := f.s.ReportNodePresence(ctx, f.node, caps); err != nil {
				t.Fatal(err)
			}
			after := f.snapshot(t)
			if !after.Nodes[0].LastSeen.Equal(f.now) || !reflect.DeepEqual(after.Nodes[0].Capabilities, caps.Capabilities) {
				t.Fatal("node telemetry did not refresh")
			}
			after.Nodes = before.Nodes
			if !reflect.DeepEqual(before, after) {
				t.Fatal("presence changed project execution or accounting")
			}
			current, err := f.s.Initialization(ctx, f.owner)
			if err != nil || !reflect.DeepEqual(baseline, current) {
				t.Fatal("presence changed baseline approval", err)
			}
			if state != "initialized" {
				if _, err := f.s.Claim(ctx, f.node, "still-blocked", caps, strings.Repeat("a", 40)); err == nil {
					t.Fatal("presence bypassed initialization gate")
				}
			}
		})
	}
}

func TestNodePresencePreservesOwnerSettingsAndCannotRenewExpiredRun(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	assignment := f.claim(t, "claim-before-expiry")
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var n domain.Node
		if err := tx.Get("node", f.node.NodeID, &n); err != nil {
			return err
		}
		n.Draining, n.DisabledNewWork = true, true
		n.SchedulingConfigured = true
		n.Tools = []domain.ToolScheduling{{Tool: "codex", Enabled: true, Implement: true, Model: "fixed-model", ReasoningEffort: "high"}}
		if err := tx.Put("node", n.ID, n); err != nil {
			return err
		}
		p, err := getProject(tx)
		if err != nil {
			return err
		}
		p.Paused = true
		return tx.Put("project", p.ID, p)
	}); err != nil {
		t.Fatal(err)
	}
	f.now = assignment.Run.LeaseUntil.Add(time.Minute)
	before := f.snapshot(t)
	for i := 0; i < 2; i++ {
		f.now = f.now.Add(time.Second)
		if err := f.s.ReportNodePresence(ctx, f.node, domain.ClaimRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	after := f.snapshot(t)
	if !after.Nodes[0].LastSeen.Equal(f.now) || len(after.Nodes[0].Capabilities) != 0 {
		t.Fatal("idle report failed to remove obsolete capabilities")
	}
	node := after.Nodes[0]
	node.LastSeen, node.Capabilities = before.Nodes[0].LastSeen, before.Nodes[0].Capabilities
	if !reflect.DeepEqual(node, before.Nodes[0]) {
		t.Fatal("presence changed Owner node settings")
	}
	after.Nodes = before.Nodes
	if !reflect.DeepEqual(before, after) {
		t.Fatal("presence changed a Run, lease or reservation")
	}
	if hb, err := f.s.Heartbeat(ctx, f.node, assignment.Run.ID, domain.HeartbeatRequest{Generation: assignment.Run.Generation}); err != nil || !hb.Cancel || hb.Reason != "stale_run" {
		t.Fatal("live node revived an expired Run", hb, err)
	}
}

func TestNodePresenceRejectsRevokedForgedAndMalformedReports(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	before := f.snapshot(t)
	for _, actor := range []Actor{f.owner, {ID: "other", Role: "node", NodeID: f.node.NodeID}, {ID: "missing", Role: "node", NodeID: "missing"}} {
		if err := f.s.ReportNodePresence(ctx, actor, domain.ClaimRequest{}); err == nil {
			t.Fatal("foreign actor reported presence", actor)
		}
	}
	if err := f.s.ReportNodePresence(ctx, f.node, domain.ClaimRequest{Capabilities: []domain.Capability{{Tool: "../shell"}}}); err == nil {
		t.Fatal("malformed capabilities changed telemetry")
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("rejected request changed project state")
	}
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		var node domain.Node
		if err := tx.Get("node", f.node.NodeID, &node); err != nil {
			return err
		}
		node.Revoked = true
		return tx.Put("node", node.ID, node)
	}); err != nil {
		t.Fatal(err)
	}
	before = f.snapshot(t)
	f.now = f.now.Add(time.Hour)
	if err := f.s.ReportNodePresence(ctx, f.node, domain.ClaimRequest{}); err == nil {
		t.Fatal("actor authenticated before revocation reported presence")
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("revoked presence mutated node")
	}
}

func TestNodePresenceCannotReplayAssignmentThroughChangedBaseline(t *testing.T) {
	f := newServiceFixture(t)
	f.claim(t, "original-claim")
	ctx := context.Background()
	if err := f.s.DB.Update(ctx, func(tx *store.Tx) error {
		v, err := initialization(tx)
		if err != nil {
			return err
		}
		v.Status, v.ApprovedVersion = "draft", 0
		return tx.Put("initialization", "current", v)
	}); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	f.now = f.now.Add(time.Second)
	caps := domain.ClaimRequest{Capabilities: recoveryCapabilities("codex")}
	if err := f.s.ReportNodePresence(ctx, f.node, caps); err != nil {
		t.Fatal(err)
	}
	if raw, err := f.s.Claim(ctx, f.node, "original-claim", caps, strings.Repeat("a", 40)); err == nil || len(raw) != 0 {
		t.Fatal("presence revived a replayed work authorization", string(raw), err)
	}
	after := f.snapshot(t)
	after.Nodes = before.Nodes
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected replay changed execution state")
	}
}
