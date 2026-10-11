package domain

import (
	"strings"
	"testing"
)

func TestToolIdentifiersAndCapabilityBounds(t *testing.T) {
	for _, id := range []string{"codex", "team_custom-9", "future-agent", strings.Repeat("a", 64)} {
		if err := ValidateToolID(id); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, id := range []string{"", "Claude", "0tool", "../tool", "tool with space", "工具", strings.Repeat("a", 65)} {
		if err := ValidateToolID(id); err == nil {
			t.Fatal("accepted", id)
		}
	}
	invalid := [][]Capability{
		{{Tool: "future"}, {Tool: "future"}},
		{{Tool: "../shell"}},
		{{Tool: "future", Protocol: "bad protocol"}},
		{{Tool: "future", Version: strings.Repeat("v", 1025)}},
		{{Tool: "future", Detail: strings.Repeat("d", 4097)}},
		make([]Capability, 65),
	}
	for _, caps := range invalid {
		if err := ValidateCapabilities(caps); err == nil {
			t.Fatalf("accepted malformed capabilities: %+v", caps)
		}
	}
	if err := ValidateCapabilities([]Capability{{Tool: "future", Available: true}}); err != nil {
		t.Fatal(err)
	}
}
