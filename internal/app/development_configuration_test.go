package app

import (
	"context"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func registeredDevelopmentCapability(tool string) domain.Capability {
	return domain.Capability{Tool: tool, Available: true, Structured: true, Cancel: true, Review: true,
		ModelDiscovery: "reported", Models: []domain.ModelOption{{ID: "provider/flash", ReasoningEfforts: []string{"high"}}}}
}

func TestRegisteredDevelopmentToolIsDerivedClaimedAndModelPinned(t *testing.T) {
	for _, tool := range []string{"dsh", "claude", "studio-agent"} {
		t.Run(tool, func(t *testing.T) {
			ctx := context.Background()
			f, d, result := planningFixture(t, true)
			cap := registeredDevelopmentCapability(tool)
			if err := f.s.ReportNodePresence(ctx, f.node, domain.ClaimRequest{Capabilities: []domain.Capability{cap}}); err != nil {
				t.Fatal(err)
			}
			settings := f.snapshot(t).Automation
			settings.DevelopmentTool, settings.DevelopmentModel = tool, "provider/flash"
			if _, err := f.s.ConfigureAutomation(ctx, f.owner, "select-tool", domain.AutomationRequest{
				ExpectedVersion: settings.Version, Settings: settings,
			}); err != nil {
				t.Fatal("registered non-Codex developer rejected", err)
			}
			scheduleTest(t, f, d, "plan-registered")
			worker := &PlanningWorker{Service: f.s, Executor: resultExecutor(result)}
			if err := worker.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			snap := f.snapshot(t)
			if len(snap.Tasks) != 2 || snap.Tasks[1].Tool != tool || snap.Tasks[1].State != "ready" {
				t.Fatal("selected developer did not reach an admitted task", snap.Tasks)
			}
			req := domain.ClaimRequest{AssignmentContextVersion: domain.AssignmentContextVersion,
				Capabilities: []domain.Capability{cap}}
			// Old Runners must not execute a newly supported goal using legacy prose.
			raw, err := f.s.Claim(ctx, f.node, "legacy-runner", req, strings.Repeat("a", 40))
			if a := decodeTest[*domain.Assignment](t, raw, err); a != nil {
				t.Fatal("old Runner claimed a goal without generic intent support")
			}
			req.ExecutionIntentVersion = domain.ExecutionIntentVersion
			raw, err = f.s.Claim(ctx, f.node, "current-runner", req, strings.Repeat("a", 40))
			assignment := decodeTest[*domain.Assignment](t, raw, err)
			if assignment == nil || assignment.Run.Tool != tool || assignment.Run.Model != "provider/flash" {
				t.Fatal("selected model/tool was lost or defaulted", assignment)
			}
			settings = f.snapshot(t).Automation
			settings.DevelopmentTool, settings.DevelopmentModel = "codex", "gpt-6.1-sol"
			if _, err := f.s.ConfigureAutomation(ctx, f.owner, "change-default", domain.AutomationRequest{
				ExpectedVersion: settings.Version, Settings: settings,
			}); err != nil {
				t.Fatal(err)
			}
			for _, run := range f.snapshot(t).Runs {
				if run.ID == assignment.Run.ID && (run.Tool != tool || run.Model != "provider/flash") {
					t.Fatal("default change rewrote in-flight execution", run)
				}
			}
		})
	}
}

func TestDevelopmentSelectionRejectsUnreportedModelsAndUnauthorizedChanges(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	cap := registeredDevelopmentCapability("studio-agent")
	if err := f.s.ReportNodePresence(ctx, f.node, domain.ClaimRequest{Capabilities: []domain.Capability{cap}}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"unreported", ""} {
		settings := f.snapshot(t).Automation
		settings.DevelopmentTool, settings.DevelopmentModel = cap.Tool, model
		if _, err := f.s.ConfigureAutomation(ctx, f.owner, ID("invalid"), domain.AutomationRequest{
			ExpectedVersion: settings.Version, Settings: settings,
		}); err == nil {
			t.Fatal("unreported development model accepted", model)
		}
	}
	settings := f.snapshot(t).Automation
	settings.DevelopmentTool, settings.DevelopmentModel = cap.Tool, "provider/flash"
	if _, err := f.s.ConfigureAutomation(ctx, f.node, "not-owner", domain.AutomationRequest{
		ExpectedVersion: settings.Version, Settings: settings,
	}); err == nil {
		t.Fatal("node changed project development defaults")
	}
}

func TestDevelopmentDefaultsRespectNodeOverridesAndCatalogRemoval(t *testing.T) {
	settings := domain.DefaultAutomationSettings()
	settings.DevelopmentTool, settings.DevelopmentModel = "studio-agent", "provider/flash"
	cap := registeredDevelopmentCapability(settings.DevelopmentTool)
	cap.Models = append(cap.Models, domain.ModelOption{ID: "provider/other", ReasoningEfforts: []string{"high"}})
	node := domain.Node{Capabilities: []domain.Capability{cap}}
	model, effort, valid := taskExecution(node, cap.Tool, settings)
	if !valid || model != settings.DevelopmentModel || effort != "" {
		t.Fatal("default model was ignored", model, effort, valid)
	}
	node.Tools = []domain.ToolScheduling{{Tool: cap.Tool, Model: "provider/other", ReasoningEffort: "high"}}
	model, effort, valid = taskExecution(node, cap.Tool, settings)
	if !valid || model != "provider/other" || effort != "high" {
		t.Fatal("explicit node selection was replaced by a project default", model, effort, valid)
	}
	node.Capabilities[0].Models = node.Capabilities[0].Models[:1]
	if _, _, valid = taskExecution(node, cap.Tool, settings); valid {
		t.Fatal("removed explicit model silently fell back to the default")
	}
	node.Tools = nil
	node.Capabilities[0].ModelDiscovery = "unavailable"
	if _, _, valid = taskExecution(node, cap.Tool, settings); valid {
		t.Fatal("unavailable catalog silently selected the harness default")
	}
}
