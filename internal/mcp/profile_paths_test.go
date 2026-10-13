package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverySaveCannotOverwriteInteractionOrPairedRoleAliases(t *testing.T) {
	bootstrapFixtures(t, "git")
	dir := t.TempDir()
	sentinel := `{"center_url":"http://127.0.0.1:8080","node_id":"node_saved","token":"private role sentinel","capacity":1}`
	data := filepath.Join(dir, "runner")
	chat := filepath.Join(dir, "interaction.json")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{chat, filepath.Join(data, "runner.json")} {
		if err := os.WriteFile(protected, []byte(sentinel), 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(dir, "linked.json")
	if err := os.Link(chat, alias); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "role-alias")
	if err := os.Symlink(dir, symlink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{chat, filepath.Join(dir, "unused", "..", "interaction.json"), filepath.Join(data, "runner.json"), alias, filepath.Join(symlink, "interaction.json")} {
		if err := ValidateProfilePaths(path, data, chat); err == nil {
			t.Fatal("startup accepted role alias", path)
		}
		replies := bootstrapReplies(t, Bootstrap{ProfilePath: path, DataDir: data, ChatProfile: chat}, bootstrapInitialize,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_environment","arguments":{}}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"save_runner_profile","arguments":{}}}`)
		if !replies[2].Result.IsError {
			t.Fatal("role alias overwritten", path)
		}
	}
	for _, protected := range []string{chat, filepath.Join(data, "runner.json")} {
		raw, err := os.ReadFile(protected)
		if err != nil || string(raw) != sentinel {
			t.Fatal("role asset changed", err)
		}
	}
	if err := ValidateProfilePaths(filepath.Join(dir, "discovery.json"), data, chat); err != nil {
		t.Fatal("custom distinct path rejected", err)
	}
	if err := ValidateProfilePaths(filepath.Join(dir, "discovery.json"), data, filepath.Join(data, "runner.json")); err == nil {
		t.Fatal("chat aliases paired identity")
	}
}
