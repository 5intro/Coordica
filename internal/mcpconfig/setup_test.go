package mcpconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, client string) (Options, dependencies) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Client: client, Home: home, ConfigHome: filepath.Join(home, "config"), Executable: filepath.Join(home, "Agent Tools 工具", "coordica"), RunnerData: filepath.Join(home, "Runner Data 数据")}
	deps := dependencies{
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
		command: func(context.Context, string, []string, string) ([]byte, error) {
			t.Fatal("unexpected external command")
			return nil, errors.New("unexpected command")
		},
		getenv: func(string) string { return "" }, // Never inherit a real client profile.
	}
	return options, deps
}

func one(t *testing.T, options Options, deps dependencies) Result {
	t.Helper()
	results, err := run(context.Background(), options, deps)
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %+v, error = %v", results, err)
	}
	return results[0]
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestJSONSetupPreservesSettingsAndIsIdempotent(t *testing.T) {
	for _, client := range []string{"claude", "cursor"} {
		t.Run(client, func(t *testing.T) {
			options, deps := fixture(t, client)
			plan := one(t, options, deps)
			if plan.Status != "preview" {
				t.Fatal(plan)
			}
			if _, err := os.Stat(filepath.Dir(plan.Path)); client == "cursor" && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preview created a directory")
			}
			original := []byte(`{"unknown":{"number":9223372036854775807},"mcpServers":{"other":{"command":"existing","env":{"SECRET":"private-fixture"}}}}`)
			put(t, plan.Path, original)
			options.Apply = true
			result := one(t, options, deps)
			if result.Status != "installed" {
				t.Fatal(result)
			}
			actual := read(t, plan.Path)
			if !bytes.Contains(actual, []byte("9223372036854775807")) || !bytes.Contains(actual, []byte("private-fixture")) {
				t.Fatal("unrelated settings were lost")
			}
			var root map[string]json.RawMessage
			if err := object(actual, &root); err != nil {
				t.Fatal(err)
			}
			var servers map[string]json.RawMessage
			json.Unmarshal(root["mcpServers"], &servers)
			if !sameLauncher(servers["coordica"], launcher(options, true)) || !bytes.Contains(servers["coordica"], []byte(`"stdio"`)) {
				t.Fatal("missing expected stdio launcher")
			}
			encoded, _ := json.Marshal(result)
			if bytes.Contains(encoded, []byte("private-fixture")) {
				t.Fatal("existing credentials appeared in a result")
			}
			backups, _ := filepath.Glob(filepath.Join(filepath.Dir(plan.Path), "."+filepath.Base(plan.Path)+".coordica-backup-*"))
			if len(backups) != 1 || !bytes.Equal(read(t, backups[0]), original) {
				t.Fatal("original backup was not preserved")
			}
			if runtime.GOOS != "windows" {
				info, _ := os.Stat(backups[0])
				if info.Mode().Perm()&0077 != 0 {
					t.Fatal("backup permits other users")
				}
			}
			if again := one(t, options, deps); again.Status != "unchanged" || !bytes.Equal(read(t, plan.Path), actual) {
				t.Fatalf("not idempotent: %+v", again)
			}
		})
	}
}

func TestExistingConfigurationIsNeverOverwrittenOnConflict(t *testing.T) {
	for _, existing := range []string{
		``, `null`, `{`, `{"mcpServers":null}`, `{"mcpServers":[]}`,
		`{"mcpServers":{"coordica":{"command":"other","args":[]}}}`,
		`{"unknown":{"same":1,"same":2}}`,
		`{"mcpServers":{},"mcpServers":{}}`,
	} {
		t.Run(fmt.Sprintf("input-%x", []byte(existing)), func(t *testing.T) {
			options, deps := fixture(t, "cursor")
			path := filepath.Join(options.Home, ".cursor", "mcp.json")
			put(t, path, []byte(existing))
			options.Apply = true
			result := one(t, options, deps)
			if result.Status != "error" && result.Status != "conflict" {
				t.Fatal(result)
			}
			if string(read(t, path)) != existing {
				t.Fatal("existing configuration changed")
			}
		})
	}
}

func TestMatchingLauncherRetainsExtraOptions(t *testing.T) {
	options, deps := fixture(t, "claude")
	var entry map[string]any
	json.Unmarshal(launcher(options, true), &entry)
	entry["env"], entry["disabled"], entry["toolTimeout"] = map[string]string{"KEY": "private-fixture"}, true, 42
	raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"coordica": entry}})
	path := filepath.Join(options.Home, ".claude.json")
	put(t, path, raw)
	options.Apply = true
	if result := one(t, options, deps); result.Status != "unchanged" || strings.Contains(result.Content, "private-fixture") {
		t.Fatal(result)
	}
	if !bytes.Equal(raw, read(t, path)) {
		t.Fatal("extra settings were changed")
	}
	entry["type"] = "http"
	http, _ := json.Marshal(entry)
	if sameLauncher(http, launcher(options, true)) {
		t.Fatal("non-stdio transport accepted")
	}
}

