package app

import (
	"context"
	"coordica/internal/domain"
	"coordica/internal/store"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func modelCapability() domain.Capability {
	return domain.Capability{Tool: "codex", Protocol: "codex-jsonl", Available: true, Structured: true, Cancel: true, Review: true, ModelDiscovery: "reported", Models: []domain.ModelOption{{ID: "gpt-6.1-sol", Name: "Sol", ReasoningEfforts: []string{"high", "ultra"}}, {ID: "gpt-6-astra", Name: "Astra", ReasoningEfforts: []string{"medium", "ultra"}}}}
}
func configureExecution(t *testing.T, f *serviceFixture, key, model, effort string) (domain.Node, error) {
	t.Helper()
	node := f.snapshot(t).Nodes[0]
	raw, err := f.s.ConfigureNode(context.Background(), f.owner, key, node.ID, domain.NodeConfigureRequest{ExpectedVersion: node.Version, Name: node.Name, Capacity: node.Capacity, Tools: []domain.ToolScheduling{{Tool: "codex", Enabled: true, Implement: true, Review: true, Model: model, ReasoningEffort: effort}}})
	if err != nil {
		return domain.Node{}, err
	}
	return decodeTest[domain.Node](t, raw, nil), nil
}
func TestExecutionPreferencesAreFencedPinnedAndAppliedToNextRun(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	caps := []domain.Capability{modelCapability()}
	// A nonmatching tool report refreshes capabilities without allocating work.
	caps[0].Available = false
	raw, err := f.s.Claim(ctx, f.node, "report", domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40))
	if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
		t.Fatal("unavailable tool received work")
	}
	if _, err := configureExecution(t, f, "unavailable", "gpt-6-astra", "ultra"); err == nil {
		t.Fatal("unavailable catalog accepted")
	}
	caps[0].Available = true
	// Pausing only controls fixture dispatch; it does not confer new authority.
	if _, err = f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: f.snapshot(t).Project.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Claim(ctx, f.node, "catalog", domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	node, err := configureExecution(t, f, "configure", "gpt-6-astra", "ultra")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ConfigureNode(ctx, f.node, "forbidden", node.ID, domain.NodeConfigureRequest{}); err == nil {
		t.Fatal("node changed owner preferences")
	}
	if _, err = f.s.ConfigureNode(ctx, f.owner, "stale", node.ID, domain.NodeConfigureRequest{ExpectedVersion: node.Version - 1, Name: node.Name, Capacity: 2}); err == nil {
		t.Fatal("stale version accepted")
	}
	if _, err = f.s.ProjectAction(ctx, f.owner, "unpause", "resume", domain.VersionRequest{ExpectedVersion: f.snapshot(t).Project.Version}); err != nil {
		t.Fatal(err)
	}
	raw, err = f.s.Claim(ctx, f.node, "first", domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40))
	first := decodeTest[*domain.Assignment](t, raw, err)
	if first == nil || first.Run.Model != "gpt-6-astra" || first.Run.ReasoningEffort != "ultra" {
		t.Fatalf("%+v", first)
	}
	if _, err = configureExecution(t, f, "edit", "gpt-6.1-sol", "high"); err != nil {
		t.Fatal(err)
	}
	if firstRun := f.snapshot(t).Runs[0]; firstRun.Model != "gpt-6-astra" || firstRun.ReasoningEffort != "ultra" {
		t.Fatal("edit changed in-flight Run")
	}
	f.now = f.now.Add(time.Second)
	if _, err = f.s.Complete(ctx, f.node, "failed", first.Run.ID, domain.CompleteRequest{Generation: first.Run.Generation, Status: "transient_failure", Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	raw, err = f.s.Claim(ctx, f.node, "second", domain.ClaimRequest{Capabilities: caps}, strings.Repeat("a", 40))
	second := decodeTest[*domain.Assignment](t, raw, err)
	if second == nil || second.Run.Model != "gpt-6.1-sol" || second.Run.ReasoningEffort != "high" || second.Run.Generation <= first.Run.Generation {
		t.Fatalf("%+v", second)
	}
	f.s.Complete(ctx, f.node, "late", first.Run.ID, domain.CompleteRequest{Generation: first.Run.Generation, Status: "transient_failure", Quiescent: true})
	if got := f.snapshot(t); got.Tasks[0].CurrentRunID != second.Run.ID || got.Tasks[0].Generation != second.Run.Generation {
		t.Fatal("late result affected next Run")
	}
}
func TestInvalidCatalogOrSelectionCannotAllocateOrReserve(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	cap := modelCapability()
	if _, err := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: f.snapshot(t).Project.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Claim(ctx, f.node, "catalog", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{"made-up", "ultra\"\nmalicious=true"} {
		if _, err := configureExecution(t, f, ID("bad"), "gpt-6.1-sol", effort); err == nil {
			t.Fatal("unreported effort accepted")
		}
	}
	if _, err := configureExecution(t, f, "valid", "gpt-6.1-sol", "ultra"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProjectAction(ctx, f.owner, "unpause", "resume", domain.VersionRequest{ExpectedVersion: f.snapshot(t).Project.Version}); err != nil {
		t.Fatal(err)
	}
	cap.Models[0].ReasoningEfforts = []string{"high"}
	before := f.snapshot(t)
	raw, err := f.s.Claim(ctx, f.node, "removed", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40))
	if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
		t.Fatal("removed effort dispatched")
	}
	after := f.snapshot(t)
	if len(after.Runs) != len(before.Runs) || after.Project.ReservedSeconds != before.Project.ReservedSeconds {
		t.Fatal("unsupported preference reserved work")
	}
	node := after.Nodes[0]
	bad := domain.NodeConfigureRequest{ExpectedVersion: node.Version, Name: node.Name, Capacity: 2, Tools: []domain.ToolScheduling{{Tool: "codex", Model: "unreported-model"}}}
	if _, err = f.s.ConfigureNode(ctx, f.owner, "unreported", node.ID, bad); err == nil {
		t.Fatal("unreported model accepted")
	}
	if _, err = configureExecution(t, f, "alternate", "gpt-6-astra", "medium"); err != nil {
		t.Fatal(err)
	}
	cap.Models = cap.Models[:1]
	before = f.snapshot(t)
	raw, err = f.s.Claim(ctx, f.node, "model-removed", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40))
	if got := decodeTest[*domain.Assignment](t, raw, err); got != nil {
		t.Fatal("removed model dispatched or fell back to default")
	}
	after = f.snapshot(t)
	if len(after.Runs) != len(before.Runs) || after.Project.ReservedSeconds != before.Project.ReservedSeconds || after.Tasks[0].Retries != before.Tasks[0].Retries {
		t.Fatal("removed model allocated, reserved or consumed retry")
	}
}

