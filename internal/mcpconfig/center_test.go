package mcpconfig

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnedCenterLauncherPreservesRolePathsAndClientSettings(t *testing.T) {
	root := t.TempDir()
	o := Options{Mode: "interaction", Executable: filepath.Join(root, "coordica"), HostRole: "center", HostConfig: filepath.Join(root, "center host.json"), RunnerData: filepath.Join(root, "selected runner"), ChatProfile: filepath.Join(root, "shared.json"), ConfigHome: root}
	var entry map[string]any
	json.Unmarshal(launcher(o, false), &entry)
	entry["disabled"] = true
	entry["timeout"] = 12345
	entry["unrelated"] = "retain"
	raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"coordica": entry, "other": map[string]any{"command": "other"}}, "outside": true})
	request := o
	request.HostRole = ""
	request.HostConfig = ""
	request.ChatProfile = ""
	request.RunnerData = ""
	merged, selected, _, e := mergeInteractionJSON(raw, request, false)
	if e != nil {
		t.Fatal(e)
	}
	if selected.HostRole != "center" || selected.HostConfig != o.HostConfig || selected.RunnerData != o.RunnerData || selected.ChatProfile != o.ChatProfile {
		t.Fatal("omitted flags downgraded role", selected)
	}
	for _, text := range []string{`"disabled":true`, `"timeout":12345`, `"unrelated":"retain"`, `"outside":true`, `"other"`} {
		var compact strings.Builder
		_ = compact
		if !strings.Contains(strings.ReplaceAll(string(merged), " ", ""), text) {
			t.Fatal("unrelated/disabled options lost", text, string(merged))
		}
	}
	for _, mutate := range []func(*Options){func(o *Options) { o.RunnerData = filepath.Join(root, "other-runner") }, func(o *Options) { o.HostRole = "node" }, func(o *Options) { o.HostConfig = filepath.Join(root, "other.json") }, func(o *Options) { o.ChatProfile = filepath.Join(root, "other-profile.json") }} {
		q := o
		mutate(&q)
		if _, _, _, e := mergeInteractionJSON(raw, q, false); e == nil {
			t.Fatal("role/path conflict overwritten")
		}
	}
}
func TestCenterLauncherRejectsForgedRoleAndAmbiguousPaths(t *testing.T) {
	root := t.TempDir()
	o := Options{Executable: filepath.Join(root, "coordica"), ConfigHome: root}
	for _, args := range [][]string{{"mcp", "--interaction", "--host-role", "center", "--chat-profile", filepath.Join(root, "chat")}, {"mcp", "--interaction", "--host-role", "node", "--host-config", filepath.Join(root, "host"), "--runner-data", filepath.Join(root, "runner"), "--chat-profile", filepath.Join(root, "chat")}, {"mcp", "--interaction", "--host-role", "center", "--host-role", "node", "--host-config", filepath.Join(root, "host"), "--chat-profile", filepath.Join(root, "chat")}} {
		raw, _ := json.Marshal(map[string]any{"command": o.Executable, "args": args})
		if _, e := ownedLauncher(raw, o); e == nil {
			t.Fatal("ambiguous role accepted", args)
		}
	}
}
func TestCenterWindowsFixedPathDecodingInTOMLAndJSON(t *testing.T) {
	for _, value := range []string{`C:\Users\中文\center config.json`, `C:\Users\name\profile.json`, `C:\fixed\trailing\`} {
		raw, _ := json.Marshal(value)
		decoded, e := tomlLauncherValue(string(raw))
		if e != nil || decoded != value {
			t.Fatal("Windows fixed path decoded incorrectly", value, decoded, e)
		}
	}
}
