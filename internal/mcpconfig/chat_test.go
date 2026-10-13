package mcpconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatSetupPreservesBootstrapAndDifferentChatConflict(t *testing.T) {
	for _, client := range []string{"claude", "cursor", "kimi", "pi", "generic", "dsh"} {
		t.Run(client, func(t *testing.T) {
			options, deps := fixture(t, client)
			options.Apply = true
			options.Mode = "bootstrap"
			if client == "kimi" {
				if e := os.Mkdir(filepath.Join(options.Home, ".kimi-code"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			if client == "pi" {
				deps.lookPath = func(string) (string, error) { return "/fake/pi", nil }
				deps.command = func(context.Context, string, []string, string) ([]byte, error) { return []byte("0.99.0"), nil }
			}
			first := one(t, options, deps)
			if first.Status != "installed" {
				t.Fatal(first)
			}
			bootstrap := read(t, first.Path)
			options.Mode = "chat"
			options.RunnerData = ""
			options.ChatProfile = filepath.Join(options.Home, "limited-private.json")
			// The configuration layer must not open the credential profile.
			put(t, options.ChatProfile, []byte(`{"token":"fixture-do-not-read-or-export"}`))
			second := one(t, options, deps)
			if second.Status != "installed" {
				t.Fatal(second)
			}
			if !strings.Contains(second.Content, "coordica-chat") && (client == "dsh" || client == "generic") {
				t.Fatal(second)
			}
			raw := read(t, second.Path)
			if strings.Contains(string(raw), "fixture-do-not-read-or-export") || !strings.Contains(string(raw), "--chat-profile") {
				t.Fatal("credential/config leak", string(raw))
			}
			if client == "generic" || client == "dsh" {
				if string(read(t, first.Path)) != string(bootstrap) {
					t.Fatal("bootstrap export changed")
				}
			} else {
				var doc map[string]json.RawMessage
				_ = json.Unmarshal(raw, &doc)
				var servers map[string]json.RawMessage
				_ = json.Unmarshal(doc["mcpServers"], &servers)
				if len(servers) != 2 || servers["coordica"] == nil || servers["coordica-chat"] == nil {
					t.Fatal("bootstrap replaced", string(raw))
				}
			}
			if third := one(t, options, deps); third.Status != "unchanged" {
				t.Fatal(third)
			}
			options.ChatProfile = filepath.Join(options.Home, "different.json")
			if conflict := one(t, options, deps); conflict.Status != "conflict" {
				t.Fatal(conflict)
			}
		})
	}
}
func TestCodexChatSetupDistinctNameAndSafeArguments(t *testing.T) {
	options, deps := fixture(t, "codex")
	options.Mode = "chat"
	options.RunnerData = ""
	options.ChatProfile = filepath.Join(options.Home, "profile.json")
	options.Apply = true
	deps.lookPath = func(string) (string, error) { return "/fake/codex", nil }
	added := false
	deps.command = func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if len(args) > 2 && args[1] == "add" {
			if args[2] != "coordica-chat" || strings.Contains(strings.Join(args, " "), "--bootstrap") {
				t.Fatal(args)
			}
			added = true
			return nil, nil
		}
		entries := []any{map[string]any{"name": "coordica", "enabled": true, "transport": map[string]any{"type": "stdio", "command": "/existing/coordica", "args": []string{"mcp", "--bootstrap"}}}}
		if added {
			var launch map[string]any
			_ = json.Unmarshal(launcher(options, true), &launch)
			entries = append(entries, map[string]any{"name": "coordica-chat", "enabled": true, "transport": launch})
		}
		return json.Marshal(entries)
	}
	if result := one(t, options, deps); result.Status != "installed" {
		t.Fatal(result)
	}
	if result := one(t, options, deps); result.Status != "unchanged" {
		t.Fatal(result)
	}
}