func TestDetectsConcurrentModificationAndSymlinks(t *testing.T) {
	options, _ := fixture(t, "cursor")
	path := filepath.Join(options.Home, ".cursor", "mcp.json")
	put(t, path, []byte(`{}`))
	before, err := readSnapshot(options.Home, path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"edited":true}`)
	put(t, path, changed)
	if _, err := writeConfig(options.Home, path, before, []byte(`{"replacement":true}`)); !errors.Is(err, errChanged) {
		t.Fatalf("concurrent edit accepted: %v", err)
	}
	if !bytes.Equal(read(t, path), changed) {
		t.Fatal("concurrent edit lost")
	}
	link := filepath.Join(options.Home, "linked")
	if err := os.Symlink(filepath.Dir(path), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows account cannot create symbolic links")
		}
		t.Fatal(err)
	}
	if _, err := readSnapshot(options.Home, filepath.Join(link, "mcp.json")); err == nil {
		t.Fatal("symlinked parent accepted")
	}
}

func TestExportUsesLocalPathsAndNeverReplacesAnEditedFile(t *testing.T) {
	for _, client := range []string{"dsh", "generic"} {
		t.Run(client, func(t *testing.T) {
			options, deps := fixture(t, client)
			preview := one(t, options, deps)
			if preview.Status != "preview" {
				t.Fatal(preview)
			}
			if _, err := os.Stat(options.ConfigHome); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preview wrote configuration")
			}
			options.Apply = true
			result := one(t, options, deps)
			if result.Status != "installed" || string(read(t, result.Path)) != result.Content {
				t.Fatal(result)
			}
			if one(t, options, deps).Status != "unchanged" {
				t.Fatal("export is not idempotent")
			}
			put(t, result.Path, []byte("user edit"))
			if one(t, options, deps).Status != "conflict" || string(read(t, result.Path)) != "user edit" {
				t.Fatal("user edit overwritten")
			}
		})
	}
	// Rendering is platform-independent; Windows quoting is checked on Unix too.
	options := Options{Executable: `C:\Agent Tools\工具\coordica.exe`, RunnerData: `C:\Runner Data\数据`}
	var decoded struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal(launcher(options, true), &decoded) != nil || decoded.Command != options.Executable || !reflect.DeepEqual(decoded.Args, arguments(options)) {
		t.Fatal("Windows argument escaping changed the launcher")
	}
}

func TestAutoDetectsOnlyExistingClients(t *testing.T) {
	options, deps := fixture(t, "auto")
	if one(t, options, deps).Status != "not_found" {
		t.Fatal("claimed nonexistent clients")
	}
	if err := os.Mkdir(filepath.Join(options.Home, ".cursor"), 0700); err != nil {
		t.Fatal(err)
	}
	result := one(t, options, deps)
	if result.Client != "cursor" || result.Status != "preview" {
		t.Fatal(result)
	}
}

func TestKimiLayoutAndCustomClientRoots(t *testing.T) {
	options, deps := fixture(t, "kimi")
	if one(t, options, deps).Status != "unsupported" {
		t.Fatal("guessed Kimi generation")
	}
	for _, dir := range []string{".kimi-code", ".kimi"} {
		if err := os.Mkdir(filepath.Join(options.Home, dir), 0700); err != nil {
			t.Fatal(err)
		}
		result := one(t, options, deps)
		if dir == ".kimi-code" && (result.Status != "preview" || result.Path != filepath.Join(options.Home, dir, "mcp.json")) {
			t.Fatal(result)
		}
		if dir == ".kimi" && result.Status != "unsupported" {
			t.Fatal("ambiguous layouts accepted")
		}
	}
	for _, client := range []string{"claude", "kimi", "pi"} {
		t.Run(client, func(t *testing.T) {
			options, deps := fixture(t, client)
			custom := filepath.Join(options.Home, "Custom Profile")
			variable := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "kimi": "KIMI_CODE_HOME", "pi": "PI_CODING_AGENT_DIR"}[client]
			deps.getenv = func(key string) string {
				if key == variable {
					return custom
				}
				return ""
			}
			deps.lookPath = func(name string) (string, error) { return filepath.Join(options.Home, name), nil }
			deps.command = func(context.Context, string, []string, string) ([]byte, error) { return []byte("0.99.0"), nil }
			options.Apply = true
			result := one(t, options, deps)
			if result.Status != "installed" || filepath.Dir(result.Path) != custom {
				t.Fatal(result)
			}
		})
	}
}

func TestPiRequiresConfirmedNativeVersion(t *testing.T) {
	for version, supported := range map[string]bool{"0.78.0": false, "0.98.99": false, "0.99.0": true, "pi v0.100.2": true, "1.0.0": true, "0.99.0-rc.1": false, "garbage 1.0.0": false} {
		if piSupportsMCP(version) != supported {
			t.Fatalf("version %q support mismatch", version)
		}
	}
	options, deps := fixture(t, "pi")
	deps.lookPath = func(string) (string, error) { return filepath.Join(options.Home, "pi"), nil }
	deps.command = func(context.Context, string, []string, string) ([]byte, error) { return []byte("0.78.0"), nil }
	options.Apply = true
	if one(t, options, deps).Status != "unsupported" {
		t.Fatal("old pi accepted")
	}
	if _, err := os.Stat(filepath.Join(options.Home, ".pi")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old pi configuration written")
	}
}

func codexList(options Options, command string, enabled bool) []byte {
	var transport map[string]any
	json.Unmarshal(launcher(options, true), &transport)
	transport["command"] = command
	transport["env"] = map[string]string{"SECRET": "codex-private-fixture"}
	raw, _ := json.Marshal([]any{map[string]any{"name": "coordica", "enabled": enabled, "transport": transport}})
	return raw
}

func TestCodexOfficialArgumentsAndVerification(t *testing.T) {
	options, deps := fixture(t, "codex")
	deps.lookPath = func(string) (string, error) { return filepath.Join(options.Home, "codex"), nil }
	var calls [][]string
	added := false
	var neutral string
	deps.command = func(_ context.Context, _ string, args []string, dir string) ([]byte, error) {
		if dir == "" || dir == options.Home || !filepath.IsAbs(dir) {
			t.Fatal("Codex query inherited a project directory")
		}
		if neutral != "" && dir != neutral {
			// Each setup call creates a new scope; all commands within that call
			// must still use the same existing empty directory.
			if _, err := os.Stat(neutral); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("Codex commands switched directories within an operation")
			}
		}
		neutral = dir
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			t.Fatal("Codex did not receive a neutral directory")
		}
		calls = append(calls, append([]string(nil), args...))
		if reflect.DeepEqual(args, []string{"mcp", "list", "--json"}) {
			if added {
				return codexList(options, options.Executable, true), nil
			}
			return []byte(`[{"name":"other","transport":{"env":{"SECRET":"private-fixture"}}}]`), nil
		}
		want := append([]string{"mcp", "add", "coordica", "--", options.Executable}, arguments(options)...)
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("wrong command arguments: %q", args)
		}
		added = true
		return nil, nil
	}
	if one(t, options, deps).Status != "preview" || added {
		t.Fatal("preview called add")
	}
	options.Apply = true
	result := one(t, options, deps)
	if result.Status != "installed" || !added || len(calls) != 5 {
		t.Fatalf("result=%+v calls=%q", result, calls)
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("private-fixture")) {
		t.Fatal("Codex credentials leaked")
	}
	if one(t, options, deps).Status != "unchanged" {
		t.Fatal("Codex setup not idempotent")
	}
}

func TestCodexConflictsDisabledAndCommandFailures(t *testing.T) {
	for _, scenario := range []string{"conflict", "disabled", "query-failure", "malformed", "add-failure", "verify-failure"} {
		t.Run(scenario, func(t *testing.T) {
			options, deps := fixture(t, "codex")
			options.Apply = true
			deps.lookPath = func(string) (string, error) { return filepath.Join(options.Home, "codex"), nil }
			added := false
			deps.command = func(_ context.Context, _ string, args []string, dir string) ([]byte, error) {
				if args[1] == "add" {
					if scenario != "add-failure" && scenario != "verify-failure" {
						t.Fatal("unexpected add")
					}
					added = true
					if scenario == "add-failure" {
						return nil, errors.New("private-fixture")
					}
					return nil, nil
				}
				switch scenario {
				case "conflict":
					return codexList(options, "different", true), nil
				case "disabled":
					return codexList(options, options.Executable, false), nil
				case "query-failure":
					return nil, errors.New("private-fixture")
				case "malformed":
					return []byte(`{"secrets":"private-fixture"}`), nil
				default:
					return []byte(`[]`), nil
				}
			}
			result := one(t, options, deps)
			want := "error"
			if scenario == "conflict" {
				want = "conflict"
			}
			if scenario == "disabled" {
				want = "unchanged"
				if !strings.Contains(result.Message, "disabled") {
					t.Fatal("disabled status hidden")
				}
			}
			if result.Status != want || strings.Contains(result.Message, "private-fixture") {
				t.Fatal(result)
			}
			if added != (scenario == "add-failure" || scenario == "verify-failure") {
				t.Fatal("wrong mutation boundary")
			}
		})
	}
	options, _ := fixture(t, "codex")
	for _, raw := range []string{`[{"name":"coordica","transport":{}}]`, `[{"name":"coordica","transport":{},"enabled":null}]`} {
		if _, _, _, err := codexEntry([]byte(raw), launcher(options, true)); err == nil {
			t.Fatal("missing or null enabled state accepted")
		}
	}
}

func TestSupervisedCommandHelper(t *testing.T) {
	if os.Getenv("COORDICA_SETUP_TEST_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "private-fixture")
	os.Exit(4)
}

func TestSupervisedCommandFailureDoesNotDiscloseOutput(t *testing.T) {
	t.Setenv("COORDICA_SETUP_TEST_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := command(ctx, executable, []string{"-test.run=^TestSupervisedCommandHelper$"}, "")
	if err == nil || len(out) != 0 || strings.Contains(err.Error(), "private-fixture") {
		t.Fatalf("output=%q error=%v", out, err)
	}
}
