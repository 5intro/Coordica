package domain

import (
	"strings"
	"testing"
)

func TestModelReportsAreBoundedAndEffortsBelongToOneModel(t *testing.T) {
	valid := []ModelOption{{ID: "sonnet[1m]", Name: "Reported model", ReasoningEfforts: []string{"high", "ultra", "0"}, DefaultReasoningEffort: "high"}}
	if err := ValidateModelOptions(valid); err != nil {
		t.Fatal(err)
	}
	cap := Capability{Available: true, ModelDiscovery: "reported", Models: valid}
	if err := ValidateExecutionOptions(cap, "sonnet[1m]", "ultra"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct{ model, effort string }{{"unreported", "high"}, {"sonnet[1m]", "made-up"}, {"", "ultra"}} {
		if ValidateExecutionOptions(cap, invalid.model, invalid.effort) == nil {
			t.Fatal("unsupported configuration accepted")
		}
	}
	for _, models := range [][]ModelOption{append(valid, valid[0]), {{ID: "--injected", ReasoningEfforts: []string{"high"}}}, {{ID: "model", ReasoningEfforts: []string{"high", "high"}}}, {{ID: "model", ReasoningEfforts: []string{"high"}, DefaultReasoningEffort: "ultra"}}, {{ID: "model", Name: strings.Repeat("x", 257)}}, make([]ModelOption, 65)} {
		if ValidateModelOptions(models) == nil {
			t.Fatal("malformed catalog accepted")
		}
	}
	if ValidateCapabilities([]Capability{{Tool: "codex", Models: valid, ModelDiscovery: "unavailable"}}) == nil {
		t.Fatal("failed discovery advertised a catalog")
	}
}
