package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertInteractionPaths(t *testing.T, client, content, runnerPath, profilePath string) {
	t.Helper()
	var entry struct {
		Args []string `json:"args"`
	}
	switch client {
	case "generic":
		var root struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(content), &root); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(root.Servers["coordica"], &entry); err != nil {
			t.Fatal(err)
		}
	case "dsh":
		// The generated YAML sequence uses JSON-compatible quoted scalars.
		collecting := false
		for _, line := range strings.Split(content, "\n") {
			if line == "        args:" {
				collecting = true
				continue
			}
			if collecting && strings.HasPrefix(line, "          - ") {
				var arg string
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "          - ")), &arg); err != nil {
					t.Fatal(err)
				}
				entry.Args = append(entry.Args, arg)
			} else if collecting {
				collecting = false
			}
		}
	default:
		if err := json.Unmarshal([]byte(content), &entry); err != nil {
			t.Fatal(err)
		}
	}
	for _, parameter := range []struct{ flag, value string }{
		{"--runner-data", runnerPath},
		{"--chat-profile", profilePath},
	} {
		count := 0
		for i, arg := range entry.Args {
			if arg == parameter.flag {
				count++
				if i+1 >= len(entry.Args) || entry.Args[i+1] != parameter.value {
					t.Fatalf("%s: want %q, got args %q", parameter.flag, parameter.value, entry.Args)
				}
			}
		}
		if count != 1 {
			t.Fatalf("want exactly one %s, got args %q", parameter.flag, entry.Args)
		}
	}
}

func TestInteractionSerializedWindowsPaths(t *testing.T) {
	// Exercise serialization only: these paths need not be absolute on the host OS.
	o := Options{
		Mode:        "interaction",
		Executable:  `C:\Users\Owner\Agent Tools 工具\coordica.exe`,
		RunnerData:  `C:\Users\Owner\Runner Data 数据`,
		ChatProfile: `C:\Users\Owner\shared-private.json`,
	}
	for _, client := range []string{"claude", "cursor", "kimi", "pi", "generic", "dsh", "codex"} {
		t.Run(client, func(t *testing.T) {
			content := launcher(o, true)
			if client == "generic" || client == "dsh" {
				content = exportContent(o, client)
			}
			assertInteractionPaths(t, client, string(content), o.RunnerData, o.ChatProfile)
		})
	}
}

