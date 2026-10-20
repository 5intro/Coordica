package domain

import (
	"strings"
	"testing"
)

func TestOpaqueProtocolModelValuesRemainExactAndBounded(t *testing.T) {
	id := `["provider","reported-model"]`
	models := []ModelOption{{ID: id, ReasoningEfforts: []string{"high"}}}
	if err := ValidateModelOptions(models); err != nil {
		t.Fatal(err)
	}
	capability := Capability{Available: true, ModelDiscovery: "reported", Models: models}
	if err := ValidateExecutionOptions(capability, id, "high"); err != nil {
		t.Fatal(err)
	}
	if ValidateExecutionOptions(capability, "provider/reported-model", "high") == nil {
		t.Fatal("opaque identifier was guessed or normalized")
	}
	for _, invalid := range []string{"", "--injected", " model", "model\nother", "model\x00other", string([]byte{255}), strings.Repeat("m", 129)} {
		if ValidateModelOptions([]ModelOption{{ID: invalid}}) == nil {
			t.Fatal("invalid model value accepted")
		}
	}
}

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