func TestObsoleteExecutionSelectionAllowsOnlyPermissionReductions(t *testing.T) {
	for _, permission := range []string{"enabled", "implement", "review"} {
		t.Run(permission, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			if _, e := f.s.ProjectAction(ctx, f.owner, "pause", "pause", domain.VersionRequest{ExpectedVersion: f.snapshot(t).Project.Version}); e != nil {
				t.Fatal(e)
			}
			cap := modelCapability()
			if _, e := f.s.Claim(ctx, f.node, "catalog", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40)); e != nil {
				t.Fatal(e)
			}
			n, e := configureExecution(t, f, "valid", "gpt-6.1-sol", "ultra")
			if e != nil {
				t.Fatal(e)
			}
			cap.Models[0].ReasoningEfforts = []string{"high"}
			if _, e = f.s.Claim(ctx, f.node, "changed-catalog", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40)); e != nil {
				t.Fatal(e)
			}
			n = f.snapshot(t).Nodes[0]
			old := n.Tools[0]
			next := old
			switch permission {
			case "enabled":
				next.Enabled = false
			case "implement":
				next.Implement = false
			case "review":
				next.Review = false
			}
			request := domain.NodeConfigureRequest{ExpectedVersion: n.Version, Name: n.Name, Capacity: n.Capacity, Tools: []domain.ToolScheduling{next}}
			raw, e := f.s.ConfigureNode(ctx, f.owner, "reduce", n.ID, request)
			updated := decodeTest[domain.Node](t, raw, e)
			if updated.Tools[0] != next {
				t.Fatal("permission reduction lost")
			}
			request.ExpectedVersion = updated.Version
			request.Tools = []domain.ToolScheduling{old}
			if _, e = f.s.ConfigureNode(ctx, f.owner, "increase", n.ID, request); e == nil {
				t.Fatal("stale parameters regained execution permission")
			}
		})
	}
}

func TestMissingCodexDefaultCannotAllocateOrReserve(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			f := newServiceFixture(t)
			cap := modelCapability()
			cap.Models = cap.Models[1:]
			if configured {
				if e := f.s.DB.Update(context.Background(), func(tx *store.Tx) error {
					var n domain.Node
					if e := tx.Get("node", f.node.NodeID, &n); e != nil {
						return e
					}
					n.Tools = []domain.ToolScheduling{{Tool: "codex", Enabled: true, Implement: true, Review: true}}
					n.SchedulingConfigured = true
					return tx.Put("node", n.ID, n)
				}); e != nil {
					t.Fatal(e)
				}
			}
			before := f.snapshot(t)
			raw, e := f.s.Claim(context.Background(), f.node, "missing-default", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40))
			if a := decodeTest[*domain.Assignment](t, raw, e); a != nil {
				t.Fatal("unsupported default assigned")
			}
			after := f.snapshot(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || len(after.Runs) != 0 || after.Project.ReservedSeconds != before.Project.ReservedSeconds {
				t.Fatal("unsupported default reserved or changed work")
			}
			cap.ModelDiscovery = "unavailable"
			cap.Models = nil
			raw, e = f.s.Claim(context.Background(), f.node, "legacy-default", domain.ClaimRequest{Capabilities: []domain.Capability{cap}}, strings.Repeat("a", 40))
			if a := decodeTest[*domain.Assignment](t, raw, e); a == nil || a.Run.Model != "gpt-6.1-sol" {
				t.Fatal("legacy catalog-less default lost")
			}
		})
	}
}
