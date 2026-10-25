package runner

import (
	"context"
	"os"
	"strings"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/process"
)

func TestRegisteredGoalToolsUseStructuredScopeHandoff(t *testing.T) {
	for _, tool := range []string{"dsh", "claude", "pi", "custom-acp"} {
		t.Run(tool, func(t *testing.T) {
			f := newIntentFixture(t)
			if tool == "custom-acp" {
				if err := f.r.registry.Register(agent.Definition{ID: tool, Executables: []string{"unused"},
					Protocol: agent.ProtocolACP, CommitMode: "runner", Cancel: true, Review: true}); err != nil {
					t.Fatal(err)
				}
			}
			f.record.Assignment.Run.Tool, f.record.Assignment.Run.Model = tool, "provider/flash"
			f.r.execute = func(_ context.Context, req agent.Request, _ func(agent.Event)) (agent.Result, error) {
				if req.Tool != tool || req.Model != "provider/flash" || !strings.Contains(req.Prompt, nativeIntentPrompt) {
					t.Fatal("registered tool lost its selection or intent contract")
				}
				if tool == "claude" {
					data, err := os.ReadFile(req.OutputSchemaPath)
					if err != nil || string(data) != string(executionOutputSchema) {
						t.Fatal("native schema was not supplied", err)
					}
				} else if req.OutputSchemaPath != "" || !strings.Contains(req.Prompt, string(executionOutputSchema)) {
					t.Fatal("RPC intent did not carry the trusted schema")
				}
				return agent.Result{ExitCode: 0, TreeStopped: true, OutputText: scopeIntentText()}, nil
			}
			f.perform(context.Background())
			if snap := f.snapshot(t); f.scopeCalls != 1 || f.artifactCalls != 0 || len(snap.ScopeRequests) != 1 ||
				snap.Runs[0].State != "stopped" || !snap.Runs[0].Quiescent {
				t.Fatal("registered tool bypassed the stopped scope handoff", snap)
			}
		})
	}
}

func TestUnstoppedModelPreflightRetainsRunOccupancy(t *testing.T) {
	f := newIntentFixture(t)
	f.r.execute = func(context.Context, agent.Request, func(agent.Event)) (agent.Result, error) {
		return agent.Result{}, process.ErrTreeNotStopped
	}
	f.perform(context.Background())
	if snap := f.snapshot(t); snap.Runs[0].Quiescent || f.artifactCalls != 0 {
		t.Fatal("missing start event fabricated a stopped model preflight", snap)
	}
}
