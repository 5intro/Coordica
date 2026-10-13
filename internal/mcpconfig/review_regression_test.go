package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCodexMigrationSeparatesFinalRetainedLineAndPreservesArrayTables(t *testing.T) {
	for _, mode := range []string{"bootstrap", "chat"} {
		for _, tail := range []string{"enabled = false", "# retained final comment"} {
			t.Run(mode+tail, func(t *testing.T) {
				o, _ := fixture(t, "codex")
				o.Mode = "interaction"
				old := o
				old.Mode = mode
				name := "coordica"
				if mode == "chat" {
					old.RunnerData = ""
					old.ChatProfile = filepath.Join(o.Home, "shared.json")
					name = "coordica-chat"
				}
				command, _ := json.Marshal(o.Executable)
				args, _ := json.Marshal(arguments(old))
				unrelated := "[[skills.config]] # preserved\npath = '/fixture/skill'\nenabled = true\n\n[[skills.config]]\npath = '/fixture/other'\nenabled = false\n"
				raw := []byte(unrelated + "[mcp_servers." + name + "]\ncommand = " + string(command) + "\nargs = " + string(args) + "\n" + tail)
				list, _ := json.Marshal([]any{map[string]any{"name": name, "enabled": false, "transport": json.RawMessage(launcher(old, true))}})
				got, _, _, err := migrateCodexTOML(raw, list, o)
				if err != nil || !bytes.HasPrefix(got, []byte(unrelated)) || !bytes.Contains(got, []byte(tail+"\ncommand = ")) {
					t.Fatalf("migration: %s, %v", got, err)
				}
				blocks, err := codexBlocks(got)
				if err != nil {
					t.Fatal(err)
				}
				entry, err := blockLauncher(blocks[len(blocks)-1])
				if err != nil || !bytes.Contains(entry, []byte("--interaction")) {
					t.Fatalf("invalid launcher %s: %v", entry, err)
				}
				for _, header := range []string{"[[mcp_servers]]", "[[mcp_servers.coordica]]", "[[mcp_servers.'coordica-chat'.env]]"} {
					if _, _, _, err = migrateCodexTOML(append([]byte(header+"\n"), raw...), list, o); err == nil {
						t.Fatal("owned array grammar accepted", header)
					}
				}
			})
		}
	}
}

