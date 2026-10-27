package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"coordica/internal/domain"
)

func TestOwnerCanSelectReportedHarnessForUnstartedTaskWithoutApprovingIt(t *testing.T) {
	f := newBoundaryFixture(t)
	ctx := context.Background()
	owner, err := f.server.Service.Authenticate(ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.server.Service.CreateProject(ctx, owner, "tool-project", domain.Project{
		Name: "Task tool selection", RepositoryURL: f.server.Service.Dir, TargetBranch: "main",
		Checks: []domain.Check{{Name: "check", Command: "git", Args: []string{"status"}, TimeoutSeconds: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedHTTPBaseline(t, f.server.Service)
	body, err := f.server.Service.CreateTask(ctx, owner, "unstarted", domain.Task{
		Title: "Independent task", Description: "Implement scoped work", Acceptance: "Checks pass",
		AllowedPaths: []string{"src/**"}, Tool: "codex", BudgetSeconds: 600, MaxRunSeconds: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	var original domain.Task
	if err = json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	node, err := f.server.Service.Authenticate(ctx, f.node.Token)
	if err != nil {
		t.Fatal(err)
	}
	caps := []domain.Capability{{Tool: "custom-agent", Protocol: "acp", Available: true,
		Structured: true, Cancel: true, ModelDiscovery: "reported", Models: []domain.ModelOption{{ID: "flash-test", Name: "Flash"}}}}
	if err = f.server.Service.ReportNodePresence(ctx, node, domain.ClaimRequest{Capabilities: caps}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.server.Service.Snapshot(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	n := snapshot.Nodes[0]
	_, err = f.server.Service.ConfigureNode(ctx, owner, "enable-custom", n.ID, domain.NodeConfigureRequest{
		ExpectedVersion: n.Version, Name: n.Name, Capacity: 1,
		Tools: []domain.ToolScheduling{{Tool: "custom-agent", Enabled: true, Implement: true, Model: "flash-test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"expected_version":%d,"tool":"custom-agent","reason":"Owner selects an available execution harness"}`, original.Version)
	path := "/api/v1/tasks/" + original.ID + "/tool"
	w := boundaryRequest(f.server.AdminHandler(), "POST", path, f.owner, payload)
	if w.Code != http.StatusOK {
		t.Fatalf("tool selection: %d %s", w.Code, w.Body.String())
	}
	var changed domain.Task
	if err = json.Unmarshal(w.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.Tool != "custom-agent" || changed.State != "draft" || changed.ApprovedVersion != 0 ||
		changed.UsedSeconds != original.UsedSeconds || changed.BudgetSeconds != original.BudgetSeconds || changed.Description != original.Description {
		t.Fatalf("configuration changed task authority, content or accounting: %+v", changed)
	}
	if w = boundaryRequest(f.server.RunnerHandler(), "POST", path, f.node.Token, payload); w.Code == http.StatusOK {
		t.Fatal("node changed Owner execution selection")
	}
}