func legacyFixture(t *testing.T, client string) (Options, dependencies, Result) {
	o, deps := fixture(t, client)
	if client == "kimi" {
		os.Mkdir(filepath.Join(o.Home, ".kimi-code"), 0700)
	}
	if client == "pi" {
		deps.lookPath = func(string) (string, error) { return "/fixture/pi", nil }
		deps.command = func(context.Context, string, []string, string) ([]byte, error) { return []byte("0.99.0"), nil }
	}
	o.Mode = "bootstrap"
	o.Apply = true
	first := one(t, o, deps)
	if first.Status != "installed" {
		t.Fatal(first)
	}
	chat := o
	chat.Mode = "chat"
	chat.RunnerData = ""
	chat.ChatProfile = filepath.Join(o.Home, "shared-private.json")
	second := one(t, chat, deps)
	if second.Status != "installed" {
		t.Fatal(second)
	}
	return o, deps, first
}
func TestDefaultInteractionMigratesOwnedLegacyJSONAndExportsPreservingPaths(t *testing.T) {
	for _, client := range []string{"claude", "cursor", "kimi", "pi", "generic", "dsh"} {
		t.Run(client, func(t *testing.T) {
			o, deps, first := legacyFixture(t, client)
			selectedRunner := o.RunnerData
			selectedProfile := filepath.Join(o.Home, "shared-private.json")
			before := read(t, first.Path)
			if client != "generic" && client != "dsh" {
				var root, servers map[string]json.RawMessage
				object(before, &root)
				object(root["mcpServers"], &servers)
				var main map[string]json.RawMessage
				object(servers["coordica"], &main)
				main["disabled"] = json.RawMessage(`true`)
				main["toolTimeout"] = json.RawMessage(`42`)
				main["env"] = json.RawMessage(`{"SECRET":"unrelated-secret"}`)
				servers["coordica"], _ = json.Marshal(main)
				servers["other"] = json.RawMessage(`{"command":"other","disabled":true}`)
				root["mcpServers"], _ = json.Marshal(servers)
				root["disabledMcpServers"] = json.RawMessage(`["coordica-chat","other"]`)
				root["trust"] = json.RawMessage(`{"other":false}`)
				root["projects"] = json.RawMessage(`{"/fixture/project":{"disabledMcpServers":["coordica-chat","other"],"hasTrustDialogAccepted":false,"unrelated":1}}`)
				before, _ = json.Marshal(root)
				put(t, first.Path, before)
			}
			o.Mode = ""
			o.RunnerData = ""
			o.Apply = false
			plan := one(t, o, deps)
			if plan.Status != "preview" || !strings.Contains(plan.Message, "owned") || !bytes.Equal(read(t, first.Path), before) {
				t.Fatal(plan)
			}
			assertInteractionPaths(t, client, plan.Content, selectedRunner, selectedProfile)
			o.Apply = true
			result := one(t, o, deps)
			if result.Status != "installed" {
				t.Fatal(result)
			}
			actual := read(t, first.Path)
			if !bytes.Contains(actual, []byte("--interaction")) || bytes.Contains(actual, []byte("--bootstrap")) {
				t.Fatal(string(actual))
			}
			if client == "generic" || client == "dsh" {
				extension := ".mcp.json"
				if client == "dsh" {
					extension = ".cordis.yml"
					if !strings.Contains(result.Message, "Remove previously loaded") {
						t.Fatal(result)
					}
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(first.Path), "coordica-chat"+extension)); !os.IsNotExist(err) {
					t.Fatal("redundant export retained", err)
				}
			} else {
				if !bytes.Contains(actual, []byte("unrelated-secret")) || !bytes.Contains(actual, []byte(`"disabled":true`)) && !bytes.Contains(actual, []byte(`"disabled": true`)) || bytes.Contains(actual, []byte("coordica-chat")) || !bytes.Contains(actual, []byte("trust")) {
					t.Fatal("client state lost", string(actual))
				}
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(first.Path), "."+filepath.Base(first.Path)+".coordica-backup-*"))
			if len(backups) == 0 {
				t.Fatal("no migration backup")
			}
			found := false
			for _, backup := range backups {
				if bytes.Equal(read(t, backup), before) {
					found = true
				}
			}
			if !found {
				t.Fatal("migration snapshot not backed up")
			}
			again := one(t, o, deps)
			if again.Status != "unchanged" || !bytes.Equal(read(t, first.Path), actual) {
				t.Fatal(again)
			}
			if strings.Contains(result.Content, "unrelated-secret") {
				t.Fatal("private settings leaked")
			}
		})
	}
}
func TestInteractionMigrationConflictsAreReadOnlyIncludingPreview(t *testing.T) {
	for _, scenario := range []string{"binary", "runner-path", "profile-path", "generic-name", "chat-binary", "chat-scope", "option-conflict"} {
		for _, apply := range []bool{false, true} {
			t.Run(scenario+string(rune('0'+boolInt(apply))), func(t *testing.T) {
				o, deps, first := legacyFixture(t, "cursor")
				var root, servers map[string]json.RawMessage
				object(read(t, first.Path), &root)
				object(root["mcpServers"], &servers)
				var main, chat map[string]json.RawMessage
				object(servers["coordica"], &main)
				object(servers["coordica-chat"], &chat)
				switch scenario {
				case "binary":
					main["command"] = json.RawMessage(`"/unrelated/tool"`)
				case "runner-path":
					o.RunnerData = filepath.Join(o.Home, "other-runner")
				case "profile-path":
					o.ChatProfile = filepath.Join(o.Home, "other-profile.json")
				case "generic-name":
					main["args"] = json.RawMessage(`["mcp"]`)
				case "chat-binary":
					chat["command"] = json.RawMessage(`"/unrelated/tool"`)
				case "chat-scope":
					chat["args"] = json.RawMessage(`["mcp","--runner-data","/fixture","--run","run_other","--generation","1"]`)
				case "option-conflict":
					main["disabled"] = json.RawMessage(`true`)
					chat["disabled"] = json.RawMessage(`false`)
				}
				servers["coordica"], _ = json.Marshal(main)
				servers["coordica-chat"], _ = json.Marshal(chat)
				root["mcpServers"], _ = json.Marshal(servers)
				before, _ := json.Marshal(root)
				put(t, first.Path, before)
				o.Mode = ""
				o.Apply = apply
				result := one(t, o, deps)
				if result.Status != "conflict" || !bytes.Equal(read(t, first.Path), before) {
					t.Fatal(result)
				}
			})
		}
	}
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestCodexMigrationPreservesRawOptionsDisabledAndCAS(t *testing.T) {
	o, deps := fixture(t, "codex")
	o.Mode = "interaction"
	o.RunnerData = ""
	runnerPath := filepath.Join(o.Home, "saved-runner")
	profilePath := filepath.Join(o.Home, "shared.json")
	main := o
	main.Mode = "bootstrap"
	main.RunnerData = runnerPath
	chat := o
	chat.Mode = "chat"
	chat.ChatProfile = profilePath
	cmd, _ := json.Marshal(o.Executable)
	mainArgs, _ := json.Marshal(arguments(main))
	chatArgs, _ := json.Marshal(arguments(chat))
	original := []byte("model = 'keep-model'\n# unrelated settings\n[mcp_servers.other]\ncommand = 'other'\nargs = ['do-not-change']\n[mcp_servers.coordica]\nenabled = false\nstartup_timeout_sec = 33\ncommand = " + string(cmd) + "\nargs = " + string(mainArgs) + "\n[mcp_servers.coordica.env]\nSECRET = 'private-fixture'\n[mcp_servers.coordica-chat]\ncommand = " + string(cmd) + "\nargs = " + string(chatArgs) + "\n")
	path := filepath.Join(o.Home, ".codex", "config.toml")
	put(t, path, original)
	list := func(migrated bool) []byte {
		m := launcher(main, true)
		if migrated {
			current := o
			current.RunnerData = runnerPath
			current.ChatProfile = profilePath
			m = launcher(current, true)
		}
		entries := []any{map[string]any{"name": "coordica", "enabled": false, "transport": json.RawMessage(m)}}
		if !migrated {
			entries = append(entries, map[string]any{"name": "coordica-chat", "enabled": true, "transport": json.RawMessage(launcher(chat, true))})
		}
		raw, _ := json.Marshal(entries)
		return raw
	}
	deps.lookPath = func(string) (string, error) { return "/fixture/codex", nil }
	deps.command = func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if len(args) != 3 || args[1] != "list" {
			t.Fatal("migration discarded options through CLI add", args)
		}
		return list(bytes.Contains(read(t, path), []byte("--interaction"))), nil
	}
	plan := one(t, o, deps)
	if plan.Status != "preview" || !bytes.Equal(read(t, path), original) {
		t.Fatal(plan)
	}
	assertInteractionPaths(t, "codex", plan.Content, runnerPath, profilePath)
	o.Apply = true
	result := one(t, o, deps)
	if result.Status != "installed" {
		t.Fatal(result)
	}
	after := read(t, path)
	for _, preserved := range []string{"model = 'keep-model'", "enabled = false", "startup_timeout_sec = 33", "SECRET = 'private-fixture'", "[mcp_servers.other]\ncommand = 'other'\nargs = ['do-not-change']"} {
		if !bytes.Contains(after, []byte(preserved)) {
			t.Fatal("lost setting", preserved, string(after))
		}
	}
	if bytes.Contains(after, []byte("coordica-chat")) || strings.Contains(result.Content, "private-fixture") {
		t.Fatal(result, string(after))
	}
	if again := one(t, o, deps); again.Status != "unchanged" {
		t.Fatal(again)
	}
	before, err := readSnapshot(o.Home, path)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, []byte("# concurrent writer\n"))
	if _, err = writeConfig(o.Home, path, before, after); err != errChanged {
		t.Fatal("CAS lost", err)
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.toml.coordica-backup-*"))
	if len(backups) != 1 || !bytes.Equal(read(t, backups[0]), original) {
		t.Fatal("missing original private backup")
	}
}

