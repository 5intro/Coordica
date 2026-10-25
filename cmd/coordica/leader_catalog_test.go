package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"coordica/internal/agent"
)

func TestLeaderCatalogReportsIncompleteToolWithoutHidingHealthySelection(t *testing.T) {
	t.Setenv("COORDICA_CATALOG_STATE_DIR", t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	definitions := []agent.Definition{}
	for _, id := range []string{"malformed", "normal"} {
		definitions = append(definitions, agent.Definition{ID: id, Protocol: agent.ProtocolACP,
			Executables: []string{executable}, Cancel: true, Review: true, CloseMethod: "session/close",
			Resume: true, ResumeMethod: "session/resume",
			VersionArgs: []string{"-test.run=^TestLeaderProtocolFixture$", "--", "coordica-leader-fixture", "fixture-version"},
			StartArgs:   []string{"-test.run=^TestLeaderProtocolFixture$", "--", "coordica-leader-fixture", "fixture-" + id}})
	}
	registry, err := agent.NewRegistry(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	executor := &AgentPlanningExecutor{Registry: registry}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	caps, err := executor.DiscoverLeaders(ctx)
	if err != nil || len(caps) != 2 || caps[0].ModelDiscovery != "unavailable" ||
		!strings.Contains(caps[0].Detail, "incomplete") || caps[1].ModelDiscovery != "reported" || len(caps[1].Models) != 2 {
		t.Fatal("incomplete discovery hid a healthy selection or looked complete", caps, err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := executor.DiscoverLeaders(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request returned a seemingly complete catalog", err)
	}
}
