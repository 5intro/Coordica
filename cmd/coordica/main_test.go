package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"coordica/internal/agent"
	"coordica/internal/mcp"
)

func TestBootstrapProfileIsLocalConfigurationOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Agent Tools", "工具")
	profile := mcp.BootstrapProfile{Name: "laptop", Platform: runtime.GOOS + "/" + runtime.GOARCH, Executables: map[string]string{"codex": path}}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeBootstrapProfile(raw)
	if err != nil || got.Executables["codex"] != path {
		t.Fatalf("profile = %+v, error = %v", got, err)
	}
	for _, raw := range []string{
		`null`,
		`{"platform":"another/platform"}`,
		`{"platform":"` + profile.Platform + `","token":"do-not-import"}`,
		`{"platform":"` + profile.Platform + `","executables":{"shell":""}}`,
		`{"platform":"` + profile.Platform + `","executables":{"codex":"relative/tool"}}`,
		`{"platform":"` + profile.Platform + `"} {}`,
	} {
		if _, err := decodeBootstrapProfile([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid profile %s", raw)
		}
	}
}

func TestCustomToolConfigurationAndGenericExecutableFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Agent Tools", "custom")
	custom := agent.Definition{ID: "future-agent", Executables: []string{path}, Protocol: agent.ProtocolCodex, Cancel: true, Review: true}
	profile := mcp.BootstrapProfile{Platform: runtime.GOOS + "/" + runtime.GOARCH, Tools: []agent.Definition{custom}, Executables: map[string]string{custom.ID: path}}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBootstrapProfile(raw)
	if err != nil || len(decoded.Tools) != 1 || decoded.Executables[custom.ID] != path {
		t.Fatalf("custom profile lost: %+v %v", decoded, err)
	}
	defsFile := filepath.Join(t.TempDir(), "tools.json")
	raw, err = json.Marshal([]agent.Definition{custom})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defsFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	definitions, err := loadToolDefinitions(defsFile)
	if err != nil || len(definitions) != 1 || definitions[0].ID != custom.ID {
		t.Fatal("custom definitions did not load", err)
	}
	if err := os.WriteFile(defsFile, []byte(`null`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadToolDefinitions(defsFile); err == nil {
		t.Fatal("null definitions accepted")
	}
	duplicate := `[{"id":"future-agent","id":"other-agent","executables":["custom"],"protocol":"codex-jsonl"}]`
	if err := os.WriteFile(defsFile, []byte(duplicate), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadToolDefinitions(defsFile); err == nil {
		t.Fatal("duplicate definition fields accepted")
	}
	var flags executableFlags
	if err := flags.Set(custom.ID + "=" + path); err != nil {
		t.Fatal(err)
	}
	if flags[custom.ID] != path {
		t.Fatal("generic flag lost executable")
	}
	for _, invalid := range []string{custom.ID + "=" + path, "../tool=" + path, "tool=", "tool"} {
		if err := flags.Set(invalid); err == nil {
			t.Fatal("accepted invalid/duplicate executable flag", invalid)
		}
	}
}

func TestMCPStartupRejectsConflictingRoleAssetPathsBeforeServing(t *testing.T) {
	dir := t.TempDir()
	runnerDir := filepath.Join(dir, "runner")
	chat := filepath.Join(dir, "shared-chat.json")
	cases := [][]string{
		{"--interaction", "--runner-data", runnerDir, "--chat-profile", chat, "--profile", chat},
		{"--interaction", "--runner-data", runnerDir, "--chat-profile", filepath.Join(runnerDir, "runner.json")},
		{"--interaction", "--runner-data", runnerDir, "--chat-profile", chat, "--profile", filepath.Join(runnerDir, "runner.json")},
		{"--bootstrap", "--runner-data", runnerDir, "--profile", filepath.Join(runnerDir, "runner.json")},
	}
	for _, args := range cases {
		if err := serveMCP(context.Background(), args); err == nil || !strings.Contains(err.Error(), "profile") && !strings.Contains(err.Error(), "role assets") {
			t.Fatal("conflicting startup was not rejected", args, err)
		}
	}
	if _, err := os.Stat(runnerDir); !os.IsNotExist(err) {
		t.Fatal("startup preflight mutated Runner storage", err)
	}
	if _, err := os.Stat(chat); !os.IsNotExist(err) {
		t.Fatal("startup preflight mutated chat profile", err)
	}
}
