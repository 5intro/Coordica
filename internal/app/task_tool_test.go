package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"coordica/internal/domain"
)

func taskToolFixture(t *testing.T) (*serviceFixture, []domain.Capability) {
	t.Helper()
	f := newServiceFixture(t)
	caps := []domain.Capability{modelCapability(), {Tool: "custom-agent", Protocol: "acp", Available: true,
		Structured: true, Cancel: true, ModelDiscovery: "reported", Models: []domain.ModelOption{{ID: "flash-test", Name: "Flash"}}}}
	if err := f.s.ReportNodePresence(context.Background(), f.node, domain.ClaimRequest{Capabilities: caps}); err != nil {
		t.Fatal(err)
	}
	n := f.snapshot(t).Nodes[0]
	_, err := f.s.ConfigureNode(context.Background(), f.owner, "tool-catalog", n.ID, domain.NodeConfigureRequest{
		ExpectedVersion: n.Version, Name: n.Name, Capacity: 2,
		Tools: []domain.ToolScheduling{
			{Tool: "codex", Enabled: true, Implement: true, Model: "gpt-6.1-sol"},
			{Tool: "custom-agent", Enabled: true, Implement: true, Model: "flash-test"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, caps
}

func TestTaskToolSelectionPreservesWorkApprovalAndAccounting(t *testing.T) {
	f, caps := taskToolFixture(t)
	ctx := context.Background()
	in := domain.TaskToolRequest{ExpectedVersion: f.task.Version, Tool: "custom-agent", Reason: "Owner selects independent harness"}
	raw, err := ChangeTaskTool(ctx, f.s, f.owner, "selection", f.task.ID, in)
	changed := decodeTest[domain.Task](t, raw, err)
	expected := f.task
	expected.Tool, expected.Version, expected.AuthorizationVersion = changed.Tool, changed.Version, changed.AuthorizationVersion
	if !reflect.DeepEqual(expected, changed) || changed.ApprovedVersion != f.task.ApprovedVersion || changed.AuthorizationVersion <= f.task.AuthorizationVersion {
		t.Fatal("selection reset work, changed content/approval or failed to fence authority", changed)
	}
	replay, err := ChangeTaskTool(ctx, f.s, f.owner, "selection", f.task.ID, in)
	if err != nil || string(replay) != string(raw) {
		t.Fatal("idempotent replay changed", err)
	}
	if _, err = ChangeTaskTool(ctx, f.s, f.owner, "stale", f.task.ID, in); err == nil {
		t.Fatal("stale version changed selection")
	}
	claim, err := f.s.Claim(ctx, f.node, "new-tool", domain.ClaimRequest{Capabilities: caps, AssignmentContextVersion: domain.AssignmentContextVersion}, strings.Repeat("a", 40))
	a := decodeTest[*domain.Assignment](t, claim, err)
	if a == nil || a.Run.Tool != "custom-agent" || a.Run.Model != "flash-test" || a.Run.AuthorizationVersion != changed.AuthorizationVersion {
		t.Fatal("claim did not pin explicit selected machinery", a)
	}
	in.ExpectedVersion = f.snapshot(t).Tasks[0].Version
	in.Tool = "codex"
	if _, err = ChangeTaskTool(ctx, f.s, f.owner, "already-issued", f.task.ID, in); err == nil {
		t.Fatal("changed a running Run's tool")
	}
	if got := f.snapshot(t).Runs[0]; got.Tool != "custom-agent" || got.Model != "flash-test" {
		t.Fatal("selection mutated assigned Run", got)
	}
}

func TestTaskToolSelectionRespectsOwnerAvailabilityAndPause(t *testing.T) {
	f, _ := taskToolFixture(t)
	ctx := context.Background()
	in := domain.TaskToolRequest{ExpectedVersion: f.task.Version, Tool: "custom-agent", Reason: "explicit owner selection"}
	for _, actor := range []Actor{f.node, {ID: "chat", Role: "chat"}} {
		if _, err := ChangeTaskTool(ctx, f.s, actor, ID("forbidden"), f.task.ID, in); err == nil {
			t.Fatal("non-owner configured execution")
		}
	}
	for _, tool := range []string{"missing-tool", "../invalid"} {
		in.Tool = tool
		if _, err := ChangeTaskTool(ctx, f.s, f.owner, ID("unavailable"), f.task.ID, in); err == nil {
			t.Fatal("unavailable or invalid tool selected")
		}
	}
	_, err := f.s.TaskAction(ctx, f.owner, "hold", f.task.ID, "pause", domain.VersionRequest{ExpectedVersion: f.task.Version})
	if err != nil {
		t.Fatal(err)
	}
	held := f.snapshot(t).Tasks[0]
	in.ExpectedVersion, in.Tool = held.Version, "custom-agent"
	raw, err := ChangeTaskTool(ctx, f.s, f.owner, "held-selection", held.ID, in)
	changed := decodeTest[domain.Task](t, raw, err)
	if changed.HoldReason != "paused" || len(f.snapshot(t).Runs) != 0 {
		t.Fatal("configuration resumed paused execution", changed)
	}
}

func TestTaskToolSelectionAndClaimRemainAtomic(t *testing.T) {
	for iteration := 0; iteration < 8; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			f, caps := taskToolFixture(t)
			var wg sync.WaitGroup
			var claim json.RawMessage
			var claimErr, selectErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, selectErr = ChangeTaskTool(context.Background(), f.s, f.owner, "select-race", f.task.ID,
					domain.TaskToolRequest{ExpectedVersion: f.task.Version, Tool: "custom-agent", Reason: "select before dispatch"})
			}()
			go func() {
				defer wg.Done()
				claim, claimErr = f.s.Claim(context.Background(), f.node, "claim-race",
					domain.ClaimRequest{Capabilities: caps, AssignmentContextVersion: domain.AssignmentContextVersion}, strings.Repeat("a", 40))
			}()
			wg.Wait()
			a := decodeTest[*domain.Assignment](t, claim, claimErr)
			if a == nil {
				t.Fatal("race lost the work")
			}
			snapshot := f.snapshot(t)
			if len(snapshot.Runs) != 1 || snapshot.Tasks[0].Tool != a.Run.Tool || snapshot.Tasks[0].AuthorizationVersion != a.Run.AuthorizationVersion {
				t.Fatal("mixed tool authority crossed dispatch transaction", snapshot.Tasks, snapshot.Runs)
			}
			if (selectErr == nil) != (a.Run.Tool == "custom-agent") {
				t.Fatal("claim did not use the winning transaction's selection", selectErr, a)
			}
		})
	}
}