func TestJSONMigrationProjectDisableWinsAndReferencesDeduplicate(t *testing.T) {
	for _, bootstrap := range []bool{false, true} {
		o, _ := fixture(t, "claude")
		o.Mode = "interaction"
		o.RunnerData = ""
		chat := o
		chat.Mode = "chat"
		chat.ChatProfile = filepath.Join(o.Home, "shared.json")
		servers := map[string]json.RawMessage{"coordica-chat": launcher(chat, false)}
		if bootstrap {
			old := o
			old.Mode = "bootstrap"
			servers["coordica"] = launcher(old, false)
		}
		root := map[string]json.RawMessage{
			"disabledMcpServers":    json.RawMessage(`["coordica","coordica-chat","other"]`),
			"enabledMcpjsonServers": json.RawMessage(`["coordica-chat","coordica","other"]`),
			"projects":              json.RawMessage(`{"/disabled":{"disabledMcpServers":["coordica-chat"],"enabledMcpjsonServers":["coordica"],"disabledMcpjsonServers":["coordica-chat","coordica"],"hasTrustDialogAccepted":false,"enableAllProjectMcpServers":false,"notes":"coordica-chat","precise":9007199254740993},"/unrelated":{"enabledMcpjsonServers":["other"],"hasTrustDialogAccepted":true,"notes":"coordica-chat","raw":"<local>\u003e","number":9007199254740993}}`),
		}
		root["mcpServers"], _ = json.Marshal(servers)
		raw, _ := marshalClientSettings(root)
		got, _, _, err := mergeInteractionJSON(raw, o, false)
		if err != nil {
			t.Fatal(err)
		}
		var after map[string]json.RawMessage
		if err = object(got, &after); err != nil {
			t.Fatal(err)
		}
		assertNames := func(fields map[string]json.RawMessage, key string, want []string) {
			t.Helper()
			var actual []string
			json.Unmarshal(fields[key], &actual)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("%s = %v, want %v", key, actual, want)
			}
		}
		assertNames(after, "disabledMcpServers", []string{"coordica", "other"})
		assertNames(after, "enabledMcpjsonServers", []string{"other"})
		var projects, disabled map[string]json.RawMessage
		object(after["projects"], &projects)
		object(projects["/disabled"], &disabled)
		assertNames(disabled, "disabledMcpServers", []string{"coordica"})
		assertNames(disabled, "disabledMcpjsonServers", []string{"coordica"})
		assertNames(disabled, "enabledMcpjsonServers", []string{})
		for key, want := range map[string]string{"hasTrustDialogAccepted": "false", "enableAllProjectMcpServers": "false", "notes": `"coordica-chat"`, "precise": "9007199254740993"} {
			if string(disabled[key]) != want {
				t.Fatal("unrelated project value changed", key, string(disabled[key]))
			}
		}
		var beforeProjects map[string]json.RawMessage
		object(root["projects"], &beforeProjects)
		var a, b any
		json.Unmarshal(projects["/unrelated"], &a)
		json.Unmarshal(beforeProjects["/unrelated"], &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("unrelated project changed")
		}
		var originalCompact, currentCompact bytes.Buffer
		json.Compact(&originalCompact, beforeProjects["/unrelated"])
		json.Compact(&currentCompact, projects["/unrelated"])
		if !bytes.Equal(originalCompact.Bytes(), currentCompact.Bytes()) {
			t.Fatal("unrelated project value bytes changed", string(projects["/unrelated"]))
		}

		for _, bad := range []string{`{"/bad":{"disabledMcpServers":null}}`, `{"/bad":{"enabledMcpjsonServers":true}}`, `{"/bad":false}`} {
			root["projects"] = json.RawMessage(bad)
			raw, _ = json.Marshal(root)
			if _, _, _, err := mergeInteractionJSON(raw, o, false); err == nil {
				t.Fatal("ambiguous project state accepted", bad)
			}
		}
	}
}

func TestLegacyImplicitRunnerRespectsLauncherEnvironment(t *testing.T) {
	o, _ := fixture(t, "claude")
	o.Mode = "interaction"
	o.RunnerData = ""
	old := o
	old.Mode = "bootstrap"
	var entry map[string]json.RawMessage
	object(launcher(old, false), &entry)
	base := filepath.Join(o.Home, "previous-config")
	env := map[string]string{"CODEX_HOME": "/fixture/codex", "OTHER_OPTION": "preserved"}
	switch runtime.GOOS {
	case "darwin", "ios":
		env["HOME"] = filepath.Join(o.Home, "previous-home")
		base = filepath.Join(env["HOME"], "Library", "Application Support")
	case "windows":
		env["APPDATA"] = base
		env["USERPROFILE"] = filepath.Join(o.Home, "previous-home")
	default:
		env["XDG_CONFIG_HOME"] = base
		env["HOME"] = filepath.Join(o.Home, "previous-home")
	}
	entry["env"], _ = json.Marshal(env)
	legacy, _ := json.Marshal(entry)
	root := map[string]any{"mcpServers": map[string]json.RawMessage{"coordica": legacy}}
	raw, _ := json.Marshal(root)
	got, selected, _, err := mergeInteractionJSON(raw, o, false)
	if err != nil || selected.RunnerData != filepath.Join(base, "Coordica", "runner") {
		t.Fatal(selected, err)
	}
	var after, servers, fields map[string]json.RawMessage
	object(got, &after)
	object(after["mcpServers"], &servers)
	object(servers["coordica"], &fields)
	if !bytes.Equal(fields["env"], entry["env"]) { // whitespace is formatting only; values must be exact.
		var a, b map[string]string
		json.Unmarshal(fields["env"], &a)
		json.Unmarshal(entry["env"], &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("launcher env changed")
		}
	}
	// Relative platform overrides cannot establish an absolute identity.
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
		env[key] = "relative"
	}
	entry["env"], _ = json.Marshal(env)
	legacy, _ = json.Marshal(entry)
	root["mcpServers"] = map[string]json.RawMessage{"coordica": legacy}
	raw, _ = json.Marshal(root)
	if _, _, _, err := mergeInteractionJSON(raw, o, false); err == nil {
		t.Fatal("ambiguous implicit identity accepted")
	}
	// An explicit old Runner path is authoritative even with environment overrides.
	old.RunnerData = filepath.Join(o.Home, "fixed")
	object(launcher(old, false), &entry)
	entry["env"], _ = json.Marshal(env)
	legacy, _ = json.Marshal(entry)
	root["mcpServers"] = map[string]json.RawMessage{"coordica": legacy}
	raw, _ = json.Marshal(root)
	if _, selected, _, err := mergeInteractionJSON(raw, o, false); err != nil || selected.RunnerData != old.RunnerData {
		t.Fatal(selected, err)
	}
}