func TestCodexMigrationMultilineArgsAndIdentityConflict(t *testing.T) {
	o, _ := fixture(t, "codex")
	o.Mode = "interaction"
	old := o
	old.Mode = "bootstrap"
	command, _ := json.Marshal(o.Executable)
	args := arguments(old)
	var array strings.Builder
	array.WriteString("[\n")
	for _, arg := range args {
		raw, _ := json.Marshal(arg)
		array.WriteString("  " + string(raw) + ", # retained syntax\n")
	}
	array.WriteString("]\n")
	raw := []byte("[mcp_servers.coordica]\ncommand = " + string(command) + "\nargs = " + array.String() + "enabled = false\n")
	list, _ := json.Marshal([]any{map[string]any{"name": "coordica", "enabled": false, "transport": json.RawMessage(launcher(old, true))}})
	updated, _, _, err := migrateCodexTOML(raw, list, o)
	if err != nil || !bytes.Contains(updated, []byte("--interaction")) || !bytes.Contains(updated, []byte("enabled = false")) {
		t.Fatal(string(updated), err)
	}
	o.RunnerData = filepath.Join(o.Home, "different")
	if _, _, _, err = migrateCodexTOML(raw, list, o); err == nil {
		t.Fatal("explicit path conflict ignored")
	}
}

