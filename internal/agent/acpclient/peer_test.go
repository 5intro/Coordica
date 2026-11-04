package acpclient

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestInitializeOnlyPersistsHostLifetimeAndCopiesDeclarations(t *testing.T) {
	t.Setenv("ACP_PARENT_FORBIDDEN", "not-in-child")
	config, journal := fixtureConfig(t, "normal")
	op, cancel := context.WithCancel(t.Context())
	p, err := OpenWithLifetime(t.Context(), op, config)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	cancel()
	info := p.Info()
	if p.Err() != nil || info.ProtocolVersion != 1 || info.AgentInfo == nil || info.AgentInfo.Name != "offline-acp" || !info.AgentCapabilities.LoadSession || !info.AgentCapabilities.Prompt.Image || !info.AgentCapabilities.MCP.HTTP || len(info.AuthMethods) != 1 {
		t.Fatal(info, p.Err())
	}
	info.AgentInfo.Name = "mutated"
	info.AuthMethods[0].ID = "mutated"
	if p.Info().AgentInfo.Name != "offline-acp" || p.Info().AuthMethods[0].ID != "offline-auth" {
		t.Fatal("initialization metadata aliases caller memory")
	}
	status, _ := p.Close()
	if !status.TreeStopped {
		t.Fatal("owned process tree was not stopped")
	}
	raw, err := os.ReadFile(journal)
	if err != nil || strings.Count(string(raw), "\n") != 1 || strings.Contains(string(raw), "initialized") || strings.Contains(string(raw), "session/") || strings.Contains(string(raw), "authenticate") {
		t.Fatalf("extra native operation: %s %v", raw, err)
	}
}