func TestAutoSetupPreflightFailureDoesNotMigrateOtherClient(t *testing.T) {
	o, deps, first := legacyFixture(t, "claude")
	before := read(t, first.Path)
	cursorPath := filepath.Join(o.Home, ".cursor", "mcp.json")
	invalid := []byte(`{"mcpServers":{"coordica":{"command":"unowned","args":[]}}}`)
	put(t, cursorPath, invalid)
	o.Client = "auto"
	o.Mode = "interaction"
	o.RunnerData = ""
	o.Apply = true
	results, err := run(context.Background(), o, deps)
	if err != nil {
		t.Fatal(err)
	}
	conflict := false
	for _, r := range results {
		conflict = conflict || r.Status == "conflict"
		if r.Status == "installed" {
			t.Fatal("preflight wrote a client", results)
		}
	}
	if !conflict || !bytes.Equal(read(t, first.Path), before) || !bytes.Equal(read(t, cursorPath), invalid) {
		t.Fatal("preflight mutation", results)
	}
	if backups, _ := filepath.Glob(filepath.Join(filepath.Dir(cursorPath), "*.coordica-backup-*")); len(backups) > 0 {
		t.Fatal("failed preflight backed up config")
	}
}

func TestCodexRediscoveryRejectsLostDisabledState(t *testing.T) {
	o, deps := fixture(t, "codex")
	o.Mode = "interaction"
	o.Apply = true
	old := o
	old.Mode = "bootstrap"
	command, _ := json.Marshal(o.Executable)
	args, _ := json.Marshal(arguments(old))
	path := filepath.Join(o.Home, ".codex", "config.toml")
	original := []byte("[mcp_servers.coordica]\ncommand = " + string(command) + "\nargs = " + string(args) + "\nenabled = false")
	put(t, path, original)
	calls := 0
	deps.lookPath = func(string) (string, error) { return "/fixture/codex", nil }
	deps.command = func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"mcp", "list", "--json"}) {
			t.Fatal("unexpected config command", args)
		}
		calls++
		current := old
		enabled := false
		if calls > 1 {
			current = interactionDefaults(o)
			enabled = true
		}
		return json.Marshal([]any{map[string]any{"name": "coordica", "enabled": enabled, "transport": json.RawMessage(launcher(current, true))}})
	}
	result := one(t, o, deps)
	if result.Status != "error" || !strings.Contains(result.Message, "rediscovery failed") || calls != 2 {
		t.Fatal("enabled drift falsely verified", result, calls)
	}
	if !bytes.Contains(read(t, path), []byte("enabled = false\ncommand")) {
		t.Fatal("disabled state was not written")
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.toml.coordica-backup-*"))
	if len(backups) != 1 || !bytes.Equal(read(t, backups[0]), original) {
		t.Fatal("original backup lost")
	}
}