func TestAutoSetupAgreesOnOneHostProfileBeforeAnyWrites(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(string(rune('0'+boolInt(conflict))), func(t *testing.T) {
			o, deps := fixture(t, "claude")
			o.RunnerData = ""
			o.Mode = "chat"
			o.ChatProfile = filepath.Join(o.Home, "existing-shared.json")
			o.Apply = true
			first := one(t, o, deps)
			if first.Status != "installed" {
				t.Fatal(first)
			}
			cursor := o
			cursor.Client = "cursor"
			if conflict {
				cursor.ChatProfile = filepath.Join(o.Home, "different-shared.json")
			}
			second := one(t, cursor, deps)
			if second.Status != "installed" {
				t.Fatal(second)
			}
			a, b := read(t, first.Path), read(t, second.Path)
			o.Client = "auto"
			o.Mode = ""
			o.ChatProfile = ""
			o.Apply = true
			results, err := run(context.Background(), o, deps)
			if err != nil {
				t.Fatal(err)
			}
			if conflict {
				if len(results) != 1 || results[0].Status != "conflict" || !bytes.Equal(read(t, first.Path), a) || !bytes.Equal(read(t, second.Path), b) {
					t.Fatal("cross-client conflict mutated config", results)
				}
			} else {
				for _, r := range results {
					if r.Status != "installed" || !strings.Contains(r.Content, "existing-shared.json") {
						t.Fatal("host shared profile lost", results)
					}
				}
			}
		})
	}
}

func TestCodexMigrationDoesNotTreatQuotedServerDotsOrStringsAsOwnedTables(t *testing.T) {
	o, _ := fixture(t, "codex")
	o.Mode = "interaction"
	old := o
	old.Mode = "bootstrap"
	command, _ := json.Marshal(o.Executable)
	args, _ := json.Marshal(arguments(old))
	unrelated := "[mcp_servers.\"coordica-chat.unrelated\"]\ncommand = 'unrelated'\nargs = []\n"
	raw := []byte("[mcp_servers.coordica]\ncommand = " + string(command) + "\nargs = " + string(args) + "\n" + unrelated)
	list, _ := json.Marshal([]any{map[string]any{"name": "coordica", "enabled": true, "transport": json.RawMessage(launcher(old, true))}})
	updated, _, _, err := migrateCodexTOML(raw, list, o)
	if err != nil || !bytes.Contains(updated, []byte(unrelated)) {
		t.Fatal("unrelated quoted server changed", string(updated), err)
	}
	malicious := append([]byte("notes = '''\n[mcp_servers.coordica-chat.env]\nSECRET = 'unrelated'\n'''\n"), raw...)
	if _, _, _, err = migrateCodexTOML(malicious, list, o); err == nil {
		t.Fatal("multiline text treated as configuration tables")
	}
}
